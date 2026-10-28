package tradeview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type finiteObservedBody struct {
	io.Reader
	closed bool
}

func (b *finiteObservedBody) Close() error { b.closed = true; return nil }

func TestFiniteParserClosesBody(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		payload     string
		wantAPI     bool
		wantHTTP    bool
		wantFailure bool
	}{
		{"success", 200, `{"s":"ok","d":[]}`, false, false, false},
		{"API error", 200, `{"s":"error","errtype":"forbidden","errmsg":"denied"}`, true, false, true},
		{"HTTP error", 502, `upstream failed`, false, true, true},
		{"malformed JSON", 200, `{`, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &finiteObservedBody{Reader: strings.NewReader(tc.payload)}
			response := &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
			result, err := ParseGetAccountsResponse(response)
			if (err != nil) != tc.wantFailure {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			var apiErr *APIError
			var httpErr *HTTPError
			if errors.As(err, &apiErr) != tc.wantAPI || errors.As(err, &httpErr) != tc.wantHTTP {
				t.Fatalf("error type = %T: %v", err, err)
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
			if !tc.wantFailure && (result.JSON200 == nil || string(result.Body) != tc.payload) {
				t.Fatal("success lost typed/raw response")
			}
		})
	}
}

func TestOrderFormPrecisionAndPresence(t *testing.T) {
	zero := float64(0)
	body := PlaceOrderFormdataRequestBody{
		Instrument: "EUR/USD", Qty: 1.23456789012345, Side: "buy", Type: "limit", CurrentAsk: 1.25, CurrentBid: 1.24, LimitPrice: &zero,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/accounts/acct/orders" {
			t.Errorf("request: %s %s", r.Method, r.URL)
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		form, err := url.ParseQuery(string(data))
		if err != nil {
			t.Error(err)
		}
		if form.Get("qty") != "1.23456789012345" {
			t.Errorf("quantity rounded: %s", form.Get("qty"))
		}
		if form.Get("limitPrice") != "0" || form.Has("stopPrice") {
			t.Errorf("presence: %v", form)
		}
		if form.Get("routing") != "venue + A" {
			t.Errorf("custom fields: %v", form)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"s":"ok","d":{"orderId":"1"}}`)
	}))
	defer server.Close()
	client, err := NewClientWithResponses(server.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PlaceOrderWithFormdataBodyWithResponse(context.Background(), "acct", &PlaceOrderParams{Locale: "en"}, body, WithExtraOrderFields(url.Values{"routing": {"venue + A"}}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCancelAllOrderUnionAndDeleteForm(t *testing.T) {
	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		data, _ := io.ReadAll(r.Body)
		form, err := url.ParseQuery(string(data))
		if err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/accounts/acct/orders/cancelAll":
			if r.Method != http.MethodPost || !reflect.DeepEqual(form["orderIds"], []string{"order 1", "order/2"}) {
				t.Errorf("cancel union form: %s %v", r.Method, form)
			}
		case "/accounts/acct/positions/pos":
			if r.Method != http.MethodDelete || form.Get("amount") != "0" {
				t.Errorf("DELETE form: %s %v", r.Method, form)
			}
		default:
			t.Errorf("unexpected URL: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"s":"ok"}`)
	}))
	defer server.Close()
	client, _ := NewClientWithResponses(server.URL)
	var body CancelAllOrdersFormdataRequestBody
	if err := json.Unmarshal([]byte(`{"orderIds":["order 1","order/2"]}`), &body); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CancelAllOrdersWithFormdataBodyWithResponse(context.Background(), "acct", &CancelAllOrdersParams{Locale: "en"}, body); err != nil {
		t.Fatal(err)
	}
	zero := float64(0)
	if _, err := client.ClosePositionWithFormdataBodyWithResponse(context.Background(), "acct", "pos", &ClosePositionParams{Locale: "en"}, ClosePositionFormdataRequestBody{Amount: &zero}); err != nil {
		t.Fatal(err)
	}
	if seen.Load() != 2 {
		t.Fatalf("requests=%d", seen.Load())
	}
}

func TestSymbolsCSVAndCursorPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/accounts/acct/instruments":
			if !reflect.DeepEqual(query["symbols"], []string{"EURUSD,BTCUSD"}) {
				t.Errorf("symbols not CSV: %v", query)
			}
			io.WriteString(w, `{"s":"ok","d":[]}`)
		case "/accounts/acct/executions":
			if query.Get("startTime") != "0" || query.Get("endTime") != "100" || query.Get("maxCount") != "2" || query.Get("cursor") != "opaque+/= token" || query.Has("instrument") {
				t.Errorf("pagination: %v", query)
			}
			io.WriteString(w, `{"s":"ok","d":[],"nextPageCursor":"next+/="}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, _ := NewClientWithResponses(server.URL)
	symbols := []string{"EURUSD", "BTCUSD"}
	if _, err := client.GetInstrumentsWithResponse(context.Background(), "acct", &GetInstrumentsParams{Locale: "en", Symbols: &symbols}); err != nil {
		t.Fatal(err)
	}
	start, end, count, cursor := StartTime(0), EndTime(100), float64(2), Cursor("opaque+/= token")
	response, err := client.GetExecutionsWithResponse(context.Background(), "acct", &GetExecutionsParams{Locale: "en", StartTime: &start, EndTime: &end, MaxCount: &count, Cursor: &cursor})
	if err != nil {
		t.Fatal(err)
	}
	if response.JSON200 == nil {
		t.Fatal("missing typed pagination response")
	}
	payload, err := response.JSON200.AsExecutionsResponse()
	if err != nil {
		t.Fatal(err)
	}
	if payload.NextPageCursor == nil || *payload.NextPageCursor != "next+/=" {
		t.Fatalf("cursor lost: %+v", payload)
	}
}

func TestOpaquePathIdentifiers(t *testing.T) {
	for _, tc := range []struct{ id, escaped string }{
		{".", "%2E"}, {"..", "%2E%2E"}, {"a/b", "a%2Fb"}, {"a%b", "a%25b"},
		{"a?b", "a%3Fb"}, {"a#b", "a%23b"}, {"a b", "a%20b"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			request, err := NewGetOrdersRequest("https://broker.example/api/", PathAccountId(tc.id), &GetOrdersParams{Locale: "en"})
			if err != nil {
				t.Fatal(err)
			}
			expected := "/api/accounts/" + tc.escaped + "/orders"
			if request.URL.EscapedPath() != expected {
				t.Fatalf("path = %s; want %s", request.URL.EscapedPath(), expected)
			}
		})
	}
	request, err := NewCancelOrderRequest("https://broker.example/api/", ".", "..", &CancelOrderParams{Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got := request.URL.EscapedPath(); got != "/api/accounts/%2E/orders/%2E%2E" {
		t.Fatalf("multiple opaque identifiers resolved as paths: %s", got)
	}
}
