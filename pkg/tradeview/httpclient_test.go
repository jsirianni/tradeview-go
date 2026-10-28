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

func TestDefaultClientRejectsCrossOriginCredentialReplay(t *testing.T) {
	for _, status := range []int{307, 308} {
		for _, differentHost := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/differentHost=%t", status, differentHost), func(t *testing.T) {
				var contacts atomic.Int32
				destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					contacts.Add(1)
					t.Error("cross-origin destination received a request")
				}))
				defer destination.Close()
				target := destination.URL
				if differentHost {
					target = strings.Replace(target, "127.0.0.1", "localhost", 1)
				}
				source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, target+"/collect", status)
				}))
				defer source.Close()
				client, err := NewClient(source.URL)
				if err != nil {
					t.Fatal(err)
				}
				client.Client.(*http.Client).Transport = source.Client().Transport
				response, err := client.AuthorizeWithFormdataBody(context.Background(), AuthorizeFormdataRequestBody{Login: "test-user", Password: "test-password"})
				if response != nil {
					response.Body.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "refusing cross-origin redirect") {
					t.Fatalf("expected cross-origin rejection, got %v", err)
				}
				if contacts.Load() != 0 {
					t.Fatalf("destination contacted %d times", contacts.Load())
				}
			})
		}
	}
}

func TestRedirectOriginComparison(t *testing.T) {
	for _, tt := range []struct {
		origin, target string
		allowed        bool
	}{
		{"https://broker.example/a", "https://BROKER.example:443/b", true},
		{"http://broker.example", "http://broker.example:80/b", true},
		{"https://broker.example", "https://broker.example:444/b", false},
		{"https://broker.example", "https://other.example/b", false},
		{"http://broker.example:443", "https://broker.example/b", false},
	} {
		t.Run(tt.target, func(t *testing.T) {
			original, err := http.NewRequest(http.MethodPost, tt.origin, nil)
			if err != nil {
				t.Fatal(err)
			}
			next, err := http.NewRequest(http.MethodPost, tt.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = defaultHTTPClient().CheckRedirect(next, []*http.Request{original})
			if (err == nil) != tt.allowed {
				t.Fatalf("allowed=%t error=%v", tt.allowed, err)
			}
		})
	}
}

func TestDefaultClientPermitsSameOriginCredentialRedirects(t *testing.T) {
	for _, status := range []int{307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var contacts atomic.Int32
			secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				contacts.Add(1)
				if r.URL.Path == "/authorize" {
					http.Redirect(w, r, "/redirected", status)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || string(body) != "login=test-user&password=test-password" {
					t.Errorf("redirected method=%s body=%q", r.Method, body)
				}
				io.WriteString(w, `{"s":"ok"}`)
			}))
			defer secure.Close()
			client, err := NewClient(secure.URL)
			if err != nil {
				t.Fatal(err)
			}
			client.Client.(*http.Client).Transport = secure.Client().Transport
			response, err := client.AuthorizeWithFormdataBody(context.Background(), AuthorizeFormdataRequestBody{Login: "test-user", Password: "test-password"})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK || contacts.Load() != 2 {
				t.Fatalf("status=%d contacts=%d", response.StatusCode, contacts.Load())
			}
		})
	}
}
