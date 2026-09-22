package telemetry

import (
	"context"

	"github.com/gofiber/contrib/otelfiber/v2"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "acr-um-azure/http-server"

// FiberMiddleware returns the official otelfiber middleware plus a small
// wrapper that exposes the current trace_id in the X-Trace-Id response header.
// serviceName is unused: it's already carried by the TracerProvider resource.
func FiberMiddleware(_ string) []fiber.Handler {
	return []fiber.Handler{
		otelfiber.Middleware(
			otelfiber.WithTracerProvider(otel.GetTracerProvider()),
			otelfiber.WithMeterProvider(otel.GetMeterProvider()),
		),
		exposeTraceIDHeader,
	}
}

func exposeTraceIDHeader(c *fiber.Ctx) error {
	if id := trace.SpanFromContext(c.UserContext()).SpanContext().TraceID(); id.IsValid() {
		c.Set("X-Trace-Id", id.String())
	}
	return c.Next()
}

// GetTracer returns a named tracer for child span creation in business logic.
func GetTracer() trace.Tracer {
	return otel.GetTracerProvider().Tracer(tracerName)
}

// StartSpan creates a child span with standard context propagation.
func StartSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return GetTracer().Start(ctx, spanName, trace.WithAttributes(attrs...))
}
