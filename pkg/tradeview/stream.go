package tradeview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// DefaultMaxFrameBytes is the maximum size of a stream message unless overridden.
const DefaultMaxFrameBytes = 1 << 20

// StreamOption configures a typed stream without changing the underlying Client.
type StreamOption func(*streamConfig) error

type streamConfig struct {
	maxFrameBytes  int
	requestEditors []RequestEditorFn
}

// WithMaxFrameBytes sets the maximum JSON frame size, excluding its line ending.
func WithMaxFrameBytes(size int) StreamOption {
	return func(config *streamConfig) error {
		if size <= 0 || size > int(^uint(0)>>1)-2 {
			return errors.New("tradeview: maximum frame size must be positive and leave room for a line ending")
		}
		config.maxFrameBytes = size
		return nil
	}
}

// WithStreamRequestEditor adds a request editor to an individual stream request.
func WithStreamRequestEditor(editor RequestEditorFn) StreamOption {
	return func(config *streamConfig) error {
		if editor == nil {
			return errors.New("tradeview: stream request editor must not be nil")
		}
		config.requestEditors = append(config.requestEditors, editor)
		return nil
	}
}

func configureStream(options []StreamOption) (streamConfig, error) {
	config := streamConfig{maxFrameBytes: DefaultMaxFrameBytes}
	for _, option := range options {
		if option == nil {
			return config, errors.New("tradeview: stream option must not be nil")
		}
		if err := option(&config); err != nil {
			return config, err
		}
	}
	return config, nil
}

// Stream incrementally reads newline-delimited JSON. Next must be called by only
// one goroutine at a time. Close may run concurrently with a blocked Next.
// Close is idempotent; EOF, errors, and context cancellation also close the body.
type Stream[T any] struct {
	ctx           context.Context
	response      *http.Response
	scanner       *bufio.Scanner
	decode        func([]byte) (T, error)
	maxFrameBytes int
	mu            sync.Mutex
	value         T
	err           error
	closed        bool
	closeOnce     sync.Once
	closeErr      error
	stopCancel    func() bool
}

func openHTTPStream[T any](ctx context.Context, response *http.Response, requestErr error, config streamConfig, decode func([]byte) (T, error)) (*Stream[T], error) {
	if requestErr != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, requestErr
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("tradeview: empty stream response")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, err := io.ReadAll(io.LimitReader(response.Body, int64(config.maxFrameBytes)))
		_ = response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("tradeview: read stream error response: %w", err)
		}
		if err := responseError(body, response); err != nil {
			return nil, err
		}
		return nil, &HTTPError{StatusCode: response.StatusCode, Status: response.Status, Body: body}
	}
	scanner := bufio.NewScanner(response.Body)
	initial := min(config.maxFrameBytes, 4096)
	// Scanner counts the newline against its token limit; leave room for CRLF.
	scanner.Buffer(make([]byte, initial), config.maxFrameBytes+2)
	stream := &Stream[T]{ctx: ctx, response: response, scanner: scanner, decode: decode, maxFrameBytes: config.maxFrameBytes}
	stream.mu.Lock()
	// #nosec G104 -- finish saves the body close error for Close; Err retains the cancellation cause.
	stream.stopCancel = context.AfterFunc(ctx, func() { stream.finish(ctx.Err()) })
	stream.mu.Unlock()
	return stream, nil
}

// Next advances to the next frame. A server error frame is yielded once so its
// typed contents remain accessible; it also terminates the stream with APIError.
func (stream *Stream[T]) Next() bool {
	stream.mu.Lock()
	closed := stream.closed
	stream.mu.Unlock()
	if closed {
		return false
	}
	for stream.scanner.Scan() {
		rawFrame := stream.scanner.Bytes()
		if len(rawFrame) > stream.maxFrameBytes {
			// #nosec G104 -- finish saves the body close error for Close; Err retains this terminal cause.
			stream.finish(fmt.Errorf("tradeview: stream frame exceeds %d bytes", stream.maxFrameBytes))
			return false
		}
		frame := bytes.TrimSpace(rawFrame)
		if len(frame) == 0 {
			continue
		}
		value, err := stream.decode(frame)
		if err != nil {
			// #nosec G104 -- finish saves the body close error for Close; Err retains this terminal cause.
			stream.finish(fmt.Errorf("tradeview: decode stream frame: %w", err))
			return false
		}
		stream.mu.Lock()
		if stream.closed {
			stream.mu.Unlock()
			return false
		}
		stream.value = value
		stream.mu.Unlock()
		if err := streamAPIError(frame, stream.response.StatusCode); err != nil {
			// #nosec G104 -- finish saves the body close error for Close; Err retains this terminal cause.
			stream.finish(err)
		}
		return true
	}
	err := stream.scanner.Err()
	if errors.Is(err, bufio.ErrTooLong) {
		err = fmt.Errorf("tradeview: stream frame exceeds %d bytes", stream.maxFrameBytes)
	}
	if stream.ctx.Err() != nil {
		err = stream.ctx.Err()
	}
	// #nosec G104 -- finish saves the body close error for Close; Err retains this terminal cause.
	stream.finish(err)
	return false
}

// Value returns the last successfully decoded frame. It is valid after Next
// returns true and remains available after EOF, Close, or an error.
func (stream *Stream[T]) Value() T {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.value
}

// Err returns the terminal transport, decoding, cancellation, or API error.
// A clean EOF or an explicit Close does not set an error.
func (stream *Stream[T]) Err() error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.err
}

// Close releases the response body and unblocks a pending Next.
func (stream *Stream[T]) Close() error { return stream.finish(nil) }

func (stream *Stream[T]) finish(err error) error {
	stream.mu.Lock()
	if !stream.closed {
		stream.closed = true
		stream.err = err
	}
	stream.mu.Unlock()
	stream.closeOnce.Do(func() {
		if stream.stopCancel != nil {
			stream.stopCancel()
		}
		stream.closeErr = stream.response.Body.Close()
	})
	return stream.closeErr
}

func streamAPIError(frame []byte, statusCode int) error {
	if len(frame) == 0 || frame[0] != '{' {
		return nil
	}
	var message struct {
		Status  string `json:"s"`
		Message string `json:"errmsg"`
		Code    string `json:"errtype"`
	}
	if err := json.Unmarshal(frame, &message); err != nil || message.Status != "error" {
		return nil
	}
	return &APIError{StatusCode: statusCode, Code: message.Code, Message: message.Message, Body: append([]byte(nil), frame...)}
}
