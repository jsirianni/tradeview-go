package tradeview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

type requestCleanupBody struct {
	io.Reader
	closes   int
	closeErr error
}

func (body *requestCleanupBody) Close() error {
	body.closes++
	return body.closeErr
}

type requestCleanupDoer func(*http.Request) (*http.Response, error)

func (do requestCleanupDoer) Do(request *http.Request) (*http.Response, error) {
	return do(request)
}

func TestRequestEditorFailureClosesBody(t *testing.T) {
	failure := errors.New("editor/token rejected request")
	calls := []struct {
		name string
		call func(*Client, io.Reader, ...RequestEditorFn) error
	}{
		{"raw order", func(c *Client, body io.Reader, editors ...RequestEditorFn) error {
			_, err := c.PlaceOrderWithBody(t.Context(), "account", nil, "application/json", body, editors...)
			return err
		}},
		{"typed order", func(c *Client, body io.Reader, editors ...RequestEditorFn) error {
			_, err := (&ClientWithResponses{ClientInterface: c}).PlaceOrderWithBodyWithResponse(t.Context(), "account", nil, "application/json", body, editors...)
			return err
		}},
		{"raw preview", func(c *Client, body io.Reader, editors ...RequestEditorFn) error {
			_, err := c.PreviewOrderWithBody(t.Context(), "account", nil, "application/json", body, editors...)
			return err
		}},
		{"typed authorization", func(c *Client, body io.Reader, editors ...RequestEditorFn) error {
			_, err := (&ClientWithResponses{ClientInterface: c}).AuthorizeWithBodyWithResponse(t.Context(), "application/json", body, editors...)
			return err
		}},
	}
	for _, call := range calls {
		for _, source := range []string{"client editor", "request editor", "token source"} {
			t.Run(call.name+"/"+source, func(t *testing.T) {
				body := &requestCleanupBody{Reader: strings.NewReader(`{}`)}
				dispatched := false
				options := []ClientOption{WithHTTPClient(requestCleanupDoer(func(*http.Request) (*http.Response, error) {
					dispatched = true
					return nil, errors.New("unexpected dispatch")
				}))}
				reject := func(context.Context, *http.Request) error { return failure }
				var editors []RequestEditorFn
				switch source {
				case "client editor":
					options = append(options, WithRequestEditorFn(reject))
				case "request editor":
					editors = append(editors, reject)
				case "token source":
					options = append(options, WithTokenSource(TokenSourceFunc(func(context.Context) (string, error) { return "", failure })))
				}
				client, err := NewClient("https://broker.example", options...)
				if err != nil {
					t.Fatal(err)
				}
				if err := call.call(client, body, editors...); !errors.Is(err, failure) {
					t.Fatalf("lost editor error: %v", err)
				}
				if dispatched || body.closes != 1 {
					t.Fatalf("dispatched=%v closes=%d", dispatched, body.closes)
				}
			})
		}
	}
}

func TestRequestConstructionFailureRetainsCallerBody(t *testing.T) {
	body := &requestCleanupBody{Reader: strings.NewReader(`{}`)}
	client, err := NewClient("https://broker.example/%zz")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.PlaceOrderWithBody(t.Context(), "account", nil, "application/json", body)
	if err == nil || body.closes != 0 {
		t.Fatalf("construction error=%v caller body closes=%d", err, body.closes)
	}
}

func TestGeneratedFormEditorFailureClosesReplacement(t *testing.T) {
	failure := errors.New("token source failed")
	var body *requestCleanupBody
	client, err := NewClient("https://broker.example",
		WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			if err := request.Body.Close(); err != nil {
				return err
			}
			body = &requestCleanupBody{Reader: strings.NewReader("replacement")}
			request.Body = body
			return nil
		}),
		WithTokenSource(TokenSourceFunc(func(context.Context) (string, error) { return "", failure })),
		WithHTTPClient(requestCleanupDoer(func(*http.Request) (*http.Response, error) {
			t.Fatal("unexpected dispatch")
			return nil, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&ClientWithResponses{ClientInterface: client}).PlaceOrderWithFormdataBodyWithResponse(t.Context(), "account", nil, PlaceOrderFormdataRequestBody{})
	if !errors.Is(err, failure) || body == nil || body.closes != 1 {
		t.Fatalf("error=%v replacement=%+v", err, body)
	}
}

func TestRequestEditorFailureClosesCurrentBody(t *testing.T) {
	failure := errors.New("rejected")
	closeFailure := errors.New("close failed")
	for _, scenario := range []string{"closed input", "replace", "wrap", "later editor", "close failure"} {
		t.Run(scenario, func(t *testing.T) {
			original := &requestCleanupBody{Reader: strings.NewReader("original")}
			replacement := &requestCleanupBody{Reader: strings.NewReader("replacement")}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://broker.example", original)
			if err != nil {
				t.Fatal(err)
			}
			var wantReplacement bool
			edit := func(_ context.Context, request *http.Request) error {
				switch scenario {
				case "closed input":
					if err := request.Body.Close(); err != nil {
						t.Fatal(err)
					}
					request.Body = nil
				case "replace", "later editor":
					if err := request.Body.Close(); err != nil {
						t.Fatal(err)
					}
					request.Body = replacement
					wantReplacement = true
				case "wrap":
					request.Body = struct{ io.ReadCloser }{request.Body}
				case "close failure":
					original.closeErr = closeFailure
				}
				if scenario == "later editor" {
					return nil
				}
				return failure
			}
			err = (&Client{RequestEditors: []RequestEditorFn{edit}}).applyEditors(t.Context(), request, []RequestEditorFn{
				func(_ context.Context, request *http.Request) error {
					if err := request.Body.Close(); err != nil {
						t.Fatal(err)
					}
					request.Body = nil
					return failure
				},
			})
			if !errors.Is(err, failure) || (scenario == "close failure" && !errors.Is(err, closeFailure)) {
				t.Fatalf("lost editor or cleanup error: %v", err)
			}
			if original.closes != 1 || (wantReplacement && replacement.closes != 1) {
				t.Fatalf("original closes=%d replacement closes=%d", original.closes, replacement.closes)
			}
		})
	}
}

func TestExtraOrderFieldsFailureClosesOnce(t *testing.T) {
	readFailure := errors.New("read failed")
	closeFailure := errors.New("close failed")
	for _, scenario := range []struct {
		name, form string
		readErr    error
		closeErr   error
		fields     url.Values
	}{
		{name: "read", readErr: readFailure, fields: url.Values{"custom": {"v"}}},
		{name: "read and close", readErr: readFailure, closeErr: closeFailure, fields: url.Values{"custom": {"v"}}},
		{name: "close", form: "qty=1", closeErr: closeFailure, fields: url.Values{"custom": {"v"}}},
		{name: "parse", form: "%zz", fields: url.Values{"custom": {"v"}}},
		{name: "collision", form: "custom=v", fields: url.Values{"custom": {"v"}}},
		{name: "early rejection", form: "qty=1", fields: url.Values{"qty": {"2"}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var reader io.Reader = strings.NewReader(scenario.form)
			if scenario.readErr != nil {
				reader = requestCleanupErrorReader{scenario.readErr}
			}
			body := &requestCleanupBody{Reader: reader, closeErr: scenario.closeErr}
			client, err := NewClient("https://broker.example", WithHTTPClient(requestCleanupDoer(func(*http.Request) (*http.Response, error) {
				t.Fatal("unexpected dispatch")
				return nil, nil
			})))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.PlaceOrderWithBody(t.Context(), "account", nil, "application/x-www-form-urlencoded", body, WithExtraOrderFields(scenario.fields))
			if err == nil || body.closes != 1 {
				t.Fatalf("err=%v closes=%d", err, body.closes)
			}
			if scenario.readErr != nil && !errors.Is(err, scenario.readErr) || scenario.closeErr != nil && !errors.Is(err, scenario.closeErr) {
				t.Fatalf("lost source error: %v", err)
			}
		})
	}
}

type requestCleanupErrorReader struct{ err error }

func (reader requestCleanupErrorReader) Read([]byte) (int, error) { return 0, reader.err }

func TestRequestEditorSuccessPreservesDoerOwnership(t *testing.T) {
	for _, edited := range []bool{false, true} {
		for _, doerFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("edited=%t/doerFails=%t", edited, doerFails), func(t *testing.T) {
				body := &requestCleanupBody{Reader: strings.NewReader("qty=0")}
				failure := errors.New("doer failed")
				var pending io.ReadCloser
				client, err := NewClient("https://broker.example", WithBearerToken("token"), WithHTTPClient(requestCleanupDoer(func(request *http.Request) (*http.Response, error) {
					wantCloses := 0
					if edited {
						wantCloses = 1 // WithExtraOrderFields replaced the original.
					}
					if body.closes != wantCloses {
						t.Fatalf("premature cleanup: %d", body.closes)
					}
					if !edited && request.Body != body {
						t.Fatal("unchanged body type not restored for doer")
					}
					data, err := io.ReadAll(request.Body)
					if err != nil {
						t.Fatal(err)
					}
					want := "qty=0"
					if edited {
						want = "custom=v&qty=0"
						replay, err := request.GetBody()
						if err != nil {
							t.Fatal(err)
						}
						repeated, err := io.ReadAll(replay)
						if err != nil || string(repeated) != want {
							t.Fatalf("replay=%s err=%v", repeated, err)
						}
						if err := replay.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if string(data) != want {
						t.Fatalf("body=%s want %s", data, want)
					}
					pending = request.Body // A doer can close asynchronously after returning.
					if doerFails {
						return nil, failure
					}
					return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
				})))
				if err != nil {
					t.Fatal(err)
				}
				var editors []RequestEditorFn
				if edited {
					editors = append(editors, WithExtraOrderFields(url.Values{"custom": {"v"}}))
				}
				_, err = client.PlaceOrderWithBody(t.Context(), "account", nil, "application/x-www-form-urlencoded", body, editors...)
				if (doerFails && !errors.Is(err, failure)) || (!doerFails && err != nil) {
					t.Fatalf("unexpected dispatch error: %v", err)
				}
				if !edited && body.closes != 0 {
					t.Fatal("client reclaimed body after dispatch")
				}
				if err := pending.Close(); err != nil {
					t.Fatal(err)
				}
				if body.closes != 1 {
					t.Fatalf("original body closes=%d", body.closes)
				}
			})
		}
	}
}

func TestRequestEditorsPreserveConcreteBodyAndSeek(t *testing.T) {
	for _, typed := range []bool{false, true} {
		for _, reject := range []bool{false, true} {
			t.Run(fmt.Sprintf("typed=%t/reject=%t", typed, reject), func(t *testing.T) {
				body, err := os.CreateTemp(t.TempDir(), "request-*.json")
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					// Dispatch/editor cleanup owns this file; close only guards early test exits.
					if err := body.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
						t.Error(err)
					}
				}()
				payload := `{"instrument":"EURUSD"}`
				if _, err := io.WriteString(body, payload); err != nil {
					t.Fatal(err)
				}
				failure := errors.New("request editor rejected file")
				dispatched := false
				client, err := NewClient("https://broker.example",
					WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
						file, ok := request.Body.(*os.File)
						if !ok || file != body {
							t.Fatalf("editor sees %T, want original *os.File", request.Body)
						}
						if _, err := file.Seek(0, io.SeekStart); err != nil {
							t.Fatal(err)
						}
						prefix := make([]byte, 1)
						if _, err := io.ReadFull(file, prefix); err != nil || prefix[0] != '{' {
							t.Fatalf("prefix=%q err=%v", prefix, err)
						}
						return nil
					}),
					WithHTTPClient(requestCleanupDoer(func(request *http.Request) (*http.Response, error) {
						dispatched = true
						if request.Body != body {
							t.Fatal("doer lost original file body")
						}
						data, err := io.ReadAll(request.Body)
						if err != nil || string(data) != payload {
							t.Fatalf("body=%q err=%v", data, err)
						}
						if err := request.Body.Close(); err != nil {
							t.Fatal(err)
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"s":"ok","d":{"orderId":"order"}}`))}, nil
					})),
				)
				if err != nil {
					t.Fatal(err)
				}
				editor := func(_ context.Context, request *http.Request) error {
					seeker, ok := request.Body.(io.Seeker)
					if !ok {
						t.Fatalf("request editor lost io.Seeker: %T", request.Body)
					}
					if _, err := seeker.Seek(0, io.SeekStart); err != nil {
						t.Fatal(err)
					}
					if reject {
						return failure
					}
					return nil
				}
				if typed {
					_, err = (&ClientWithResponses{ClientInterface: client}).PlaceOrderWithBodyWithResponse(t.Context(), "account", nil, "application/json", body, editor)
				} else {
					var response *http.Response
					response, err = client.PlaceOrderWithBody(t.Context(), "account", nil, "application/json", body, editor)
					if response != nil {
						if closeErr := response.Body.Close(); closeErr != nil {
							t.Fatal(closeErr)
						}
					}
				}
				if reject && err != failure || !reject && err != nil || dispatched == reject {
					t.Fatalf("err=%v dispatched=%v", err, dispatched)
				}
				if _, err := body.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("request body was not closed: %v", err)
				}
			})
		}
	}
}
