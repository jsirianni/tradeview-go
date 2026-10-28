package tradeview

import (
	"errors"
	"log/slog"
)

// WithLogger enables one structured log record when each operation completes.
// Successful operations use Info level; failures use Error level. The caller
// configures the handler's level, format, and destination (for example stdout
// or an open file), and owns the handler and its output's lifetime.
//
// Logging is disabled by default and is independent of WithTelemetry. Records
// contain fixed operation/route names, duration, HTTP status, response bytes,
// and bounded error categories. Credentials, payloads, IDs, URLs, and error
// text are never included. Raw operations complete on body EOF or Close;
// typed operations complete after parsing or when the stream terminates.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(client *Client) error {
		if logger == nil {
			return errors.New("tradeview: nil logger")
		}
		client.logger = logger
		return nil
	}
}
