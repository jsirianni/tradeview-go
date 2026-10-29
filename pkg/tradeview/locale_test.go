package tradeview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestLocaleDefaultWithGeneratedFiniteRequests(t *testing.T) {
	zero, cursor := StartTime(0), Cursor("opaque+/= token")
	count := float64(0)
	for _, tc := range []struct {
		name   string
		params *GetExecutionsParams
		query  url.Values
	}{
		{"nil params", nil, url.Values{"locale": {"en"}}},
		{"empty params", &GetExecutionsParams{}, url.Values{"locale": {"en"}}},
		{"zero filter", &GetExecutionsParams{StartTime: &zero}, url.Values{"locale": {"en"}, "startTime": {"0"}}},
		{"pagination", &GetExecutionsParams{StartTime: &zero, MaxCount: &count, Cursor: &cursor}, url.Values{"locale": {"en"}, "startTime": {"0"}, "maxCount": {"0"}, "cursor": {"opaque+/= token"}}},
		{"override", &GetExecutionsParams{Locale: "fr", StartTime: &zero, Cursor: &cursor}, url.Values{"locale": {"fr"}, "startTime": {"0"}, "cursor": {"opaque+/= token"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/accounts/acct/executions" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if !reflect.DeepEqual(r.URL.Query(), tc.query) {
					t.Errorf("query=%v want=%v", r.URL.Query(), tc.query)
				}
				w.Header().Set("Content-Type", "application/json")
				writeTestString(t, w, `{"s":"ok","d":[]}`)
			}))
			defer server.Close()
			client, err := NewClientWithResponses(server.URL, WithLocale("en"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.GetExecutionsWithResponse(context.Background(), "acct", tc.params)
			if err != nil {
				t.Fatal(err)
			}
			if response.JSON200 == nil {
				t.Fatal("missing response payload")
			}
		})
	}
}

func TestLocaleDefaultWithGeneratedStreams(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params *StreamOrdersParams
		locale string
	}{
		{"nil params", nil, "en"},
		{"empty locale", &StreamOrdersParams{}, "en"},
		{"override", &StreamOrdersParams{Locale: "fr"}, "fr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := url.Values{"locale": {tc.locale}}
				if !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("query=%v want=%v", r.URL.Query(), want)
				}
				w.Header().Set("Content-Type", "application/json")
				writeTestString(t, w, "{\"s\":\"ok\"}\n")
			}))
			defer server.Close()
			client, err := NewClient(server.URL, WithLocale("en"))
			if err != nil {
				t.Fatal(err)
			}
			stream, err := client.OpenStreamOrders(context.Background(), "acct", tc.params)
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestBody(t, stream)
			if !stream.Next() || stream.Value().Kind != StreamPing {
				t.Fatalf("missing ping: %v", stream.Err())
			}
			if stream.Next() || stream.Err() != nil {
				t.Fatalf("unexpected end: %v", stream.Err())
			}
		})
	}
}
