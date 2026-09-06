package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config defines the configuration options for OpenTelemetry provider.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OtlpEndpoint   string
	UseStdout      bool
}

// Provider encapsulates the initialized OpenTelemetry Tracer and Meter providers.
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
}

// LoadConfigFromEnv builds Config using standard environment variables with safe fallbacks.
func LoadConfigFromEnv() Config {
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "acr-um-azure-app"
	}

	serviceVersion := os.Getenv("IMAGE_TAG")
	if serviceVersion == "" {
		serviceVersion = "v1.0.0"
	}

	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		environment = "production"
	}

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	useStdout := os.Getenv("OTEL_EXPORTER_STDOUT") == "true" || otlpEndpoint == ""

	return Config{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Environment:    environment,
		OtlpEndpoint:   otlpEndpoint,
		UseStdout:      useStdout,
	}
}

// InitTelemetry initializes the global OpenTelemetry TracerProvider, MeterProvider, and Propagator.
func InitTelemetry(ctx context.Context, cfg Config) (*Provider, func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create telemetry resource: %w", err)
	}

	tracerProvider, err := initTracerProvider(ctx, cfg, res)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize tracer provider: %w", err)
	}

	meterProvider, err := initMeterProvider(ctx, cfg, res)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)
		return nil, nil, fmt.Errorf("failed to initialize meter provider: %w", err)
	}

	// Register global providers and W3C trace context propagator
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	provider := &Provider{
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
	}

	shutdown := func(shutdownCtx context.Context) error {
		var errs []error
		if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("error shutting down tracer provider: %w", err))
		}
		if err := meterProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("error shutting down meter provider: %w", err))
		}
		return errors.Join(errs...)
	}

	log.Printf("[Telemetry] Initialized for service=%s, version=%s, stdout_fallback=%v",
		cfg.ServiceName, cfg.ServiceVersion, cfg.UseStdout)

	return provider, shutdown, nil
}

func initTracerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	var err error

	if cfg.UseStdout {
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	} else {
		exporter, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.OtlpEndpoint),
			otlptracehttp.WithInsecure(),
		)
	}

	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	return tp, nil
}

func initMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	var reader sdkmetric.Reader
	var err error

	if cfg.UseStdout {
		var exp sdkmetric.Exporter
		exp, err = stdoutmetric.New(stdoutmetric.WithPrettyPrint())
		if err == nil {
			reader = sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(15*time.Second))
		}
	} else {
		var exp sdkmetric.Exporter
		exp, err = otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpoint(cfg.OtlpEndpoint),
			otlpmetrichttp.WithInsecure(),
		)
		if err == nil {
			reader = sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(15*time.Second))
		}
	}

	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)

	return mp, nil
}
