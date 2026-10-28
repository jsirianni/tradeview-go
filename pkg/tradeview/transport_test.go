package tradeview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestResponseError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		api    bool
		http   bool
	}{
		{"success", 200, `{"s":"ok","d":[]}`, false, false},
		{"broker error", 200, `{"s":"error","errtype":"invalid_cursor","errmsg":"cursor expired"}`, true, false},
		{"unknown error code", 200, `{"s":"error","errtype":"new_code","errmsg":"message"}`, true, false},
		{"HTTP error", 429, `{"s":"error","errtype":"rate_limit","errmsg":"slow down"}`, false, true},
		{"plain HTTP error", 503, `unavailable`, false, true},
		{"bad JSON left to parser", 200, `{`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.body)
			err := responseError(data, &http.Response{StatusCode: tc.status, Status: http.StatusText(tc.status)})
			var apiErr *APIError
			var httpErr *HTTPError
			if errors.As(err, &apiErr) != tc.api {
				t.Fatalf("API error = %v", err)
			}
			if errors.As(err, &httpErr) != tc.http {
				t.Fatalf("HTTP error = %v", err)
			}
			if err == nil && (tc.api || tc.http) {
				t.Fatal("expected error")
			}
			if apiErr != nil {
				if apiErr.StatusCode != tc.status || apiErr.Message == "" || apiErr.Code == "" {
					t.Fatalf("incomplete APIError: %+v", apiErr)
				}
				data[0] = 'x'
				if string(apiErr.Body) != tc.body {
					t.Fatal("APIError aliases caller body")
				}
			}
			if httpErr != nil {
				if httpErr.StatusCode != tc.status {
					t.Fatalf("HTTPError: %+v", httpErr)
				}
				data[0] = 'x'
				if string(httpErr.Body) != tc.body {
					t.Fatal("HTTPError aliases caller body")
				}
			}
		})
	}
}

func TestTokenSourceAndLocale(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request value")
	calls := 0
	client := &Client{}
	source := TokenSourceFunc(func(got context.Context) (string, error) {
		if got.Value(contextKey{}) != "request value" {
			t.Fatal("lost request context")
		}
		calls++
		return "token", nil
	})
	if err := WithTokenSource(source)(client); err != nil {
		t.Fatal(err)
	}
	if err := WithLocale("en")(client); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "?locale=fr"} {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://broker.example/api/accounts"+query, nil)
		for _, edit := range client.RequestEditors {
			if err := edit(ctx, request); err != nil {
				t.Fatal(err)
			}
		}
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatal("missing authorization")
		}
		expected := "en"
		if query != "" {
			expected = "fr"
		}
		if request.URL.Query().Get("locale") != expected {
			t.Fatalf("query: %s", request.URL.RawQuery)
		}
	}
	if calls != 2 {
		t.Fatalf("token calls = %d, want one per request", calls)
	}
}

func TestTokenSourceFailures(t *testing.T) {
	if err := WithTokenSource(nil)(&Client{}); err == nil {
		t.Fatal("accepted nil source")
	}
	if err := WithLocale(" ")(&Client{}); err == nil {
		t.Fatal("accepted empty locale")
	}
	failure := errors.New("expired")
	for _, tc := range []struct {
		name     string
		source   TokenSource
		expected error
	}{
		{"caller error", TokenSourceFunc(func(context.Context) (string, error) { return "", failure }), failure},
		{"empty", TokenSourceFunc(func(context.Context) (string, error) { return "", nil }), nil},
		{"line breaks", TokenSourceFunc(func(context.Context) (string, error) { return "token\r\nInjected: yes", nil }), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &Client{}
			if err := WithTokenSource(tc.source)(client); err != nil {
				t.Fatal(err)
			}
			request, _ := http.NewRequest(http.MethodGet, "https://broker.example", nil)
			err := client.RequestEditors[0](request.Context(), request)
			if err == nil {
				t.Fatal("accepted invalid token")
			}
			if tc.expected != nil && !errors.Is(err, tc.expected) {
				t.Fatalf("lost underlying error: %v", err)
			}
		})
	}
}

func TestEncodeForm(t *testing.T) {
	zero := float64(0)
	no := false
	body := struct {
		Zero    *float64         `json:"zero,omitempty"`
		Omitted *float64         `json:"omitted,omitempty"`
		Enabled *bool            `json:"enabled,omitempty"`
		Text    string           `json:"text"`
		IDs     []string         `json:"orderIds"`
		Levels  []map[string]any `json:"exitLevels"`
	}{Zero: &zero, Enabled: &no, Text: "EUR/USD + x", IDs: []string{"one", "two"}, Levels: []map[string]any{{"qty": 1.25, "stopLoss": float64(0)}}}
	encoded, err := encodeForm(body)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("zero") != "0" || values.Get("enabled") != "false" || values.Has("omitted") {
		t.Fatalf("presence lost: %v", values)
	}
	if values.Get("text") != body.Text || !reflect.DeepEqual(values["orderIds"], body.IDs) {
		t.Fatalf("scalars/arrays: %v", values)
	}
	var levels []map[string]any
	if err := json.Unmarshal([]byte(values.Get("exitLevels")), &levels); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(levels, body.Levels) {
		t.Fatalf("nested form = %s", values.Get("exitLevels"))
	}
	for _, invalid := range []any{nil, 3, "string", []int{1}} {
		if _, err := encodeForm(invalid); err == nil {
			t.Fatalf("accepted non-object: %#v", invalid)
		}
	}
}

func TestExtraOrderFields(t *testing.T) {
	original := url.Values{"customRouting": {"venue A", "venue B"}}
	edit := WithExtraOrderFields(original)
	original.Set("customRouting", "mutated")
	request, _ := http.NewRequest(http.MethodPost, "https://broker.example/accounts/a/orders", strings.NewReader("qty=0&instrument=EURUSD"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	if err := edit(request.Context(), request); err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(request.Body)
	values, _ := url.ParseQuery(string(data))
	if values.Get("qty") != "0" || !reflect.DeepEqual(values["customRouting"], []string{"venue A", "venue B"}) {
		t.Fatalf("merged: %v", values)
	}
	if request.ContentLength != int64(len(data)) {
		t.Fatal("stale content length")
	}
	replay, err := request.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	repeated, _ := io.ReadAll(replay)
	if string(data) != string(repeated) {
		t.Fatal("GetBody does not replay modified form")
	}
}

func TestExtraOrderFieldsRejectCollisions(t *testing.T) {
	for _, key := range []string{"qty", "instrument", "currentAsk", "side", "type", "exitLevels", "confirmId", "orderInfoId", "customExisting", ""} {
		t.Run(key, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodPost, "https://broker.example", strings.NewReader("qty=1&customExisting=value"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := WithExtraOrderFields(url.Values{key: {"override"}})(request.Context(), request); err == nil {
				t.Fatal("accepted collision")
			}
		})
	}
	request, _ := http.NewRequest(http.MethodPost, "https://broker.example", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	if err := WithExtraOrderFields(url.Values{"custom": {"value"}})(request.Context(), request); err == nil {
		t.Fatal("accepted non-form")
	}
}
