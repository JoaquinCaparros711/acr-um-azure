package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
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
	ServiceName           string
	ServiceVersion        string
	Environment           string
	OtlpEndpoint          string
	OtlpHeaders           map[string]string
	AppInsightsConnString string
	UseStdout             bool
	StdoutWriter          io.Writer
	ExportInterval        time.Duration
}

// Provider encapsulates the initialized OpenTelemetry Tracer and Meter providers.
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
}

// ShutdownFunc cleans up OpenTelemetry background exporters and flushes pending telemetry.
type ShutdownFunc func(context.Context) error

// LoadConfigFromEnv builds Config using standard environment variables with safe defaults.
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

	appInsightsConn := os.Getenv("APPLICATIONINSIGHTS_CONNECTION_STRING")
	if appInsightsConn == "" {
		appInsightsConn = os.Getenv("CONNECTION_STRING")
	}
	if appInsightsConn == "" {
		appInsightsConn = os.Getenv("APPLICATIONINSIGHTS")
	}
	if appInsightsConn == "" {
		appInsightsConn = os.Getenv("APPLICAITONINSIGHTS")
	}
	if appInsightsConn == "" {
		appInsightsConn = os.Getenv("APPINSIGHTS_CONNECTION_STRING")
	}
	appInsightsConn = strings.TrimPrefix(strings.TrimSpace(appInsightsConn), "$")
	appInsightsConn = strings.Trim(appInsightsConn, `"'`)

	instrumentationKey := os.Getenv("INSTRUMENTATION_KEY")
	if instrumentationKey == "" {
		instrumentationKey = os.Getenv("INSTRUMENTATION")
	}
	if instrumentationKey == "" {
		instrumentationKey = os.Getenv("APPINSIGHTS_INSTRUMENTATIONKEY")
	}
	if instrumentationKey == "" {
		instrumentationKey = os.Getenv("APPLICATIONINSIGHTS_INSTRUMENTATION_KEY")
	}
	instrumentationKey = strings.TrimPrefix(strings.TrimSpace(instrumentationKey), "$")
	instrumentationKey = strings.Trim(instrumentationKey, `"'`)

	if appInsightsConn == "" && instrumentationKey != "" {
		appInsightsConn = "InstrumentationKey=" + instrumentationKey
	}

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	otlpHeaders := parseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))

	// If Azure App Insights connection string is provided and no explicit OTLP endpoint is set,
	// parse the ingestion endpoint and instrumentation key from connection string.
	if appInsightsConn != "" && otlpEndpoint == "" {
		ingestionEndpoint, ikey := parseAppInsightsConnectionString(appInsightsConn)
		if ingestionEndpoint != "" {
			otlpEndpoint = ingestionEndpoint
		}
		if ikey != "" && otlpHeaders == nil {
			otlpHeaders = map[string]string{"x-api-key": ikey}
		}
	}

	// Default to stdout export if no remote endpoint is configured or if explicitly enabled
	useStdout := os.Getenv("OTEL_EXPORTER_STDOUT") == "true" || (otlpEndpoint == "" && appInsightsConn == "")

	return Config{
		ServiceName:           serviceName,
		ServiceVersion:        serviceVersion,
		Environment:           environment,
		OtlpEndpoint:          otlpEndpoint,
		OtlpHeaders:           otlpHeaders,
		AppInsightsConnString: appInsightsConn,
		UseStdout:             useStdout,
		StdoutWriter:          os.Stdout,
		ExportInterval:        15 * time.Second,
	}
}

// InitTelemetry initializes the global OpenTelemetry TracerProvider, MeterProvider, and W3C Propagator.
func InitTelemetry(ctx context.Context, cfg Config) (*Provider, ShutdownFunc, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
			attribute.String("cloud.provider", "azure"),
			attribute.String("ai.cloud.role", cfg.ServiceName),
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

	log.Printf("[Telemetry] Initialized: service=%s, version=%s, env=%s, stdout=%v, endpoint=%s",
		cfg.ServiceName, cfg.ServiceVersion, cfg.Environment, cfg.UseStdout, cfg.OtlpEndpoint)

	return provider, shutdown, nil
}

func initTracerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	var err error

	if cfg.UseStdout {
		writer := cfg.StdoutWriter
		if writer == nil {
			writer = os.Stdout
		}
		exporter, err = stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
			stdouttrace.WithWriter(writer),
		)
	} else {
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(cfg.OtlpEndpoint),
		}
		if len(cfg.OtlpHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.OtlpHeaders))
		}
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") && !strings.Contains(cfg.OtlpEndpoint, ":443") {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create span exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	return tp, nil
}

func initMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	var reader sdkmetric.Reader
	var err error

	interval := cfg.ExportInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}

	if cfg.UseStdout {
		writer := cfg.StdoutWriter
		if writer == nil {
			writer = os.Stdout
		}
		var exp sdkmetric.Exporter
		exp, err = stdoutmetric.New(
			stdoutmetric.WithPrettyPrint(),
			stdoutmetric.WithWriter(writer),
		)
		if err == nil {
			reader = sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval))
		}
	} else {
		opts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(cfg.OtlpEndpoint),
		}
		if len(cfg.OtlpHeaders) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(cfg.OtlpHeaders))
		}
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") && !strings.Contains(cfg.OtlpEndpoint, ":443") {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}

		var exp sdkmetric.Exporter
		exp, err = otlpmetrichttp.New(ctx, opts...)
		if err == nil {
			reader = sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval))
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create metric reader: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)

	return mp, nil
}

// parseAppInsightsConnectionString extracts IngestionEndpoint and InstrumentationKey from Azure Connection String.
func parseAppInsightsConnectionString(connStr string) (string, string) {
	var endpoint, ikey string
	parts := strings.Split(connStr, ";")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(kv[0])
		val := kv[1]
		if key == "ingestionendpoint" {
			endpoint = strings.TrimPrefix(val, "https://")
			endpoint = strings.TrimPrefix(endpoint, "http://")
			endpoint = strings.TrimSuffix(endpoint, "/")
		} else if key == "instrumentationkey" {
			ikey = val
		}
	}
	if endpoint == "" && ikey != "" {
		endpoint = "in.applicationinsights.azure.com"
	}
	return endpoint, ikey
}

// parseHeaders parses comma-separated key=value strings into a map.
func parseHeaders(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	headers := make(map[string]string)
	pairs := strings.Split(raw, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 {
			headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return headers
}
