package tradeview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestEveryFiniteMethodRejectsMalformedJSONModels(t *testing.T) {
	contract := loadFixtureContract(t)
	count := 0
	for _, fixture := range _operationFixtures {
		entry := contract.manifest[fixture.id]
		if entry.Streaming {
			continue
		}
		count++
		t.Run(fixture.id, func(t *testing.T) {
			query, _ := contract.queryFixture(fixture.id, contract.operations[fixture.id])
			_, requestBody := contract.requestBody(contract.operations[fixture.id])
			payloads := []string{`null`, `[]`, `123`, `"ok"`, `true`, `{"s":"error","errtype":123,"errmsg":"denied"}`, `{"s":"error","errmsg":{}}`, `{"s":"error","errtype":null}`}
			if fixture.id != "getMapping" {
				payloads = append(payloads, `{}`, `{"d":[]}`, `{"s":null}`, `{"s":123}`, `{"s":"unexpected"}`)
			}
			for _, payload := range payloads {
				t.Run(payload, func(t *testing.T) {
					body := &finiteObservedBody{Reader: strings.NewReader(payload)}
					client, err := NewClientWithResponses("https://broker.example/api", WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
					})))
					if err != nil {
						t.Fatal(err)
					}
					results := invokeFixture(t, reflect.ValueOf(client).MethodByName(entry.TypedMethod), context.Background(), fixture.path, query, requestBody)
					var protocolErr *ProtocolError
					if results[1].IsNil() || !errors.As(results[1].Interface().(error), &protocolErr) || !results[0].IsNil() || !body.closed {
						t.Fatalf("result=%v error=%v closed=%v", results[0], results[1], body.closed)
					}
					if string(protocolErr.Body) != payload || protocolErr.StatusCode != 200 || protocolErr.ContentType != "application/json" {
						t.Fatalf("lost diagnostics: %+v", protocolErr)
					}
				})
			}
		})
	}
	if count != 38 {
		t.Fatalf("validated %d finite methods", count)
	}
}

func TestFiniteFixturePayloadTypeValidation(t *testing.T) {
	for _, operation := range finiteResponseOperations() {
		for _, variant := range operation.variants {
			t.Run(operation.id+"/"+variant.file, func(t *testing.T) {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(readResponseFixture(t, variant.file), &object); err != nil {
					t.Fatal(err)
				}
				// Corrupt a declared non-envelope top-level payload field. Generic
				// SuccessResponse fixtures have only s, covered by the inventory test.
				field := ""
				for _, name := range []string{"d", "symbols", "symbol", "t"} {
					if _, ok := object[name]; ok {
						field = name
						break
					}
				}
				if field == "" {
					return
				}
				object[field] = json.RawMessage(`true`)
				body, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				client, ctx := newResponseFixture(t, operation.request, 200, "application/json", body)
				_, err = operation.call(ctx, client)
				var protocolErr *ProtocolError
				var typeErr *json.UnmarshalTypeError
				if !errors.As(err, &protocolErr) || !errors.As(err, &typeErr) {
					t.Fatalf("accepted incompatible %s or lost cause: %v", field, err)
				}
			})
		}
	}
}

func TestFiniteNestedUnionsAndOverlappingSuccessShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		parse      func(*http.Response) error
	}{
		{"transaction hidden by generic success", `{"s":"ok","d":{"transactionId":123}}`, func(r *http.Response) error { _, err := ParseCancelOrderResponse(r); return err }},
		{"transaction object type", `{"s":"ok","d":[]}`, func(r *http.Response) error { _, err := ParseCancelOrderResponse(r); return err }},
		{"nested row union", `{"s":"ok","d":{"rows":[{"title":123}]}}`, func(r *http.Response) error { _, err := ParseOrderInfoResponse(r); return err }},
		{"nested markup union", `{"s":"ok","d":{"warnings":[["text",{"url":123}]]}}`, func(r *http.Response) error { _, err := ParsePreviewOrderResponse(r); return err }},
		{"folded field", `{"s":"ok","D":[{"ID":123}]}`, func(r *http.Response) error { _, err := ParseGetAccountsResponse(r); return err }},
		{"overwritten field", `{"s":"ok","d":[{"id":123,"id":"valid"}]}`, func(r *http.Response) error { _, err := ParseGetAccountsResponse(r); return err }},
		{"overwritten map entry", `{"s":"ok","d":[{"tradingOptionsSets":{"forex":123,"forex":{}}}]}`, func(r *http.Response) error { _, err := ParseGetAccountsResponse(r); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.parse(&http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))})
			var protocolErr *ProtocolError
			var typeErr *json.UnmarshalTypeError
			if !errors.As(err, &protocolErr) || !errors.As(err, &typeErr) {
				t.Fatalf("expected protocol/type error, got %v", err)
			}
		})
	}
}

func TestFiniteValidationPreservesCompatibilityAndObservations(t *testing.T) {
	for _, tc := range []struct{ payload, kind string }{
		{`{"s":"ok","d":[{"id":"a","isVerified":false,"brokerExtension":{"x":1}}],"extension":[null,123]}`, ""},
		{`{"s":"ok","d":null}`, ""},
		{`{"s":"error","errtype":"new-broker-code","errmsg":"denied"}`, "API"},
		{`{"s":"error","errtype":123}`, "protocol"},
		{`{"s":"ok","d":[{"id":123}]}`, "protocol"},
	} {
		t.Run(tc.payload, func(t *testing.T) {
			payload := tc.payload
			var output bytes.Buffer
			body := &streamObservedBody{Reader: strings.NewReader(payload)}
			telemetry := newTelemetryFixture(t)
			client, err := NewClientWithResponses("https://broker.example", telemetry.option, WithLogger(slog.New(slog.NewJSONHandler(&output, nil))), WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
			})))
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.GetAccountsWithResponse(t.Context(), nil)
			if tc.kind == "protocol" {
				var protocolErr *ProtocolError
				if result != nil || !errors.As(err, &protocolErr) {
					t.Fatalf("expected protocol failure: %v", err)
				}
			} else if tc.kind == "API" {
				var apiErr *APIError
				if result != nil || !errors.As(err, &apiErr) || apiErr.Code != "new-broker-code" {
					t.Fatalf("lost broker code: %v", err)
				}
			} else if err != nil || !bytes.Equal(result.Body, []byte(payload)) {
				t.Fatalf("compatibility changed: %v", err)
			}
			if body.closed.Load() != 1 || len(logRecords(t, output.Bytes())) != 1 || len(telemetry.spans.Ended()) != 1 {
				t.Fatalf("completion/close count changed: %d %s", body.closed.Load(), output.Bytes())
			}
			if telemetry.sum(t, "tradeview.client.operations") != 1 || telemetry.sum(t, "tradeview.client.active_operations") != 0 {
				t.Fatal("observation accounting is unbalanced")
			}
		})
	}
}
