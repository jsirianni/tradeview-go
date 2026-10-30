package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes int64 = 1 << 20

// APIError reports an HTTP rejection or a Slack ok:false response. Code retains
// Slack's error code when supplied. RetryAfter is the server's requested delay;
// zero means no valid delay was supplied. No automatic retry is performed.
// Error intentionally omits provider-controlled text; inspect Code separately.
type APIError struct {
	StatusCode int
	Code       string
	RetryAfter time.Duration
}

func (err *APIError) Error() string {
	return fmt.Sprintf("slack: API request failed (HTTP %d)", err.StatusCode)
}

// operationError retains error identity without printing transport or decoding
// diagnostics that may contain credentials, response content, or alert text.
type operationError struct {
	operation string
	cause     error
}

func (err *operationError) Error() string { return "slack: " + err.operation + " failed" }
func (err *operationError) Unwrap() error { return err.cause }

func checkResponse(response *http.Response) error {
	body, readErr := readResponse(response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: response.StatusCode, RetryAfter: retryAfter(response.Header)}
		if readErr != nil {
			return errors.Join(apiErr, readErr)
		}
		// Preserve HTTP failures even when Slack or a proxy returns a body
		// without a JSON error envelope.
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(body, &envelope); err == nil {
			var code string
			if err := json.Unmarshal(envelope["error"], &code); err == nil {
				apiErr.Code = code
			}
		}
		return apiErr
	}
	if readErr != nil {
		return readErr
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("slack: unexpected success status HTTP %d", response.StatusCode)
	}
	// Match protocol keys exactly, so an unrelated "OK" field cannot
	// override an explicit ok:false rejection through struct field folding.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return &operationError{operation: "decode response", cause: err}
	}
	var ok *bool
	if err := json.Unmarshal(envelope["ok"], &ok); err != nil {
		return &operationError{operation: "decode response status", cause: err}
	}
	if ok == nil {
		return errors.New("slack: response is missing a boolean ok field")
	}
	if *ok {
		return nil
	}
	var code string
	if err := json.Unmarshal(envelope["error"], &code); err != nil {
		return &operationError{operation: "decode error code", cause: err}
	}
	if strings.TrimSpace(code) == "" {
		return errors.New("slack: rejected response is missing an error code")
	}
	return &APIError{StatusCode: response.StatusCode, Code: code, RetryAfter: retryAfter(response.Header)}
}

func readResponse(response *http.Response) ([]byte, error) {
	if response.Body == nil {
		return nil, errors.New("slack: response has no body")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &operationError{operation: "read response", cause: err}
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, errors.New("slack: response exceeds 1 MiB limit")
	}
	return body, nil
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(header.Get("Retry-After")), 10, 64)
	if err != nil || seconds <= 0 || seconds > int64((1<<63-1)/time.Second) {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
