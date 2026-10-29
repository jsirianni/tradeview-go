package tradeview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type telemetryFixture struct {
	option ClientOption
	spans  *tracetest.SpanRecorder
	reader *sdkmetric.ManualReader
	traces *sdktrace.TracerProvider
}

func newTelemetryFixture(t *testing.T) telemetryFixture {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()); _ = mp.Shutdown(context.Background()) })
	return telemetryFixture{WithTelemetry(TelemetryOptions{TracerProvider: tp, MeterProvider: mp, Propagator: propagation.TraceContext{}}), recorder, reader, tp}
}

func spanAttribute(span sdktrace.ReadOnlySpan, key string) string {
	for _, value := range span.Attributes() {
		if string(value.Key) == key {
			return value.Value.String()
		}
	}
	return ""
}

func (f telemetryFixture) sum(t *testing.T, name string) int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s is %T", name, m.Data)
				}
				var value int64
				for _, point := range sum.DataPoints {
					value += point.Value
				}
				return value
			}
		}
	}
	return 0
}

func TestTelemetryEveryOperation(t *testing.T) {
	contract := loadFixtureContract(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"s":"ok"}`)
	}))
	defer server.Close()
	for _, fixture := range _operationFixtures {
		for _, typed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed=%t", fixture.id, typed), func(t *testing.T) {
				f := newTelemetryFixture(t)
				client, err := NewClient(server.URL, f.option)
				if err != nil {
					t.Fatal(err)
				}
				entry := contract.manifest[fixture.id]
				method := reflect.ValueOf(client).MethodByName(entry.RawMethod)
				if typed {
					if entry.Streaming {
						method = reflect.ValueOf(client).MethodByName(entry.TypedMethod)
					} else {
						method = reflect.ValueOf(&ClientWithResponses{ClientInterface: client}).MethodByName(entry.TypedMethod)
					}
				}
				query, _ := contract.queryFixture(fixture.id, contract.operations[fixture.id])
				_, body := contract.requestBody(contract.operations[fixture.id])
				result := invokeFixture(t, method, t.Context(), fixture.path, query, body)
				if !result[1].IsNil() {
					t.Fatal(result[1].Interface())
				}
				if !typed {
					response := fixtureValue[*http.Response](t, result[0].Interface())
					if len(f.spans.Ended()) != 0 {
						t.Fatal("raw operation ended before consumption")
					}
					_, err := io.ReadAll(response.Body)
					if err != nil {
						t.Fatal(err)
					}
					_ = response.Body.Close()
				} else if entry.Streaming {
					if len(f.spans.Ended()) != 0 {
						t.Fatal("stream ended on headers")
					}
					result[0].MethodByName("Close").Call(nil)
				}
				spans := f.spans.Ended()
				if len(spans) != 1 || spans[0].Name() != "tradeview."+fixture.id {
					t.Fatalf("spans: %v", spans)
				}
				if spanAttribute(spans[0], "url.template") != fixture.path || spanAttribute(spans[0], "http.request.method") != fixture.method {
					t.Fatalf("attributes: %v", spans[0].Attributes())
				}
				if f.sum(t, "tradeview.client.operations") != 1 || f.sum(t, "tradeview.client.active_operations") != 0 {
					t.Fatal("unbalanced operation metrics")
				}
			})
		}
	}
}

func TestTelemetryTypedErrorsAndPrivacy(t *testing.T) {
	for _, scenario := range []struct {
		name, body, contentType, category string
		status                            int
		limit                             int64
	}{
		{"success", `{"s":"ok"}`, "application/json", "", 200, 0},
		{"api", `{"s":"error","errtype":"secret-code","errmsg":"secret-message"}`, "application/json", "api", 200, 0},
		{"http", "secret-body", "text/plain", "http", 503, 0},
		{"malformed", "{", "application/json", "protocol", 200, 0},
		{"content-type", `{"s":"ok"}`, "text/plain", "protocol", 200, 0},
		{"size", `{"s":"ok"}`, "application/json", "response_size", 200, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newTelemetryFixture(t)
			ctx, parent := f.traces.Tracer("test").Start(t.Context(), "parent")
			defer parent.End()
			propagatedContext := make(chan trace.SpanContext, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				propagatedContext <- trace.SpanContextFromContext(propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header)))
				w.Header().Set("Content-Type", scenario.contentType)
				w.WriteHeader(scenario.status)
				_, _ = io.WriteString(w, scenario.body)
			}))
			defer server.Close()
			opts := []ClientOption{f.option, WithBearerToken("secret-token")}
			if scenario.limit > 0 {
				opts = append(opts, WithFiniteResponseLimit(scenario.limit))
			}
			client, err := NewClientWithResponses(server.URL+"/secret-base", opts...)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetBalancesWithResponse(ctx, "secret-account", &GetBalancesParams{Locale: "secret-locale"})
			if (err != nil) != (scenario.category != "") {
				t.Fatalf("unexpected error: %v", err)
			}
			spans := f.spans.Ended()
			if len(spans) != 1 {
				t.Fatalf("spans=%d", len(spans))
			}
			span := spans[0]
			propagated := <-propagatedContext
			if span.Parent().SpanID() != parent.SpanContext().SpanID() || propagated.SpanID() != span.SpanContext().SpanID() {
				t.Fatal("lost parent/propagation")
			}
			if spanAttribute(span, "error.type") != scenario.category {
				t.Fatalf("attrs=%v", span.Attributes())
			}
			if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "secret") {
				t.Fatal("sensitive values leaked into telemetry")
			}
			var metrics metricdata.ResourceMetrics
			if err := f.reader.Collect(ctx, &metrics); err != nil {
				t.Fatal(err)
			}
			seenDuration, seenSize := false, false
			for _, scope := range metrics.ScopeMetrics {
				for _, metric := range scope.Metrics {
					switch metric.Name {
					case "tradeview.client.operation.duration":
						histogram, ok := metric.Data.(metricdata.Histogram[float64])
						if !ok || len(histogram.DataPoints) != 1 || histogram.DataPoints[0].Count != 1 || histogram.DataPoints[0].Sum < 0 {
							t.Fatalf("invalid duration: %v", metric)
						}
						seenDuration = true
					case "tradeview.client.response.body.size":
						histogram, ok := metric.Data.(metricdata.Histogram[int64])
						want := int64(len(scenario.body))
						if scenario.limit > 0 {
							want = scenario.limit + 1
						}
						if !ok || len(histogram.DataPoints) != 1 || histogram.DataPoints[0].Count != 1 || histogram.DataPoints[0].Sum != want {
							t.Fatalf("invalid body size: %v", metric)
						}
						seenSize = true
					}
				}
			}
			if !seenDuration || !seenSize {
				t.Fatal("missing histograms")
			}
			if strings.Contains(fmt.Sprint(metrics), "secret") {
				t.Fatal("sensitive metric attributes")
			}
			if scenario.category != "" && span.Status().Code != codes.Error {
				t.Fatal("error span not marked")
			}
		})
	}
}

func TestTelemetryEditorFailureAndDisabled(t *testing.T) {
	f := newTelemetryFixture(t)
	client, err := NewClient("http://example.invalid", f.option, WithRequestEditorFn(func(context.Context, *http.Request) error { return errors.New("secret-editor-error") }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetGroups(t.Context())
	if err == nil {
		t.Fatal("missing editor error")
	}
	if spans := f.spans.Ended(); len(spans) != 1 || spanAttribute(spans[0], "error.type") != "client" {
		t.Fatalf("spans=%v", spans)
	}
	if f.sum(t, "tradeview.client.active_operations") != 0 {
		t.Fatal("leaked active operation")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") != "" {
			t.Error("disabled propagation")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	disabled, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := f.traces.Tracer("test").Start(t.Context(), "parent")
	defer parent.End()
	response, err := disabled.GetGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if _, ok := response.Body.(*observedBody); ok {
		t.Fatal("disabled telemetry wrapped body")
	}
}

func TestTelemetryStreamLifecycle(t *testing.T) {
	for _, mode := range []string{"eof", "api", "decode", "cancel", "close"} {
		t.Run(mode, func(t *testing.T) {
			f := newTelemetryFixture(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, "{\"s\":\"ok\"}\n")
				if mode == "api" {
					_, _ = io.WriteString(w, "{\"s\":\"error\",\"errmsg\":\"secret\"}\n")
				}
				if mode == "decode" {
					_, _ = io.WriteString(w, "{\n")
				}
				if mode == "cancel" || mode == "close" {
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			client, err := NewClient(server.URL, f.option)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream, err := client.OpenStreamOrders(ctx, "secret-account", &StreamOrdersParams{})
			if err != nil {
				t.Fatal(err)
			}
			if !stream.Next() {
				t.Fatal(stream.Err())
			}
			if len(f.spans.Ended()) != 0 {
				t.Fatal("stream ended at frame")
			}
			if mode == "cancel" {
				cancel()
			}
			if mode == "close" {
				nextDone := make(chan struct{})
				go func() { stream.Next(); close(nextDone) }()
				var wg sync.WaitGroup
				for range 8 {
					wg.Go(func() { _ = stream.Close() })
				}
				wg.Wait()
				<-nextDone
			} else {
				for stream.Next() {
				}
			}
			_ = stream.Close()
			spans := f.spans.Ended()
			if len(spans) != 1 {
				t.Fatalf("spans=%d", len(spans))
			}
			category := map[string]string{"api": "api", "decode": "client", "cancel": "canceled"}[mode]
			if spanAttribute(spans[0], "error.type") != category {
				t.Fatalf("attrs=%v err=%v", spans[0].Attributes(), stream.Err())
			}
			frames := int64(1)
			if mode == "api" {
				frames = 2
			}
			if f.sum(t, "tradeview.client.stream.frames") != frames {
				t.Fatal("wrong frame count")
			}
			if f.sum(t, "tradeview.client.operations") != 1 || f.sum(t, "tradeview.client.active_operations") != 0 {
				t.Fatal("unbalanced stream metrics")
			}
		})
	}
}
