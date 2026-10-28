package tradeview

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDefaultClientRejectsDowngradeRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308} {
		for _, intermediate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/intermediate=%t", status, intermediate), func(t *testing.T) {
				var contacts atomic.Int32
				var credentials atomic.Bool
				plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					contacts.Add(1)
					credentials.Store(r.Header.Get("Authorization") != "")
					io.WriteString(w, `{"s":"ok","d":[]}`)
				}))
				defer plain.Close()
				secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if intermediate && r.URL.Path == "/accounts" {
						http.Redirect(w, r, "/intermediate", status)
						return
					}
					http.Redirect(w, r, plain.URL+"/accounts", status)
				}))
				defer secure.Close()
				client, err := NewClient(secure.URL, WithBearerToken("test-token"))
				if err != nil {
					t.Fatal(err)
				}
				// Trust only the local TLS fixture; exercise the default policy.
				client.Client.(*http.Client).Transport = secure.Client().Transport
				response, err := client.GetAccounts(context.Background(), nil)
				if response != nil && response.Body != nil {
					response.Body.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "refusing HTTPS-to-HTTP redirect") {
					t.Fatalf("expected downgrade rejection, got %v", err)
				}
				if contacts.Load() != 0 || credentials.Load() {
					t.Fatalf("HTTP destination contacted %d times; credentials=%t", contacts.Load(), credentials.Load())
				}
			})
		}
	}
}

func TestDefaultClientPermitsHTTPSRedirects(t *testing.T) {
	var contacts atomic.Int32
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacts.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token on HTTPS request")
		}
		if r.URL.Path == "/accounts" {
			http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"s":"ok","d":[]}`)
	}))
	defer secure.Close()
	client, err := NewClient(secure.URL, WithBearerToken("test-token"))
	if err != nil {
		t.Fatal(err)
	}
	client.Client.(*http.Client).Transport = secure.Client().Transport
	response, err := client.GetAccounts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || contacts.Load() != 2 {
		t.Fatalf("status=%d requests=%d", response.StatusCode, contacts.Load())
	}
}

func TestDefaultClientRetainsRedirectLimit(t *testing.T) {
	var contacts atomic.Int32
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacts.Add(1)
		http.Redirect(w, r, "/accounts", http.StatusTemporaryRedirect)
	}))
	defer secure.Close()
	client, err := NewClient(secure.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.Client.(*http.Client).Transport = secure.Client().Transport
	response, err := client.GetAccounts(context.Background(), nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") || contacts.Load() != 10 {
		t.Fatalf("requests=%d error=%v", contacts.Load(), err)
	}
}

func TestProvidedHTTPClientKeepsRedirectPolicy(t *testing.T) {
	provided := &http.Client{}
	client, err := NewClient("https://broker.example", WithHTTPClient(provided))
	if err != nil {
		t.Fatal(err)
	}
	if client.Client != provided || provided.CheckRedirect != nil {
		t.Fatal("modified caller-provided HTTP client")
	}
}
