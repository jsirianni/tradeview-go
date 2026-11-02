// Package alerts defines delivery contracts for application-generated alerts.
// Applications compose a Sender with their data clients and own alert rules,
// recipient lookup, deadlines, retries, scheduling, and duplicate prevention.
package alerts

import "context"

// Alert is provider-neutral, plain-text content for a notification.
type Alert struct {
	Title string // Optional title.
	Body  string // Required, nonblank message body.
}

// Sender delivers an alert to one provider-native recipient, such as a Slack ID,
// email address, or telephone number. Each Send makes one synchronous delivery
// attempt. A nil error means the provider accepted the alert, not that it was
// read. An error can leave delivery uncertain; callers own retry decisions.
type Sender interface {
	Send(ctx context.Context, recipient string, alert Alert) error
}
