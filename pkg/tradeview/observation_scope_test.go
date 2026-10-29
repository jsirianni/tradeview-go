package tradeview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type observationNestedKey struct{}
type observationValueKey struct{}

func assertObservationCounts(t *testing.T, output *bytes.Buffer, telemetry telemetryFixture, completed, active int64) {
	t.Helper()
	if got := len(logRecords(t, output.Bytes())); int64(got) != completed {
		t.Fatalf("completion logs = %d, want %d", got, completed)
	}
	if got := len(telemetry.spans.Ended()); int64(got) != completed {
		t.Fatalf("ended spans = %d, want %d", got, completed)
	}
	if got := telemetry.sum(t, "tradeview.client.operations"); got != completed {
		t.Fatalf("completed operations = %d, want %d", got, completed)
	}
	if got := telemetry.sum(t, "tradeview.client.active_operations"); got != active {
		t.Fatalf("active operations = %d, want %d", got, active)
	}
}

func observationResponse(body *streamObservedBody) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
}

func TestNestedObservationOwnership(t *testing.T) {
	for _, outerKind := range []string{"finite", "stream"} {
		for _, hook := range []string{"client editor", "request editor", "token source"} {
			for _, separate := range []bool{false, true} {
				for _, completion := range []string{"raw EOF", "raw Close", "typed", "typed error"} {
					t.Run(fmt.Sprintf("%s/%s/separate=%t/%s", outerKind, hook, separate, completion), func(t *testing.T) {
						telemetry := newTelemetryFixture(t)
						var output bytes.Buffer
						logger := WithLogger(slog.New(slog.NewJSONHandler(&output, nil)))
						ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), observationValueKey{}, "retained"), time.Minute)
						defer cancel()
						ctx, parent := telemetry.traces.Tracer("test").Start(ctx, "parent")
						defer parent.End()
						deadline, _ := ctx.Deadline()
						checkContext := func(got context.Context) {
							t.Helper()
							gotDeadline, ok := got.Deadline()
							if !ok || !gotDeadline.Equal(deadline) || got.Done() != ctx.Done() || got.Value(observationValueKey{}) != "retained" {
								t.Fatal("lost context deadline, cancellation, or value")
							}
						}
						nestedPayload := `{"s":"ok"}`
						if completion == "typed error" {
							nestedPayload = `{"s":"unexpected"}`
						}
						nestedBody := &streamObservedBody{Reader: strings.NewReader(nestedPayload)}
						outerPayload := `{"s":"ok"}`
						if outerKind == "stream" {
							outerPayload = "{\"heartbeat\":0}\n"
						}
						outerBody := &streamObservedBody{Reader: strings.NewReader(outerPayload)}
						doer := finiteResponseDoer(func(request *http.Request) (*http.Response, error) {
							checkContext(request.Context())
							if request.Context().Value(observationNestedKey{}) == true {
								return observationResponse(nestedBody), nil
							}
							return observationResponse(outerBody), nil
						})
						var nested *Client
						var outerSpan trace.SpanContext
						callback := func(got context.Context) error {
							if got.Value(observationNestedKey{}) == true {
								return nil
							}
							checkContext(got)
							outerSpan = trace.SpanContextFromContext(got)
							assertObservationCounts(t, &output, telemetry, 0, 1)
							nestedCtx := context.WithValue(got, observationNestedKey{}, true)
							if strings.HasPrefix(completion, "raw") {
								response, err := nested.GetGroups(nestedCtx)
								if err != nil {
									t.Fatal(err)
								}
								assertObservationCounts(t, &output, telemetry, 0, 2)
								if completion == "raw EOF" {
									if _, err := io.ReadAll(response.Body); err != nil {
										t.Fatal(err)
									}
									assertObservationCounts(t, &output, telemetry, 1, 1)
								}
								if err := response.Body.Close(); err != nil {
									t.Fatal(err)
								}
							} else {
								typed := &ClientWithResponses{ClientInterface: nested}
								response, err := typed.GetGroupsWithResponse(nestedCtx)
								if completion == "typed error" {
									var protocolErr *ProtocolError
									if response != nil || !errors.As(err, &protocolErr) {
										t.Fatalf("nested typed error = %v", err)
									}
								} else if err != nil {
									t.Fatal(err)
								}
							}
							assertObservationCounts(t, &output, telemetry, 1, 1)
							if nestedBody.closed.Load() != 1 {
								t.Fatal("nested body was not closed exactly once")
							}
							return nil
						}
						editor := func(got context.Context, _ *http.Request) error { return callback(got) }
						opts := []ClientOption{WithHTTPClient(doer), telemetry.option, logger}
						switch hook {
						case "client editor":
							opts = append(opts, WithRequestEditorFn(editor))
						case "token source":
							opts = append(opts, WithTokenSource(TokenSourceFunc(func(got context.Context) (string, error) { return "token", callback(got) })))
						}
						outer, err := NewClient("https://example.invalid", opts...)
						if err != nil {
							t.Fatal(err)
						}
						nested = outer
						if separate {
							nested, err = NewClient("https://other.invalid", WithHTTPClient(doer), telemetry.option, logger)
							if err != nil {
								t.Fatal(err)
							}
						}
						if outerKind == "finite" {
							var editors []RequestEditorFn
							if hook == "request editor" {
								editors = append(editors, editor)
							}
							_, err = (&ClientWithResponses{ClientInterface: outer}).GetGroupsWithResponse(ctx, editors...)
							if err != nil {
								t.Fatal(err)
							}
						} else {
							var options []StreamOption
							if hook == "request editor" {
								options = append(options, WithStreamRequestEditor(editor))
							}
							stream, err := outer.OpenStreaming(ctx, options...)
							if err != nil {
								t.Fatal(err)
							}
							assertObservationCounts(t, &output, telemetry, 1, 1)
							if !stream.Next() || stream.Next() || stream.Err() != nil {
								t.Fatalf("outer stream failed: %v", stream.Err())
							}
							if err := stream.Close(); err != nil {
								t.Fatal(err)
							}
						}
						assertObservationCounts(t, &output, telemetry, 2, 0)
						if outerBody.closed.Load() != 1 {
							t.Fatal("outer body was not closed exactly once")
						}
						spans := telemetry.spans.Ended()
						if spans[0].Parent().SpanID() != outerSpan.SpanID() || spans[1].Parent().SpanID() != parent.SpanContext().SpanID() {
							t.Fatal("nested trace parentage changed")
						}
						category := ""
						if completion == "typed error" {
							category = "protocol"
							if logRecords(t, output.Bytes())[0]["error_type"] != category {
								t.Fatal("typed parse error was lost at body EOF")
							}
						}
						if spanAttribute(spans[0], "error.type") != category || spanAttribute(spans[1], "error.type") != "" {
							t.Fatal("nested outcome contaminated the outer operation")
						}
					})
				}
			}
		}
	}
}

// An adapter can issue the same raw operation before forwarding the wrapper's
// request, so even a one-use context token could claim the wrong response.
type observationClientAdapter struct {
	ClientInterface
	before func(context.Context)
}

func (adapter observationClientAdapter) GetGroups(ctx context.Context, editors ...RequestEditorFn) (*http.Response, error) {
	adapter.before(ctx)
	return adapter.ClientInterface.GetGroups(ctx, editors...)
}

func TestObservationClientAdapterAndStandaloneParser(t *testing.T) {
	telemetry := newTelemetryFixture(t)
	var output bytes.Buffer
	var bodies []*streamObservedBody
	doer := finiteResponseDoer(func(*http.Request) (*http.Response, error) {
		body := &streamObservedBody{Reader: strings.NewReader(`{"s":"unexpected"}`)}
		bodies = append(bodies, body)
		return observationResponse(body), nil
	})
	client, err := NewClient("https://example.invalid", WithHTTPClient(doer), telemetry.option, WithLogger(slog.New(slog.NewJSONHandler(&output, nil))))
	if err != nil {
		t.Fatal(err)
	}
	adapter := observationClientAdapter{ClientInterface: client, before: func(ctx context.Context) {
		response, err := client.GetGroups(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(response.Body); err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		assertObservationCounts(t, &output, telemetry, 1, 0)
	}}
	_, err = (&ClientWithResponses{ClientInterface: adapter}).GetGroupsWithResponse(t.Context())
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("typed wrapper error = %v", err)
	}
	assertObservationCounts(t, &output, telemetry, 2, 0)
	response, err := client.GetGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseGetGroupsResponse(response)
	if !errors.As(err, &protocolErr) {
		t.Fatalf("standalone parser error = %v", err)
	}
	assertObservationCounts(t, &output, telemetry, 3, 0)
	records := logRecords(t, output.Bytes())
	if records[0]["error_type"] != nil || records[1]["error_type"] != "protocol" || records[2]["error_type"] != "protocol" {
		t.Fatalf("raw and parsed outcomes confused: %v", records)
	}
	for _, body := range bodies {
		if body.closed.Load() != 1 {
			t.Fatal("response body not closed exactly once")
		}
	}
}

func TestNestedObservationFailure(t *testing.T) {
	for _, source := range []string{"editor", "token source"} {
		for _, failure := range []string{"editor", "canceled", "deadline"} {
			for _, observeOuter := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/observeOuter=%t", source, failure, observeOuter), func(t *testing.T) {
					telemetry := newTelemetryFixture(t)
					var output bytes.Buffer
					logger := WithLogger(slog.New(slog.NewJSONHandler(&output, nil)))
					cause := errors.New("nested editor failure")
					category := "client"
					if failure == "canceled" {
						cause, category = context.Canceled, "canceled"
					} else if failure == "deadline" {
						cause, category = context.DeadlineExceeded, "deadline_exceeded"
					}
					doer := finiteResponseDoer(func(request *http.Request) (*http.Response, error) {
						if failure == "editor" {
							t.Fatal("dispatched after editor failure")
						}
						return nil, request.Context().Err()
					})
					nested, err := NewClient("https://nested.invalid", WithHTTPClient(doer), telemetry.option, logger)
					if err != nil {
						t.Fatal(err)
					}
					callback := func(ctx context.Context) error {
						var editor RequestEditorFn
						if failure == "editor" {
							editor = func(context.Context, *http.Request) error { return cause }
						} else {
							var cancel context.CancelFunc
							if failure == "canceled" {
								ctx, cancel = context.WithCancel(ctx)
							} else {
								ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
							}
							cancel()
							editor = func(context.Context, *http.Request) error { return nil }
						}
						_, err := nested.GetGroups(ctx, editor)
						if !errors.Is(err, cause) {
							t.Fatalf("nested error = %v, want %v", err, cause)
						}
						return err
					}
					opts := []ClientOption{WithHTTPClient(doer)}
					if observeOuter {
						opts = append(opts, telemetry.option, logger)
					}
					if source == "editor" {
						opts = append(opts, WithRequestEditorFn(func(ctx context.Context, _ *http.Request) error { return callback(ctx) }))
					} else {
						opts = append(opts, WithTokenSource(TokenSourceFunc(func(ctx context.Context) (string, error) { return "", callback(ctx) })))
					}
					outer, err := NewClientWithResponses("https://outer.invalid", opts...)
					if err != nil {
						t.Fatal(err)
					}
					_, err = outer.GetGroupsWithResponse(t.Context())
					if !errors.Is(err, cause) {
						t.Fatalf("outer error = %v, want %v", err, cause)
					}
					count := int64(1)
					if observeOuter {
						count++
					}
					assertObservationCounts(t, &output, telemetry, count, 0)
					for _, record := range logRecords(t, output.Bytes()) {
						if record["error_type"] != category {
							t.Fatalf("error category = %v, want %s", record["error_type"], category)
						}
					}
				})
			}
		}
	}
}

func TestNestedStreamObservation(t *testing.T) {
	for _, observeOuter := range []bool{false, true} {
		for _, typed := range []bool{false, true} {
			t.Run(fmt.Sprintf("observeOuter=%t/typed=%t", observeOuter, typed), func(t *testing.T) {
				telemetry := newTelemetryFixture(t)
				var output bytes.Buffer
				logger := WithLogger(slog.New(slog.NewJSONHandler(&output, nil)))
				body := &streamObservedBody{Reader: strings.NewReader("{\"heartbeat\":0}\n{\"s\":\"error\",\"errmsg\":\"denied\"}\n")}
				nested, err := NewClient("https://nested.invalid", telemetry.option, logger, WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
					return observationResponse(body), nil
				})))
				if err != nil {
					t.Fatal(err)
				}
				outerActive := int64(0)
				opts := []ClientOption{WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
					return observationResponse(&streamObservedBody{Reader: strings.NewReader(`{"s":"ok"}`)}), nil
				}))}
				if observeOuter {
					outerActive = 1
					opts = append(opts, telemetry.option, logger)
				}
				opts = append(opts, WithRequestEditorFn(func(ctx context.Context, _ *http.Request) error {
					if typed {
						stream, err := nested.OpenStreaming(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if !stream.Next() || stream.Value().Kind != StreamHeartbeat {
							t.Fatal("nested heartbeat was not yielded")
						}
						assertObservationCounts(t, &output, telemetry, 0, outerActive+1)
						if !stream.Next() || stream.Value().Kind != StreamError || stream.Next() || stream.Value().Kind != StreamError {
							t.Fatal("nested terminal event was not retained")
						}
						if _, ok := errors.AsType[*APIError](stream.Err()); !ok {
							t.Fatalf("nested stream error = %v", stream.Err())
						}
						if err := stream.Close(); err != nil {
							t.Fatal(err)
						}
					} else {
						response, err := nested.Streaming(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := io.ReadAll(response.Body); err != nil {
							t.Fatal(err)
						}
						if err := response.Body.Close(); err != nil {
							t.Fatal(err)
						}
					}
					assertObservationCounts(t, &output, telemetry, 1, outerActive)
					if body.closed.Load() != 1 {
						t.Fatal("nested body was not closed exactly once")
					}
					return nil
				}))
				outer, err := NewClientWithResponses("https://outer.invalid", opts...)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := outer.GetGroupsWithResponse(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertObservationCounts(t, &output, telemetry, 1+outerActive, 0)
				category := ""
				if typed {
					category = "api"
					if logRecords(t, output.Bytes())[0]["error_type"] != category {
						t.Fatal("nested stream error was lost")
					}
				}
				if spanAttribute(telemetry.spans.Ended()[0], "error.type") != category {
					t.Fatal("nested stream span outcome changed")
				}
			})
		}
	}
}
