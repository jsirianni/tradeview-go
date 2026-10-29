package tradeview

import (
	"errors"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type failingInstrumentMeter struct {
	metric.Meter
	fail  string
	err   error
	calls []string
}

func (m *failingInstrumentMeter) instrument(name string) error {
	m.calls = append(m.calls, name)
	if name == m.fail {
		return m.err
	}
	return nil
}

func (m *failingInstrumentMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if err := m.instrument(name); err != nil {
		return nil, err
	}
	return m.Meter.Int64Counter(name, options...)
}

func (m *failingInstrumentMeter) Int64UpDownCounter(name string, options ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	if err := m.instrument(name); err != nil {
		return nil, err
	}
	return m.Meter.Int64UpDownCounter(name, options...)
}

func (m *failingInstrumentMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if err := m.instrument(name); err != nil {
		return nil, err
	}
	return m.Meter.Float64Histogram(name, options...)
}

func (m *failingInstrumentMeter) Int64Histogram(name string, options ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	if err := m.instrument(name); err != nil {
		return nil, err
	}
	return m.Meter.Int64Histogram(name, options...)
}

type failingInstrumentProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (p failingInstrumentProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return p.meter
}

func TestTelemetryInstrumentCreationFailure(t *testing.T) {
	names := []string{"tradeview.client.operations", "tradeview.client.active_operations", "tradeview.client.operation.duration", "tradeview.client.response.body.size", "tradeview.client.stream.frames"}
	for i, name := range names {
		t.Run(name, func(t *testing.T) {
			cause := errors.New("instrument unavailable")
			provider := noop.NewMeterProvider()
			meter := &failingInstrumentMeter{Meter: provider.Meter(InstrumentationName), fail: name, err: cause}
			option := WithTelemetry(TelemetryOptions{MeterProvider: failingInstrumentProvider{provider, meter}})
			client, err := NewClient("https://broker.example", option)
			if client != nil || !errors.Is(err, cause) {
				t.Fatalf("client = %v, error = %v; want nil client and original error", client, err)
			}
			if !reflect.DeepEqual(meter.calls, names[:i+1]) {
				t.Fatalf("instrument creation continued after failure: %v", meter.calls)
			}
			// Applying a failed reconfiguration must not replace valid client state.
			previous := &telemetry{}
			existing := &Client{telemetry: previous}
			if err := option(existing); !errors.Is(err, cause) || existing.telemetry != previous {
				t.Fatalf("failed option changed prior telemetry: error = %v", err)
			}
		})
	}
}
