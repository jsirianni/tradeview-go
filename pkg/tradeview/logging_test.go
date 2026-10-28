package tradeview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func logRecords(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []map[string]any
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				return records
			}
			t.Fatal(err)
		}
		records = append(records, record)
	}
}

func TestLoggingEveryOperation(t *testing.T) {
	contract := loadFixtureContract(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") != "" {
			t.Error("logging alone enabled trace propagation")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"s":"ok"}`)
	}))
	defer server.Close()
	for _, fixture := range _operationFixtures {
		for _, typed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed=%t", fixture.id, typed), func(t *testing.T) {
				var output bytes.Buffer
				client, err := NewClient(server.URL, WithLogger(slog.New(slog.NewJSONHandler(&output, nil))))
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
				wantBytes := float64(len(`{"s":"ok"}`))
				if !typed {
					response := fixtureValue[*http.Response](t, result[0].Interface())
					if output.Len() != 0 {
						t.Fatal("logged raw operation before body consumption")
					}
					if _, err := io.ReadAll(response.Body); err != nil {
						t.Fatal(err)
					}
					_ = response.Body.Close()
				} else if entry.Streaming {
					if output.Len() != 0 {
						t.Fatal("logged stream before completion")
					}
					result[0].MethodByName("Close").Call(nil)
					result[0].MethodByName("Close").Call(nil)
					wantBytes = 0
				}
				records := logRecords(t, output.Bytes())
				if len(records) != 1 {
					t.Fatalf("got %d completion records", len(records))
				}
				r := records[0]
				if r["operation"] != fixture.id || r["route"] != fixture.path || r["method"] != fixture.method || r["streaming"] != entry.Streaming {
					t.Fatalf("wrong operation metadata: %v", r)
				}
				if r["level"] != "INFO" || r["status_code"] != float64(200) || r["response_bytes"] != wantBytes || r["error_type"] != nil {
					t.Fatalf("wrong completion: %v", r)
				}
				if duration, ok := r["duration"].(float64); !ok || duration < 0 {
					t.Fatalf("missing duration: %v", r)
				}
			})
		}
	}
}

func TestLoggingErrorsAndPrivacy(t *testing.T) {
	for _, scenario := range []struct {
		name, body, category string
		status               int
		limit                int64
	}{
		{"api", `{"s":"error","errtype":"secret-code","errmsg":"secret-message"}`, "api", 200, 0},
		{"http", "secret-body", "http", 503, 0},
		{"protocol", "{secret", "protocol", 200, 0},
		{"size", `{"s":"ok"}`, "response_size", 200, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(scenario.status)
				_, _ = io.WriteString(w, scenario.body)
			}))
			defer server.Close()
			var output bytes.Buffer
			opts := []ClientOption{WithLogger(slog.New(slog.NewJSONHandler(&output, nil))), WithBearerToken("secret-token")}
			if scenario.limit != 0 {
				opts = append(opts, WithFiniteResponseLimit(scenario.limit))
			}
			client, err := NewClientWithResponses(server.URL+"/secret-base", opts...)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetBalancesWithResponse(t.Context(), "secret-account", &GetBalancesParams{Locale: "secret-locale"})
			if err == nil {
				t.Fatal("missing response error")
			}
			records := logRecords(t, output.Bytes())
			if len(records) != 1 || records[0]["error_type"] != scenario.category || records[0]["level"] != "ERROR" {
				t.Fatalf("wrong error log: %v", records)
			}
			if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), server.URL) {
				t.Fatalf("sensitive values leaked: %s", output.String())
			}
		})
	}
	for _, cause := range []error{errors.New("secret-editor"), context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			var output bytes.Buffer
			client, err := NewClient("http://example.invalid", WithLogger(slog.New(slog.NewJSONHandler(&output, nil))), WithRequestEditorFn(func(context.Context, *http.Request) error { return cause }))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.GetGroups(t.Context()); !errors.Is(err, cause) {
				t.Fatalf("error changed: %v", err)
			}
			records := logRecords(t, output.Bytes())
			want := "client"
			if cause == context.Canceled {
				want = "canceled"
			} else if cause == context.DeadlineExceeded {
				want = "deadline_exceeded"
			}
			if len(records) != 1 || records[0]["error_type"] != want || records[0]["status_code"] != nil || strings.Contains(output.String(), "secret") {
				t.Fatalf("wrong pre-request log: %s", output.String())
			}
		})
	}
}

func TestLoggingFileAndLevels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	if err := os.WriteFile(path, []byte("existing log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	level := new(slog.LevelVar)
	level.Set(slog.LevelError)
	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: level}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"s":"ok"}`)
	}))
	defer server.Close()
	client, err := NewClientWithResponses(server.URL, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetGroupsWithResponse(t.Context()); err != nil {
		t.Fatal(err)
	}
	level.Set(slog.LevelInfo)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if _, err := client.GetGroupsWithResponse(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	// The client must leave the caller's destination usable.
	logger.Info("application still owns file")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("existing log\n")) {
		t.Fatal("existing file was truncated")
	}
	records := logRecords(t, bytes.TrimPrefix(data, []byte("existing log\n")))
	if len(records) != 9 || records[8]["msg"] != "application still owns file" {
		t.Fatalf("filtering, concurrency, or ownership failed: %v", records)
	}
}

func TestLoggingWithTelemetry(t *testing.T) {
	fixture := newTelemetryFixture(t)
	var output bytes.Buffer
	client, err := NewClient("http://example.invalid", fixture.option,
		WithLogger(slog.New(slog.NewJSONHandler(&output, nil))),
		WithRequestEditorFn(func(context.Context, *http.Request) error { return errors.New("secret-editor") }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetGroups(t.Context()); err == nil {
		t.Fatal("missing editor error")
	}
	if len(logRecords(t, output.Bytes())) != 1 || len(fixture.spans.Ended()) != 1 || fixture.sum(t, "tradeview.client.operations") != 1 {
		t.Fatal("logging and telemetry must each complete once")
	}
	if _, err := NewClient("http://example.invalid", WithLogger(nil)); err == nil {
		t.Fatal("nil logger accepted")
	}
}

func TestLoggingStreamLifecycle(t *testing.T) {
	for _, mode := range []string{"eof", "api", "decode", "cancel", "close"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			var output bytes.Buffer
			client, err := NewClient(server.URL, WithLogger(slog.New(slog.NewJSONHandler(&output, nil))))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream, err := client.OpenStreamOrders(ctx, "secret-account", &StreamOrdersParams{})
			if err != nil {
				t.Fatal(err)
			}
			if !stream.Next() || output.Len() != 0 {
				t.Fatalf("early completion: %v", stream.Err())
			}
			if mode == "cancel" {
				cancel()
			}
			if mode == "close" {
				nextDone := make(chan struct{})
				go func() { stream.Next(); close(nextDone) }()
				var group sync.WaitGroup
				for range 8 {
					group.Go(func() { _ = stream.Close() })
				}
				group.Wait()
				<-nextDone
			} else {
				for stream.Next() {
				}
			}
			_ = stream.Close()
			records := logRecords(t, output.Bytes())
			category := map[string]string{"api": "api", "decode": "client", "cancel": "canceled"}[mode]
			if len(records) != 1 || records[0]["streaming"] != true || strings.Contains(output.String(), "secret") {
				t.Fatalf("wrong stream log: %s", output.String())
			}
			got, ok := records[0]["error_type"].(string)
			if got != category || (ok != (category != "")) {
				t.Fatalf("error_type=%q, want %q", got, category)
			}
		})
	}
}
