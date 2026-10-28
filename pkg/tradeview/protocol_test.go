package tradeview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type finiteResponseDoer func(*http.Request) (*http.Response, error)

func (f finiteResponseDoer) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestEveryFiniteMethodRejectsUnexpectedResponses(t *testing.T) {
	contract := loadFixtureContract(t)
	finiteCount := 0
	for _, fixture := range _operationFixtures {
		entry := contract.manifest[fixture.id]
		if entry.Streaming {
			continue
		}
		finiteCount++
		t.Run(fixture.id, func(t *testing.T) {
			query, _ := contract.queryFixture(fixture.id, contract.operations[fixture.id])
			_, requestBody := contract.requestBody(contract.operations[fixture.id])
			for _, tc := range []struct {
				name, contentType, payload string
				status                     int
			}{
				{"HTML", "text/html", "<html>Login required</html>", http.StatusOK},
				{"undeclared status", "application/json", `{"s":"ok"}`, http.StatusCreated},
			} {
				t.Run(tc.name, func(t *testing.T) {
					body := &finiteObservedBody{Reader: strings.NewReader(tc.payload)}
					client, err := NewClientWithResponses("https://broker.example/api", WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.contentType}}, Body: body}, nil
					})))
					if err != nil {
						t.Fatal(err)
					}
					results := invokeFixture(t, reflect.ValueOf(client).MethodByName(entry.TypedMethod), context.Background(), fixture.path, query, requestBody)
					if results[1].IsNil() {
						t.Fatal("unexpected response appeared successful")
					}
					var protocolErr *ProtocolError
					if !errors.As(results[1].Interface().(error), &protocolErr) || protocolErr.StatusCode != tc.status || protocolErr.ContentType != tc.contentType || string(protocolErr.Body) != tc.payload {
						t.Fatalf("wrong error: %v", results[1].Interface())
					}
					if !results[0].IsNil() || !body.closed {
						t.Fatalf("result nil=%v, body closed=%v", results[0].IsNil(), body.closed)
					}
				})
			}
		})
	}
	if finiteCount != 38 {
		t.Fatalf("checked %d finite operations, want 38", finiteCount)
	}
}

func TestFiniteProtocolResponses(t *testing.T) {
	const success = `{"s":"ok","d":[]}`
	const brokerError = `{"s":"error","errtype":"forbidden","errmsg":"denied"}`
	for _, tc := range []struct {
		name, contentType, payload, kind string
		status                           int
		syntaxError                      bool
	}{
		{"JSON", "application/json", success, "", 200, false},
		{"JSON parameters and case", "Application/JSON; Charset=utf-8", success, "", 200, false},
		{"JSON suffix", "application/vnd.broker+json; charset=UTF-8", success, "", 200, false},
		{"JSON problem media type", "application/problem+json", success, "", 200, false},
		{"HTML", "text/html", "<html>Login required</html>", "protocol", 200, false},
		{"absent media type", "", success, "protocol", 200, false},
		{"wrong media type", "text/plain", success, "protocol", 200, false},
		{"misleading media type", "application/notjson", success, "protocol", 200, false},
		{"malformed media type", "application/json; charset", success, "protocol", 200, false},
		{"empty JSON suffix subtype", "application/+json", success, "protocol", 200, false},
		{"malformed JSON", "application/json", `{"s":`, "protocol", 200, true},
		{"empty JSON", "application/json", "", "protocol", 200, true},
		{"malformed HTML payload", "text/html", `{`, "protocol", 200, false},
		{"created", "application/json", success, "protocol", 201, false},
		{"no content", "application/json", "", "protocol", 204, false},
		{"partial content", "application/json", success, "protocol", 206, false},
		{"undeclared broker error", "application/json", brokerError, "protocol", 201, false},
		{"broker error", "application/json", brokerError, "API", 200, false},
		{"broker error media precedence", "text/html", brokerError, "API", 200, false},
		{"HTTP error", "text/html", "<html>Unavailable</html>", "HTTP", 503, false},
		{"HTTP error envelope", "application/json", brokerError, "HTTP", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &finiteObservedBody{Reader: strings.NewReader(tc.payload)}
			rsp := &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: body}
			if tc.contentType != "" {
				rsp.Header.Set("Content-Type", tc.contentType)
			}
			result, err := ParseGetAccountsResponse(rsp)
			if !body.closed {
				t.Fatal("response body was not closed")
			}
			var protocolErr *ProtocolError
			var apiErr *APIError
			var httpErr *HTTPError
			if errors.As(err, &protocolErr) != (tc.kind == "protocol") || errors.As(err, &apiErr) != (tc.kind == "API") || errors.As(err, &httpErr) != (tc.kind == "HTTP") || (err == nil) != (tc.kind == "") {
				t.Fatalf("result=%+v, error=%T: %v", result, err, err)
			}
			var syntaxErr *json.SyntaxError
			if errors.As(err, &syntaxErr) != tc.syntaxError {
				t.Fatalf("JSON error was not preserved: %T: %v", err, err)
			}
			if protocolErr != nil && (protocolErr.StatusCode != tc.status || protocolErr.ContentType != tc.contentType || string(protocolErr.Body) != tc.payload || protocolErr.Err == nil) {
				t.Fatalf("missing response diagnostic: %+v", protocolErr)
			}
			if tc.kind != "" {
				if result != nil {
					t.Fatal("error returned a successful response wrapper")
				}
				return
			}
			if result.JSON200 == nil || string(result.Body) != tc.payload {
				t.Fatal("success lost typed or raw response")
			}
			account, err := result.JSON200.AsAccountResponse()
			if err != nil || account.S != "ok" || account.D == nil {
				t.Fatalf("success union changed: %+v, %v", account, err)
			}
		})
	}
}

func TestProtocolErrorRetainsOwnedDiagnostics(t *testing.T) {
	body := []byte("diagnostic response payload")
	rsp := &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"Content-Type": {"text/html"}}}
	cause := errors.New("unexpected success status")
	protocolErr := newProtocolError(body, rsp, cause)
	body[0] = 'x'
	if string(protocolErr.Body) != "diagnostic response payload" || !errors.Is(protocolErr, cause) {
		t.Fatalf("lost owned body or cause: %+v", protocolErr)
	}
	if !strings.Contains(protocolErr.Error(), "201") || !strings.Contains(protocolErr.Error(), cause.Error()) || strings.Contains(protocolErr.Error(), string(protocolErr.Body)) {
		t.Fatalf("unexpected error diagnostic: %s", protocolErr.Error())
	}
}

type finiteFailingReader struct{ err error }

func (r finiteFailingReader) Read([]byte) (int, error) { return 0, r.err }

func TestFiniteReadFailureClosesBody(t *testing.T) {
	cause := errors.New("response interrupted")
	body := &finiteObservedBody{Reader: finiteFailingReader{err: cause}}
	_, err := ParseGetAccountsResponse(&http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: body})
	if !errors.Is(err, cause) || !body.closed {
		t.Fatalf("error=%v, body closed=%v", err, body.closed)
	}
}

func TestRawMethodPreservesArbitraryResponse(t *testing.T) {
	const payload = "<html>Login required</html>"
	body := &finiteObservedBody{Reader: strings.NewReader(payload)}
	client, err := NewClient("https://broker.example/api", WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: body}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	rsp, err := client.GetAccounts(context.Background(), nil)
	if err != nil || rsp == nil || body.closed {
		t.Fatalf("raw request changed: response=%+v, error=%v, closed=%v", rsp, err, body.closed)
	}
	defer rsp.Body.Close()
	data, err := io.ReadAll(rsp.Body)
	if err != nil || !bytes.Equal(data, []byte(payload)) {
		t.Fatalf("raw payload=%q, error=%v", data, err)
	}
}
