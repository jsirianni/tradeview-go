package tradeview

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type countingResponseBody struct {
	remaining, read int64
	closed          bool
}

func (b *countingResponseBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), b.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	b.remaining -= n
	b.read += n
	return int(n), nil
}
func (b *countingResponseBody) Close() error { b.closed = true; return nil }

func TestEveryFiniteMethodBoundsResponse(t *testing.T) {
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
			for _, status := range []int{200, 502} {
				body := &countingResponseBody{remaining: 1 << 20}
				client, err := NewClientWithResponses("https://broker.example/api", WithFiniteResponseLimit(32), WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, ContentLength: -1, Body: body}, nil
				})))
				if err != nil {
					t.Fatal(err)
				}
				results := invokeFixture(t, reflect.ValueOf(client).MethodByName(entry.TypedMethod), context.Background(), fixture.path, query, requestBody)
				if results[1].IsNil() {
					t.Fatal("missing size error")
				}
				resultErr, ok := results[1].Interface().(error)
				if !ok {
					t.Fatalf("unexpected result error type: %T", results[1].Interface())
				}
				var sizeErr *ResponseSizeError
				if !errors.As(resultErr, &sizeErr) || sizeErr.Limit != 32 || sizeErr.StatusCode != status {
					t.Fatalf("wrong error: %v", results[1].Interface())
				}
				if !results[0].IsNil() || body.read != 33 || !body.closed {
					t.Fatalf("result=%v read=%d closed=%v", results[0], body.read, body.closed)
				}
			}
		})
	}
	if count != 38 {
		t.Fatalf("checked %d finite operations", count)
	}
}

func TestStandaloneParserDefaultResponseLimit(t *testing.T) {
	body := &countingResponseBody{remaining: DefaultFiniteResponseLimit + 100}
	_, err := ParseGetAccountsResponse(&http.Response{StatusCode: 502, ContentLength: 1, Body: body})
	var sizeErr *ResponseSizeError
	if !errors.As(err, &sizeErr) || sizeErr.Limit != DefaultFiniteResponseLimit || body.read != DefaultFiniteResponseLimit+1 || !body.closed {
		t.Fatalf("err=%v read=%d closed=%v", err, body.read, body.closed)
	}
}

func TestFiniteResponseLimitBoundaryAndRaw(t *testing.T) {
	const payload = `{"s":"ok","d":[]}`
	for _, limit := range []int64{int64(len(payload)), int64(len(payload)) + 1} {
		body := &finiteObservedBody{Reader: strings.NewReader(payload)}
		client, err := NewClientWithResponses("https://broker.example", WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
		})), WithFiniteResponseLimit(limit))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.GetAccountsWithResponse(context.Background(), nil); err != nil || !body.closed {
			t.Fatalf("exact boundary: %v closed=%v", err, body.closed)
		}
	}
	body := &countingResponseBody{remaining: 100}
	client, err := NewClient("https://broker.example", WithFiniteResponseLimit(1), WithHTTPClient(finiteResponseDoer(func(*http.Request) (*http.Response, error) { return &http.Response{Body: body}, nil })))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.GetAccounts(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	closeTestBody(t, response.Body)
	if err != nil || len(data) != 100 {
		t.Fatalf("raw read=%d err=%v", len(data), err)
	}
	for _, limit := range []int64{0, -1, math.MaxInt64} {
		if _, err := NewClient("https://broker.example", WithFiniteResponseLimit(limit)); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}
}

func TestFiniteResponseReadErrorClosesBody(t *testing.T) {
	cause := errors.New("broken reader")
	body := &finiteObservedBody{Reader: failingFiniteReader{err: cause}}
	_, err := ParseGetAccountsResponse(&http.Response{Body: body})
	if !errors.Is(err, cause) || !body.closed {
		t.Fatalf("err=%v closed=%v", err, body.closed)
	}
}

type failingFiniteReader struct{ err error }

func (r failingFiniteReader) Read([]byte) (int, error) { return 0, r.err }
