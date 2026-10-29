package tradeview

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// operationObservation owns the common request/response lifetime. Logging and
// OpenTelemetry are optional, independent consumers of its terminal outcome.
type operationObservation struct {
	telemetry *operationTelemetry
	logger    *slog.Logger
	ctx       context.Context
	operation string
	method    string
	route     string
	streaming bool
	start     time.Time
	typed     bool
	once      sync.Once
	mu        sync.Mutex
	status    int
	bytes     int64
	closeErr  error
}

func (client *Client) startObservation(ctx context.Context, operation, method, route string, streaming bool) (context.Context, *operationObservation) {
	if client.telemetry == nil && client.logger == nil {
		return ctx, nil
	}
	ctx, telemetry := client.startTelemetry(ctx, operation, method, route, streaming)
	o := &operationObservation{
		telemetry: telemetry, logger: client.logger, ctx: ctx,
		operation: operation, method: method, route: route, streaming: streaming,
		start: time.Now(),
	}
	return ctx, o
}

func doObservedRequest(client HttpRequestDoer, request *http.Request, observation *operationObservation) (*http.Response, error) {
	if observation != nil && observation.telemetry != nil {
		request = observation.telemetry.prepare(request)
	}
	return doRequest(client, request)
}

func (o *operationObservation) response(response *http.Response, err error) {
	if o == nil {
		return
	}
	if response != nil {
		o.status = response.StatusCode
	}
	if err != nil {
		o.finish(err)
		return
	}
	if response == nil || response.Body == nil {
		o.finish(errors.New("empty response"))
		return
	}
	response.Body = &observedBody{ReadCloser: response.Body, observation: o}
}

type observedBody struct {
	io.ReadCloser
	observation *operationObservation
}

func (body *observedBody) Read(p []byte) (int, error) {
	n, err := body.ReadCloser.Read(p)
	o := body.observation
	o.mu.Lock()
	o.bytes += int64(n)
	typed := o.typed
	o.mu.Unlock()
	if err != nil && !typed {
		o.finish(err)
	}
	return n, err
}

func (body *observedBody) Close() error {
	err := body.ReadCloser.Close()
	o := body.observation
	o.mu.Lock()
	if err != nil {
		o.closeErr = err
	}
	typed := o.typed
	o.mu.Unlock()
	if !typed {
		o.finish(err)
	}
	return err
}

func responseObserver(response *http.Response) *operationObservation {
	if response != nil {
		if body, ok := response.Body.(*observedBody); ok {
			return body.observation
		}
	}
	return nil
}

// beginTypedResponseObservation hands this response's completion to its parser
// or stream before body consumption. Context values never control ownership:
// nested requests from editors, token sources, or client adapters retain their
// own raw or typed lifecycle.
func beginTypedResponseObservation(response *http.Response) *operationObservation {
	o := responseObserver(response)
	if o != nil {
		o.mu.Lock()
		o.typed = true
		o.mu.Unlock()
	}
	return o
}

func finishResponseObservation(response *http.Response, err error) {
	responseObserver(response).finish(err)
}

func (o *operationObservation) frame() {
	if o != nil && o.telemetry != nil {
		o.telemetry.frame()
	}
}

func (o *operationObservation) finish(err error) {
	if o == nil {
		return
	}
	o.once.Do(func() {
		o.mu.Lock()
		if err == nil || err == io.EOF {
			err = o.closeErr
		}
		size := o.bytes
		o.mu.Unlock()
		category := operationErrorType(err)
		if category == "" && o.status >= 400 {
			category = "http"
		}
		duration := time.Since(o.start)
		if o.telemetry != nil {
			o.telemetry.finish(o.status, size, duration, category)
		}
		if o.logger != nil {
			level := slog.LevelInfo
			if category != "" {
				level = slog.LevelError
			}
			if !o.logger.Enabled(o.ctx, level) {
				return
			}
			attrs := []slog.Attr{
				slog.String("operation", o.operation), slog.String("method", o.method),
				slog.String("route", o.route), slog.Bool("streaming", o.streaming),
				slog.Duration("duration", duration), slog.Int64("response_bytes", size),
			}
			if o.status != 0 {
				attrs = append(attrs, slog.Int("status_code", o.status))
			}
			if category != "" {
				attrs = append(attrs, slog.String("error_type", category))
			}
			o.logger.LogAttrs(o.ctx, level, "tradeview operation completed", attrs...)
		}
	})
}

func operationErrorType(err error) string {
	if err == nil || err == io.EOF {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var apiErr *APIError
	var httpErr *HTTPError
	var protocolErr *ProtocolError
	var sizeErr *ResponseSizeError
	switch {
	case errors.As(err, &apiErr):
		return "api"
	case errors.As(err, &httpErr):
		return "http"
	case errors.As(err, &protocolErr):
		return "protocol"
	case errors.As(err, &sizeErr):
		return "response_size"
	default:
		return "client"
	}
}
