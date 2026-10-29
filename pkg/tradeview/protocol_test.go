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
				{name: "HTML", contentType: "text/html", payload: "<html>Login required</html>", status: http.StatusOK},
				{name: "undeclared status", contentType: "application/json", payload: `{"s":"ok"}`, status: http.StatusCreated},
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
		{name: "JSON", contentType: "application/json", payload: success, kind: "", status: 200, syntaxError: false},
		{name: "JSON parameters and case", contentType: "Application/JSON; Charset=utf-8", payload: success, kind: "", status: 200, syntaxError: false},
		{name: "JSON suffix", contentType: "application/vnd.broker+json; charset=UTF-8", payload: success, kind: "", status: 200, syntaxError: false},
		{name: "JSON problem media type", contentType: "application/problem+json", payload: success, kind: "", status: 200, syntaxError: false},
		{name: "HTML", contentType: "text/html", payload: "<html>Login required</html>", kind: "protocol", status: 200, syntaxError: false},
		{name: "absent media type", contentType: "", payload: success, kind: "protocol", status: 200, syntaxError: false},
		{name: "wrong media type", contentType: "text/plain", payload: success, kind: "protocol", status: 200, syntaxError: false},
		{name: "misleading media type", contentType: "application/notjson", payload: success, kind: "protocol", status: 200, syntaxError: false},
		{name: "malformed media type", contentType: "application/json; charset", payload: success, kind: "protocol", status: 200, syntaxError: false},
		{name: "empty JSON suffix subtype", contentType: "application/+json", payload: success, kind: "protocol", status: 200, syntaxError: false},
		{name: "malformed JSON", contentType: "application/json", payload: `{"s":`, kind: "protocol", status: 200, syntaxError: true},
		{name: "empty JSON", contentType: "application/json", payload: "", kind: "protocol", status: 200, syntaxError: true},
		{name: "malformed HTML payload", contentType: "text/html", payload: `{`, kind: "protocol", status: 200, syntaxError: false},
		{name: "created", contentType: "application/json", payload: success, kind: "protocol", status: 201, syntaxError: false},
		{name: "no content", contentType: "application/json", payload: "", kind: "protocol", status: 204, syntaxError: false},
		{name: "partial content", contentType: "application/json", payload: success, kind: "protocol", status: 206, syntaxError: false},
		{name: "undeclared broker error", contentType: "application/json", payload: brokerError, kind: "protocol", status: 201, syntaxError: false},
		{name: "broker error", contentType: "application/json", payload: brokerError, kind: "API", status: 200, syntaxError: false},
		{name: "broker error media precedence", contentType: "text/html", payload: brokerError, kind: "API", status: 200, syntaxError: false},
		{name: "HTTP error", contentType: "text/html", payload: "<html>Unavailable</html>", kind: "HTTP", status: 503, syntaxError: false},
		{name: "HTTP error envelope", contentType: "application/json", payload: brokerError, kind: "HTTP", status: 403, syntaxError: false},
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
	defer closeTestBody(t, rsp.Body)
	data, err := io.ReadAll(rsp.Body)
	if err != nil || !bytes.Equal(data, []byte(payload)) {
		t.Fatalf("raw payload=%q, error=%v", data, err)
	}
}
