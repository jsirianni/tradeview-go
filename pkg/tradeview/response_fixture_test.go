package tradeview

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type responseRequest struct {
	method, path string
	query, form  url.Values
	jsonBody     string
}

type responseObservation struct {
	status  int
	body    []byte
	payload any
}

type responseVariant struct {
	schema, file string
	check        func(*testing.T, any)
}

type responseOperation struct {
	id       string
	request  responseRequest
	call     func(context.Context, *ClientWithResponses) (responseObservation, error)
	variants []responseVariant
}

func readResponseFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "responses", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Every scenario owns its server and transport. The handler only reports
// nonfatal errors: FailNow must never run in the HTTP handler goroutine.
func newResponseFixture(t *testing.T, want responseRequest, status int, contentType string, body []byte) (*ClientWithResponses, context.Context) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != want.method || r.URL.EscapedPath() != "/fixture-api"+want.path {
			t.Errorf("request = %s %s, want %s /fixture-api%s", r.Method, r.URL.EscapedPath(), want.method, want.path)
		}
		if !reflect.DeepEqual(r.URL.Query(), want.query) {
			t.Errorf("query = %v, want %v", r.URL.Query(), want.query)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-token" {
			t.Errorf("authorization = %q", got)
		}
		actual, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		switch {
		case want.form != nil:
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/x-www-form-urlencoded" {
				t.Errorf("form content type = %q: %v", mediaType, err)
			}
			form, err := url.ParseQuery(string(actual))
			if err != nil || !reflect.DeepEqual(form, want.form) {
				t.Errorf("form = %v, want %v: %v", form, want.form, err)
			}
		case want.jsonBody != "":
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				t.Errorf("JSON content type = %q: %v", mediaType, err)
			}
			var got, expected any
			if err := json.Unmarshal(actual, &got); err != nil {
				t.Error(err)
				return
			}
			if err := json.Unmarshal([]byte(want.jsonBody), &expected); err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("JSON body = %s, want %s", actual, want.jsonBody)
			}
		default:
			if len(actual) != 0 {
				t.Errorf("unexpected request body %q", actual)
			}
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		if _, err := w.Write(body); err != nil {
			t.Error(err)
		}
	}))
	transport := server.Client()
	transport.Timeout = 5 * time.Second
	t.Cleanup(func() {
		server.Close()
		transport.CloseIdleConnections()
		if got := calls.Load(); got != 1 {
			t.Errorf("requests = %d, want 1", got)
		}
	})
	client, err := NewClientWithResponses(server.URL+"/fixture-api", WithHTTPClient(transport), WithBearerToken("fixture-token"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return client, ctx
}

// Use the public union accessor, not a second decode into the expected model.
func checkResponseUnion[U, P any](decode func(U) (P, error), check func(*testing.T, P)) func(*testing.T, any) {
	return func(t *testing.T, value any) {
		t.Helper()
		union := fixtureValue[*U](t, value)
		if union == nil {
			t.Fatal("missing JSON200")
		}
		payload, err := decode(*union)
		if err != nil {
			t.Fatal(err)
		}
		check(t, payload)
	}
}

func checkResponseDirect[P any](check func(*testing.T, P)) func(*testing.T, any) {
	return func(t *testing.T, value any) {
		t.Helper()
		payload := fixtureValue[*P](t, value)
		if payload == nil {
			t.Fatal("missing JSON200")
		}
		check(t, *payload)
	}
}

func responseEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func responsePresent[T any](t *testing.T, value *T) T {
	t.Helper()
	if value == nil {
		t.Fatal("expected a present optional value")
	}
	return *value
}

//go:fix inline
func TestFiniteResponseFixtures(t *testing.T) {
	for _, operation := range finiteResponseOperations() {
		for _, variant := range operation.variants {
			t.Run(operation.id+"/"+variant.file, func(t *testing.T) {
				t.Parallel()
				body := readResponseFixture(t, variant.file)
				client, ctx := newResponseFixture(t, operation.request, http.StatusOK, "application/json", body)
				response, err := operation.call(ctx, client)
				if err != nil {
					t.Fatal(err)
				}
				responseEqual(t, response.status, http.StatusOK)
				if !bytes.Equal(response.body, body) {
					t.Fatal("raw response body was changed")
				}
				variant.check(t, response.payload)
			})
		}
	}
}

func TestFiniteResponseFixtureInventory(t *testing.T) {
	contract := loadFixtureContract(t)
	seen := make(map[string]bool)
	for _, operation := range finiteResponseOperations() {
		if seen[operation.id] {
			t.Fatalf("duplicate operation %s", operation.id)
		}
		seen[operation.id] = true
		entry, ok := contract.manifest[operation.id]
		if !ok || entry.Streaming {
			t.Fatalf("unknown finite operation %s", operation.id)
		}
		responseEqual(t, operation.request.method, entry.Method)
		responseEqual(t, operation.request.path, replaceResponsePath(entry.Path))
		// YAML accepts both a quoted status key and the upstream numeric key.
		var successResponse any
		switch responses := contract.operations[operation.id]["responses"].(type) {
		case map[string]any:
			successResponse = responses["200"]
		case map[any]any:
			successResponse = responses[200]
		default:
			t.Fatalf("unsupported responses map for %s", operation.id)
		}
		response := fixtureValue[map[string]any](t, successResponse)
		content := fixtureValue[map[string]any](t, response["content"])
		jsonMedia := fixtureValue[map[string]any](t, content["application/json"])
		schema := fixtureValue[map[string]any](t, jsonMedia["schema"])
		want := make(map[string]bool)
		var collect func(map[string]any)
		collect = func(schema map[string]any) {
			if ref, ok := schema["$ref"].(string); ok {
				name := strings.TrimPrefix(ref, "#/components/schemas/")
				if name != "ErrorResponse" {
					want[name] = true
				}
				return
			}
			for _, key := range []string{"oneOf", "anyOf"} {
				if choices, ok := schema[key].([]any); ok {
					for _, choice := range choices {
						collect(fixtureValue[map[string]any](t, choice))
					}
					return
				}
			}
			t.Fatalf("unsupported success schema for %s: %v", operation.id, schema)
		}
		collect(schema)
		got := make(map[string]bool)
		files := make(map[string]bool)
		for _, variant := range operation.variants {
			if files[variant.file] || variant.check == nil {
				t.Fatalf("invalid variant %s/%s", operation.id, variant.file)
			}
			files[variant.file] = true
			got[variant.schema] = true
			if !json.Valid(readResponseFixture(t, variant.file)) {
				t.Fatalf("invalid JSON: %s", variant.file)
			}
		}
		responseEqual(t, got, want)
	}
	finite := 0
	for id, entry := range contract.manifest {
		if entry.Streaming {
			continue
		}
		finite++
		if !seen[id] {
			t.Errorf("missing finite response fixture: %s", id)
		}
	}
	responseEqual(t, finite, 38)
	responseEqual(t, len(seen), finite)
}

func replaceResponsePath(path string) string {
	for _, name := range fixturePathNames(path) {
		value := "position-1"
		if name == "accountId" {
			value = "account-1"
		}
		if name == "orderId" {
			value = "order-1"
		}
		path = strings.ReplaceAll(path, "{"+name+"}", value)
	}
	return path
}
