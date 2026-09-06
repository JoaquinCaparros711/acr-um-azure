package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName = "acr-um-azure/http-server"
	meterName  = "acr-um-azure/http-metrics"
)

// HTTPMetrics holds metric instruments for HTTP request lifecycle.
type HTTPMetrics struct {
	requestCounter metric.Int64Counter
	durationHist   metric.Float64Histogram
	activeRequests metric.Int64UpDownCounter
}

// NewHTTPMetrics initializes Prometheus and Azure Monitor-compatible OpenTelemetry metric instruments.
func NewHTTPMetrics() (*HTTPMetrics, error) {
	meter := otel.GetMeterProvider().Meter(meterName)

	reqCounter, err := meter.Int64Counter(
		"http_server_requests_total",
		metric.WithDescription("Total count of HTTP requests processed by the server"),
		metric.WithUnit("{requests}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request counter: %w", err)
	}

	durHist, err := meter.Float64Histogram(
		"http_server_duration_milliseconds",
		metric.WithDescription("Duration of HTTP requests in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create duration histogram: %w", err)
	}

	activeReqs, err := meter.Int64UpDownCounter(
		"http_server_active_requests",
		metric.WithDescription("Number of concurrent active HTTP requests"),
		metric.WithUnit("{requests}"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create active requests gauge: %w", err)
	}

	return &HTTPMetrics{
		requestCounter: reqCounter,
		durationHist:   durHist,
		activeRequests: activeReqs,
	}, nil
}

// FiberMiddleware creates a high-performance OpenTelemetry tracing and metrics middleware for Fiber.
func FiberMiddleware(serviceName string) fiber.Handler {
	tracer := otel.GetTracerProvider().Tracer(tracerName)
	metrics, err := NewHTTPMetrics()
	if err != nil {
		fmt.Printf("[Telemetry] Warning: metrics init error: %v\n", err)
	}

	return func(c *fiber.Ctx) error {
		start := time.Now()
		reqHeader := make(http.Header)
		c.Request().Header.VisitAll(func(k, v []byte) {
			reqHeader.Add(string(k), string(v))
		})

		// Extract incoming W3C trace context from HTTP headers
		ctx := otel.GetTextMapPropagator().Extract(c.Context(), propagation.HeaderCarrier(reqHeader))

		spanName := fmt.Sprintf("HTTP %s %s", c.Method(), c.Path())
		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Method()),
				semconv.URLPath(c.Path()),
				semconv.UserAgentOriginal(string(c.Request().Header.UserAgent())),
				semconv.ClientAddress(c.IP()),
				attribute.String("service.name", serviceName),
			),
		)
		defer span.End()

		// Inject trace ID into Fiber response headers for end-to-end tracing observability
		traceID := span.SpanContext().TraceID().String()
		if traceID != "" {
			c.Set("X-Trace-Id", traceID)
		}

		// Propagate context to Fiber handlers
		c.SetUserContext(ctx)

		if metrics != nil {
			metrics.activeRequests.Add(ctx, 1)
			defer metrics.activeRequests.Add(ctx, -1)
		}

		// Process request down the middleware chain
		err := c.Next()

		statusCode := c.Response().StatusCode()
		elapsed := float64(time.Since(start).Microseconds()) / 1000.0

		// Set status and error attributes on span
		span.SetAttributes(
			semconv.HTTPResponseStatusCode(statusCode),
		)

		if statusCode >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d internal server error", statusCode))
			if err != nil {
				span.RecordError(err)
			}
		} else if statusCode >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d client error", statusCode))
		} else {
			span.SetStatus(codes.Ok, "OK")
		}

		// Record latency and request count metrics
		if metrics != nil {
			metricAttrs := metric.WithAttributes(
				attribute.String("http.method", c.Method()),
				attribute.String("http.route", c.Path()),
				attribute.Int("http.status_code", statusCode),
			)
			metrics.requestCounter.Add(ctx, 1, metricAttrs)
			metrics.durationHist.Record(ctx, elapsed, metricAttrs)
		}

		return err
	}
}

// GetTracer returns a named tracer for child span creation in business logic.
func GetTracer() trace.Tracer {
	return otel.GetTracerProvider().Tracer(tracerName)
}

// StartSpan creates a child span with standard context propagation.
func StartSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return GetTracer().Start(ctx, spanName, trace.WithAttributes(attrs...))
}
