package tradeview

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestFiniteResponseFixtureFailures(t *testing.T) {
	request := responseRequest{method: http.MethodGet, path: "/accounts", query: url.Values{"locale": {"en"}}}
	for _, scenario := range []struct {
		name, contentType string
		status            int
		body              []byte
		check             func(*testing.T, error)
	}{
		{
			name: "malformed JSON", status: http.StatusOK, contentType: "application/json", body: []byte(`{"s":`),
			check: func(t *testing.T, err error) {
				var protocolErr *ProtocolError
				var syntaxErr *json.SyntaxError
				if !errors.As(err, &protocolErr) || !errors.As(err, &syntaxErr) {
					t.Fatalf("expected protocol/syntax error, got %T: %v", err, err)
				}
				responseEqual(t, protocolErr.StatusCode, http.StatusOK)
				responseEqual(t, protocolErr.ContentType, "application/json")
				responseEqual(t, string(protocolErr.Body), `{"s":`)
			},
		},
		{
			name: "HTTP error", status: http.StatusServiceUnavailable, contentType: "text/plain", body: []byte("fixture unavailable"),
			check: func(t *testing.T, err error) {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) {
					t.Fatalf("expected HTTPError, got %T: %v", err, err)
				}
				responseEqual(t, httpErr.StatusCode, http.StatusServiceUnavailable)
				responseEqual(t, string(httpErr.Body), "fixture unavailable")
			},
		},
		{
			name: "HTTP 200 broker error", status: http.StatusOK, contentType: "application/json", body: readResponseFixture(t, "broker-error"),
			check: func(t *testing.T, err error) {
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected APIError, got %T: %v", err, err)
				}
				responseEqual(t, apiErr.StatusCode, http.StatusOK)
				responseEqual(t, apiErr.Code, "forbidden")
				responseEqual(t, apiErr.Message, "fixture denied")
				responseEqual(t, apiErr.Body, readResponseFixture(t, "broker-error"))
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			client, ctx := newResponseFixture(t, request, scenario.status, scenario.contentType, scenario.body)
			response, err := client.GetAccountsWithResponse(ctx, &GetAccountsParams{Locale: "en"})
			if response != nil || err == nil {
				t.Fatalf("response = %+v, error = %v; want nil response and error", response, err)
			}
			scenario.check(t, err)
		})
	}
}

func TestFiniteResponseFixtureDeferredTypeError(t *testing.T) {
	body := readResponseFixture(t, "wrong-account-field")
	request := responseRequest{method: http.MethodGet, path: "/accounts", query: url.Values{"locale": {"en"}}}
	client, ctx := newResponseFixture(t, request, http.StatusOK, "application/json", body)
	response, err := client.GetAccountsWithResponse(ctx, &GetAccountsParams{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	responseEqual(t, response.StatusCode(), http.StatusOK)
	responseEqual(t, response.Body, body)
	if response.JSON200 == nil {
		t.Fatal("missing response union")
	}
	// The HTTP parser stores this valid JSON in a union. Its concrete account
	// shape is decoded only when the caller selects the public accessor.
	_, err = response.JSON200.AsAccountResponse()
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("expected field type error from accessor, got %T: %v", err, err)
	}
	responseEqual(t, typeErr.Field, "d.0.id")
	responseEqual(t, typeErr.Value, "number")
}
