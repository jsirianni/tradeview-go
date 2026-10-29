package tradeview

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryGlobalProviders(t *testing.T) {
	const scenarioEnv = "TRADEVIEW_TEST_TELEMETRY_GLOBALS"
	if scenario := os.Getenv(scenarioEnv); scenario != "" {
		testTelemetryGlobals(t, scenario)
		return
	}
	// The OTel global proxy has process-wide, once-only registration behavior.
	// Fresh processes prevent order-dependent tests and contamination of callers.
	for _, scenario := range []string{"unconfigured", "configured", "unsampled", "explicit-tracer", "explicit-meter", "explicit-propagator"} {
		t.Run(scenario, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestTelemetryGlobalProviders$", "-test.timeout=25s")
			command.Env = append(os.Environ(), scenarioEnv+"="+scenario)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("isolated globals %s: %v\n%s", scenario, err, output)
			}
		})
	}
}

func testTelemetryGlobals(t *testing.T, scenario string) {
	t.Helper()
	globals, explicit := newTelemetryFixture(t), newTelemetryFixture(t)
	options := TelemetryOptions{}
	if scenario == "explicit-tracer" {
		options.TracerProvider = explicit.traces
	}
	if scenario == "explicit-meter" {
		options.MeterProvider = explicit.meters
	}
	if scenario == "explicit-propagator" {
		options.Propagator = propagation.NewCompositeTextMapPropagator()
	}
	// Resolve nil fields when applying the option, even if the option was
	// constructed before the application's global providers were configured.
	option := WithTelemetry(options)
	if scenario != "unconfigured" {
		otel.SetTracerProvider(globals.traces)
		otel.SetMeterProvider(globals.meters)
		otel.SetTextMapPropagator(propagation.TraceContext{})
	}
	state, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatal(err)
	}
	flags := trace.FlagsSampled
	if scenario == "unsampled" {
		flags = 0
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3}, SpanID: trace.SpanID{4, 5, 6},
		TraceFlags: flags, TraceState: state, Remote: true,
	})
	ctx := trace.ContextWithRemoteSpanContext(t.Context(), parent)
	var propagated, doerContext trace.SpanContext
	calls := 0
	client, err := NewClientWithResponses("https://broker.example", option, WithHTTPClient(finiteResponseDoer(func(r *http.Request) (*http.Response, error) {
		calls++
		doerContext = trace.SpanContextFromContext(r.Context())
		propagated = trace.SpanContextFromContext(propagation.TraceContext{}.Extract(t.Context(), propagation.HeaderCarrier(r.Header)))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"s":"ok","d":{}}`)), Request: r}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetGroupsWithResponse(ctx); err != nil || calls != 1 {
		t.Fatalf("request calls=%d, error=%v", calls, err)
	}
	if scenario == "unconfigured" || scenario == "explicit-propagator" {
		if propagated.IsValid() {
			t.Fatal("no-op propagator injected a trace")
		}
	} else if doerContext.IsRemote() || !propagated.Equal(doerContext.WithRemote(true)) || propagated.TraceID() != parent.TraceID() || propagated.TraceState().String() != "vendor=value" || propagated.IsSampled() != parent.IsSampled() {
		t.Fatalf("propagation lost context: parent=%v, doer=%v, wire=%v", parent, doerContext, propagated)
	}
	wantSpans := 1
	if scenario == "unconfigured" || scenario == "unsampled" {
		wantSpans = 0
	}
	spanFixture, meterFixture := globals, globals
	if scenario == "explicit-tracer" {
		spanFixture = explicit
	}
	if scenario == "explicit-meter" {
		meterFixture = explicit
	}
	spans := spanFixture.spans.Ended()
	if len(spans) != wantSpans {
		t.Fatalf("recorded spans=%d, want %d", len(spans), wantSpans)
	}
	if wantSpans != 0 {
		span := spans[0]
		if span.Parent().SpanID() != parent.SpanID() || span.SpanContext().SpanID() == parent.SpanID() || span.SpanKind() != trace.SpanKindClient || span.Name() != "tradeview.getGroups" || span.InstrumentationScope().Name != "github.com/jsirianni/tradeview-go/pkg/tradeview" {
			t.Fatalf("invalid client span: %v", span)
		}
	}
	wantOperations := int64(1)
	if scenario == "unconfigured" {
		wantOperations = 0
	}
	if got := meterFixture.sum(t, "tradeview.client.operations"); got != wantOperations {
		t.Fatalf("completed operations=%d, want %d", got, wantOperations)
	}
	if got := meterFixture.sum(t, "tradeview.client.active_operations"); got != 0 {
		t.Fatalf("active operations leaked: %d", got)
	}
	if scenario == "explicit-tracer" && len(globals.spans.Ended()) != 0 {
		t.Fatal("explicit tracer also recorded to globals")
	}
	if scenario == "explicit-meter" && globals.sum(t, "tradeview.client.operations") != 0 {
		t.Fatal("explicit meter also recorded to globals")
	}
	if scenario != "explicit-tracer" && len(explicit.spans.Ended()) != 0 {
		t.Fatal("unused explicit tracer recorded a span")
	}
}

func TestTelemetryDefaultGlobalsNoop(t *testing.T) {
	// Also exercise the default branch in the coverage-owning parent process.
	client, err := NewClientWithResponses("https://broker.example", WithTelemetry(TelemetryOptions{}), WithHTTPClient(finiteResponseDoer(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("traceparent") != "" || r.Header.Get("tracestate") != "" {
			t.Error("unconfigured globals injected trace headers")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"s":"ok","d":{}}`)), Request: r}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetGroupsWithResponse(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestTelemetryMetricContract(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "finite"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newTelemetryFixture(t)
			body, operation, route := `{"s":"ok","d":{}}`, "getGroups", "/groups"
			if streaming {
				body, operation, route = "{\"s\":\"ok\"}\n{\"s\":\"error\",\"errmsg\":\"denied\"}\n", "streamOrders", "/accounts/{accountId}/stream/orders"
			}
			client, err := NewClient("https://broker.example", fixture.option, WithHTTPClient(finiteResponseDoer(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})))
			if err != nil {
				t.Fatal(err)
			}
			var finish func()
			if streaming {
				stream, err := client.OpenStreamOrders(t.Context(), "private-account", &StreamOrdersParams{Locale: En})
				if err != nil {
					t.Fatal(err)
				}
				finish = func() {
					for stream.Next() {
					}
					if stream.Err() == nil {
						t.Fatal("missing terminal broker error")
					}
					closeTestBody(t, stream)
				}
			} else {
				response, err := client.GetGroups(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				finish = func() {
					_, err := io.Copy(io.Discard, response.Body)
					checkTestError(t, err)
					closeTestBody(t, response.Body)
				}
			}
			if fixture.sum(t, "tradeview.client.active_operations") != 1 {
				t.Fatal("in-flight operation missing from gauge")
			}
			finish()
			base := []attribute.KeyValue{
				attribute.String("tradeview.operation", operation), attribute.String("http.request.method", "GET"),
				attribute.String("url.template", route), attribute.Bool("tradeview.streaming", streaming),
			}
			completed := append(append([]attribute.KeyValue(nil), base...), attribute.Int("http.response.status_code", 200))
			if streaming {
				completed = append(completed, attribute.String("error.type", "api"))
			}
			baseSet, completedSet := attribute.NewSet(base...), attribute.NewSet(completed...)
			var data metricdata.ResourceMetrics
			if err := fixture.reader.Collect(t.Context(), &data); err != nil {
				t.Fatal(err)
			}
			if len(data.ScopeMetrics) != 1 || data.ScopeMetrics[0].Scope.Name != "github.com/jsirianni/tradeview-go/pkg/tradeview" {
				t.Fatalf("instrumentation scope: %v", data.ScopeMetrics)
			}
			units := map[string]string{
				"tradeview.client.operations": "{operation}", "tradeview.client.active_operations": "{operation}",
				"tradeview.client.operation.duration": "s", "tradeview.client.response.body.size": "By",
			}
			if streaming {
				units["tradeview.client.stream.frames"] = "{frame}"
			}
			for _, metric := range data.ScopeMetrics[0].Metrics {
				unit, ok := units[metric.Name]
				if !ok || metric.Unit != unit || metric.Description == "" {
					t.Fatalf("unexpected instrument: %+v", metric)
				}
				delete(units, metric.Name)
				switch value := metric.Data.(type) {
				case metricdata.Sum[int64]:
					if metric.Name != "tradeview.client.operations" && metric.Name != "tradeview.client.active_operations" && metric.Name != "tradeview.client.stream.frames" {
						t.Fatalf("unexpected sum instrument: %s", metric.Name)
					}
					want, attrs, monotonic := int64(1), completedSet, true
					if metric.Name == "tradeview.client.active_operations" {
						want, attrs, monotonic = 0, baseSet, false
					} else if metric.Name == "tradeview.client.stream.frames" {
						want, attrs = 2, baseSet
					}
					if value.Temporality != metricdata.CumulativeTemporality || value.IsMonotonic != monotonic || len(value.DataPoints) != 1 || value.DataPoints[0].Value != want || !value.DataPoints[0].Attributes.Equals(&attrs) {
						t.Fatalf("sum contract %s: %+v", metric.Name, value)
					}
				case metricdata.Histogram[int64]:
					if metric.Name != "tradeview.client.response.body.size" || value.Temporality != metricdata.CumulativeTemporality || len(value.DataPoints) != 1 || value.DataPoints[0].Count != 1 || value.DataPoints[0].Sum != int64(len(body)) || !value.DataPoints[0].Attributes.Equals(&completedSet) {
						t.Fatalf("body histogram: %+v", value)
					}
				case metricdata.Histogram[float64]:
					bounds := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
					if metric.Name != "tradeview.client.operation.duration" || value.Temporality != metricdata.CumulativeTemporality || len(value.DataPoints) != 1 || value.DataPoints[0].Count != 1 || value.DataPoints[0].Sum < 0 || !reflect.DeepEqual(value.DataPoints[0].Bounds, bounds) || !value.DataPoints[0].Attributes.Equals(&completedSet) {
						t.Fatalf("duration histogram: %+v", value)
					}
				default:
					t.Fatalf("unexpected aggregation for %s: %T", metric.Name, metric.Data)
				}
			}
			if len(units) != 0 {
				t.Fatalf("missing instruments: %v", units)
			}
		})
	}
}
