package tradeview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// DefaultFiniteResponseLimit bounds decoded bytes read by finite response parsers.
const DefaultFiniteResponseLimit int64 = 16 << 20

type finiteResponseLimitKey struct{}

// ResponseSizeError reports a finite response exceeding its decoded byte budget.
// Oversized payloads are not retained. The response body is closed by the parser.
type ResponseSizeError struct {
	Limit      int64
	StatusCode int
}

func (e *ResponseSizeError) Error() string {
	return fmt.Sprintf("tradeview: HTTP %d response exceeds %d byte limit", e.StatusCode, e.Limit)
}

// WithFiniteResponseLimit sets the positive decoded byte budget for typed finite
// responses. Raw methods remain unbounded. Standalone parsers use the default
// unless their response carries a request configured by this client option.
func WithFiniteResponseLimit(limit int64) ClientOption {
	return func(client *Client) error {
		if limit <= 0 || limit == int64(^uint64(0)>>1) {
			return errors.New("tradeview: finite response limit must be positive and less than MaxInt64")
		}
		client.RequestEditors = append(client.RequestEditors, func(_ context.Context, request *http.Request) error {
			*request = *request.WithContext(context.WithValue(request.Context(), finiteResponseLimitKey{}, limit))
			return nil
		})
		return nil
	}
}

// doRequest preserves response request metadata for custom doers as well as the
// standard HTTP client. It does not read, wrap, or limit raw response bodies.
func doRequest(client HttpRequestDoer, request *http.Request) (*http.Response, error) {
	response, err := client.Do(request)
	if response != nil {
		if response.Request == nil {
			response.Request = request
		} else if limit, ok := request.Context().Value(finiteResponseLimitKey{}).(int64); ok {
			response.Request = response.Request.WithContext(context.WithValue(response.Request.Context(), finiteResponseLimitKey{}, limit))
		}
	}
	return response, err
}

func readFiniteResponse(response *http.Response) ([]byte, error) {
	limit := DefaultFiniteResponseLimit
	if response.Request != nil {
		if configured, ok := response.Request.Context().Value(finiteResponseLimitKey{}).(int64); ok {
			limit = configured
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if int64(len(body)) > limit {
		return nil, &ResponseSizeError{Limit: limit, StatusCode: response.StatusCode}
	}
	return body, err
}

// APIError is an error envelope returned by a broker, including HTTP 200 errors.
// Code preserves the broker's errtype, including values introduced after this API version.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Body       []byte
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return "tradeview: " + e.Message
	}
	return fmt.Sprintf("tradeview: %s: %s", e.Code, e.Message)
}

// HTTPError is a non-2xx response. Body retains its uninterpreted payload.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("tradeview: HTTP %d %s", e.StatusCode, http.StatusText(e.StatusCode))
}

// ProtocolError reports a response that violates the declared finite-response
// or stream-handshake contract. Body retains the original payload (bounded by
// the finite response limit or stream frame limit); Err describes the failure.
// Unwrap preserves access to underlying JSON decoding errors.
type ProtocolError struct {
	StatusCode  int
	ContentType string
	Body        []byte
	Err         error
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("tradeview: invalid HTTP %d response: %v", e.StatusCode, e.Err)
}

func (e *ProtocolError) Unwrap() error { return e.Err }

func newProtocolError(body []byte, response *http.Response, err error) *ProtocolError {
	return &ProtocolError{
		StatusCode: response.StatusCode, ContentType: response.Header.Get("Content-Type"),
		Body: bytes.Clone(body), Err: err,
	}
}

// finiteResponseError validates the pinned contract's HTTP 200 JSON response.
// Streams have their own handshake and frame handling.
func finiteResponseError(body []byte, response *http.Response) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(body, response)
	}
	if response.StatusCode != http.StatusOK {
		return newProtocolError(body, response, errors.New("unexpected success status; expected HTTP 200"))
	}
	// Preserve broker errors even when a proxy supplies an incorrect media type.
	if err := finiteBrokerError(body, response); err != nil {
		return err
	}
	if !isJSONMediaType(response.Header.Get("Content-Type")) {
		return newProtocolError(body, response, fmt.Errorf("expected a JSON Content-Type, received %q", response.Header.Get("Content-Type")))
	}
	return nil
}

// Decode the discriminator independently so malformed error details cannot turn
// a known broker failure into a successful opaque response union.
func finiteBrokerError(body []byte, response *http.Response) error {
	var envelope struct {
		S       json.RawMessage `json:"s"`
		Code    json.RawMessage `json:"errtype"`
		Message json.RawMessage `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil // The finite model validator supplies JSON/type diagnostics.
	}
	var status string
	if json.Unmarshal(envelope.S, &status) != nil || status != "error" {
		return nil
	}
	apiErr := &APIError{StatusCode: response.StatusCode, Body: bytes.Clone(body)}
	for _, field := range []struct {
		name  string
		raw   json.RawMessage
		value *string
	}{
		{"errtype", envelope.Code, &apiErr.Code},
		{"errmsg", envelope.Message, &apiErr.Message},
	} {
		if len(field.raw) == 0 {
			continue
		}
		if bytes.Equal(field.raw, []byte("null")) {
			return newProtocolError(body, response, fmt.Errorf("null broker error %s", field.name))
		}
		if err := json.Unmarshal(field.raw, field.value); err != nil {
			return newProtocolError(body, response, responseJSONFieldError(err, field.name))
		}
	}
	return apiErr
}

func isJSONMediaType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" ||
		(strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json") && len(mediaType) > len("application/+json"))
}

func responseError(body []byte, response *http.Response) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &HTTPError{StatusCode: response.StatusCode, Status: response.Status, Body: bytes.Clone(body)}
	}
	var envelope struct {
		S       string `json:"s"`
		Code    string `json:"errtype"`
		Message string `json:"errmsg"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.S == "error" {
		return &APIError{StatusCode: response.StatusCode, Code: envelope.Code, Message: envelope.Message, Body: bytes.Clone(body)}
	}
	return nil
}

// TokenSource supplies a bearer token for each request. Token lifecycle and caching
// belong to the caller; implementations must support concurrent calls.
type TokenSource interface {
	Token(context.Context) (string, error)
}

// TokenSourceFunc adapts a function to TokenSource.
type TokenSourceFunc func(context.Context) (string, error)

func (f TokenSourceFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

// WithBearerToken attaches a caller-provided bearer token to requests.
func WithBearerToken(token string) ClientOption {
	return WithTokenSource(TokenSourceFunc(func(context.Context) (string, error) { return token, nil }))
}

// WithTokenSource obtains a token using the request context immediately before sending.
// It does not acquire, refresh, cache, or persist tokens itself.
func WithTokenSource(source TokenSource) ClientOption {
	return func(client *Client) error {
		if source == nil {
			return errors.New("tradeview: nil token source")
		}
		client.RequestEditors = append(client.RequestEditors, func(ctx context.Context, request *http.Request) error {
			token, err := source.Token(ctx)
			if err != nil {
				return fmt.Errorf("tradeview: bearer token: %w", err)
			}
			if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
				return errors.New("tradeview: bearer token must be non-empty and contain no line breaks")
			}
			request.Header.Set("Authorization", "Bearer "+token)
			return nil
		})
		return nil
	}
}

// WithLocale supplies locale when the operation's query has no locale or an empty value.
// Explicit nonempty per-request locale values always take precedence.
func WithLocale(locale string) ClientOption {
	return func(client *Client) error {
		if strings.TrimSpace(locale) == "" {
			return errors.New("tradeview: locale must be non-empty")
		}
		client.RequestEditors = append(client.RequestEditors, func(_ context.Context, request *http.Request) error {
			query := request.URL.Query()
			if query.Get("locale") == "" {
				query.Set("locale", locale)
				request.URL.RawQuery = query.Encode()
			}
			return nil
		})
		return nil
	}
}

// WithExtraOrderFields adds broker-defined order-dialog fields to a form request.
// It snapshots fields at construction and rejects standard or already-present keys.
// Pass the returned editor to a single order/preview operation, not a whole client.
func WithExtraOrderFields(fields url.Values) RequestEditorFn {
	extras := make(url.Values, len(fields))
	for key, values := range fields {
		extras[key] = append([]string(nil), values...)
	}
	return func(_ context.Context, request *http.Request) error {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/x-www-form-urlencoded" {
			return errors.New("tradeview: extra order fields require a form request")
		}
		for key := range extras {
			if key == "" || _standardOrderFields[key] {
				return fmt.Errorf("tradeview: extra order field %q conflicts with a standard field", key)
			}
		}
		if request.Body == nil {
			return errors.New("tradeview: extra order fields require a request body")
		}
		body, err := io.ReadAll(request.Body)
		closeErr := closeRequestBody(request)
		if err != nil {
			return fmt.Errorf("tradeview: read order form: %w", errors.Join(err, closeErr))
		}
		if closeErr != nil {
			return fmt.Errorf("tradeview: close order form: %w", closeErr)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return fmt.Errorf("tradeview: parse order form: %w", err)
		}
		for key, values := range extras {
			if form.Has(key) {
				return fmt.Errorf("tradeview: extra order field %q already exists", key)
			}
			form[key] = append([]string(nil), values...)
		}
		encoded := form.Encode()
		request.Body = io.NopCloser(strings.NewReader(encoded))
		request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(encoded)), nil }
		request.ContentLength = int64(len(encoded))
		request.Header.Del("Content-Length")
		return nil
	}
}

var _standardOrderFields = map[string]bool{
	"instrument": true, "qty": true, "side": true, "type": true, "id": true,
	"limitPrice": true, "stopPrice": true, "durationType": true, "durationDateTime": true,
	"stopLoss": true, "trailingStopPips": true, "guaranteedStop": true, "takeProfit": true,
	"digitalSignature": true, "currentAsk": true, "currentBid": true, "exitLevels": true,
	"confirmId": true, "orderInfoId": true,
}

// encodeForm handles generated oneOf/allOf JSON models without reflecting over
// generator-private union fields. Primitive arrays use repeated form keys;
// complex fields (such as exitLevels) are compact JSON within a single value.
func encodeForm(body any) (string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("tradeview: marshal form: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return "", fmt.Errorf("tradeview: form body must be an object: %w", err)
	}
	if object == nil {
		return "", errors.New("tradeview: form body must be an object")
	}
	form := make(url.Values, len(object))
	for key, raw := range object {
		value := bytes.TrimSpace(raw)
		if bytes.Equal(value, []byte("null")) {
			continue
		}
		if len(value) > 0 && value[0] == '[' {
			var items []json.RawMessage
			if err := json.Unmarshal(value, &items); err != nil {
				return "", err
			}
			hasNestedValues := false
			for _, item := range items {
				item = bytes.TrimSpace(item)
				if len(item) > 0 && (item[0] == '{' || item[0] == '[') {
					hasNestedValues = true
					break
				}
			}
			if hasNestedValues {
				form.Set(key, string(value))
				continue
			}
			for _, item := range items {
				scalar, err := formScalar(item)
				if err != nil {
					return "", err
				}
				form.Add(key, scalar)
			}
			// An explicitly empty array must remain distinguishable from omission.
			if len(items) == 0 {
				form.Set(key, "[]")
			}
			continue
		}
		scalar, err := formScalar(value)
		if err != nil {
			return "", err
		}
		form.Set(key, scalar)
	}
	return form.Encode(), nil
}

func formScalar(raw json.RawMessage) (string, error) {
	value := bytes.TrimSpace(raw)
	if len(value) > 0 && value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return "", err
		}
		return text, nil
	}
	return string(value), nil
}

// escapeDotPathSegment keeps opaque IDs from being normalized as relative URL paths.
// Other characters have already been escaped by the generated path serializer.
func escapeDotPathSegment(value string) string {
	switch value {
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	default:
		return value
	}
}
