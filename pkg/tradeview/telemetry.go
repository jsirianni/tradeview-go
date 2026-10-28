package tradeview

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationName is the OpenTelemetry scope used by the client.
const InstrumentationName = "github.com/jsirianni/tradeview-go/pkg/tradeview"

// TelemetryOptions selects caller-owned OpenTelemetry providers. Nil providers
// and propagators use the global OpenTelemetry instances when the option is
// applied. The caller owns SDK setup, exporting, flushing, and shutdown.
type TelemetryOptions struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
}

// WithTelemetry enables tracing and metrics for every client operation.
// Without this option the client does not create telemetry or propagate traces.
// Only fixed operation/route names, HTTP status, and bounded error categories
// are recorded; payloads, credentials, IDs, URLs, and error text are excluded.
func WithTelemetry(options TelemetryOptions) ClientOption {
	return func(client *Client) error {
		config := options
		if config.TracerProvider == nil {
			config.TracerProvider = otel.GetTracerProvider()
		}
		if config.MeterProvider == nil {
			config.MeterProvider = otel.GetMeterProvider()
		}
		if config.Propagator == nil {
			config.Propagator = otel.GetTextMapPropagator()
		}
		meter := config.MeterProvider.Meter(InstrumentationName)
		t := &telemetry{tracer: config.TracerProvider.Tracer(InstrumentationName), propagator: config.Propagator}
		var err error
		if t.completed, err = meter.Int64Counter("tradeview.client.operations", metric.WithUnit("{operation}"), metric.WithDescription("Completed client operations")); err != nil {
			return err
		}
		if t.active, err = meter.Int64UpDownCounter("tradeview.client.active_operations", metric.WithUnit("{operation}"), metric.WithDescription("Operations awaiting completion")); err != nil {
			return err
		}
		if t.duration, err = meter.Float64Histogram("tradeview.client.operation.duration", metric.WithUnit("s"), metric.WithDescription("Operation duration through response consumption"), metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60)); err != nil {
			return err
		}
		if t.bytes, err = meter.Int64Histogram("tradeview.client.response.body.size", metric.WithUnit("By"), metric.WithDescription("Response bytes consumed by the caller or parser")); err != nil {
			return err
		}
		if t.frames, err = meter.Int64Counter("tradeview.client.stream.frames", metric.WithUnit("{frame}"), metric.WithDescription("Successfully decoded stream frames, including broker error frames")); err != nil {
			return err
		}
		client.telemetry = t
		return nil
	}
}

type telemetry struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	completed  metric.Int64Counter
	active     metric.Int64UpDownCounter
	duration   metric.Float64Histogram
	bytes      metric.Int64Histogram
	frames     metric.Int64Counter
}

type operationTelemetry struct {
	owner *telemetry
	ctx   context.Context
	span  trace.Span
	attrs []attribute.KeyValue
}

func (client *Client) startTelemetry(ctx context.Context, operation, method, route string, streaming bool) (context.Context, *operationTelemetry) {
	if client.telemetry == nil {
		return ctx, nil
	}
	t := client.telemetry
	attrs := []attribute.KeyValue{
		attribute.String("tradeview.operation", operation),
		attribute.String("http.request.method", method),
		attribute.String("url.template", route),
		attribute.Bool("tradeview.streaming", streaming),
	}
	ctx, span := t.tracer.Start(ctx, "tradeview."+operation, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	t.active.Add(ctx, 1, metric.WithAttributes(attrs...))
	return ctx, &operationTelemetry{owner: t, ctx: ctx, span: span, attrs: attrs}
}

func (o *operationTelemetry) prepare(request *http.Request) *http.Request {
	// Keep request-editor context values and cancellation while supplying the
	// operation span to the doer, even if an editor replaced the context.
	request = request.WithContext(trace.ContextWithSpan(request.Context(), o.span))
	o.owner.propagator.Inject(request.Context(), propagation.HeaderCarrier(request.Header))
	return request
}

func (o *operationTelemetry) frame() {
	o.owner.frames.Add(o.ctx, 1, metric.WithAttributes(o.attrs...))
}

func (o *operationTelemetry) finish(status int, size int64, duration time.Duration, category string) {
	attrs := append([]attribute.KeyValue(nil), o.attrs...)
	if status != 0 {
		attrs = append(attrs, attribute.Int("http.response.status_code", status))
	}
	if category != "" {
		attrs = append(attrs, attribute.String("error.type", category))
		o.span.SetStatus(codes.Error, category)
	}
	o.span.SetAttributes(attrs...)
	o.owner.completed.Add(o.ctx, 1, metric.WithAttributes(attrs...))
	o.owner.duration.Record(o.ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	o.owner.bytes.Record(o.ctx, size, metric.WithAttributes(attrs...))
	o.owner.active.Add(o.ctx, -1, metric.WithAttributes(o.attrs...))
	o.span.End()
}
