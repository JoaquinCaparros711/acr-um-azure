package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const loggerName = "acr-um-azure"

// Config defines the configuration options for OpenTelemetry provider.
type Config struct {
	ServiceName           string
	ServiceVersion        string
	Environment           string
	OtlpEndpoint          string
	OtlpProtocol          string
	OtlpHeaders           map[string]string
	AppInsightsConnString string
	UseStdout             bool
	StdoutWriter          io.Writer
	ExportInterval        time.Duration
}

// Provider encapsulates the initialized OpenTelemetry Tracer, Meter and Logger providers.
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	loggerProvider *sdklog.LoggerProvider
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

	appInsightsConn := sanitizeValue(os.Getenv("APPLICATIONINSIGHTS_CONNECTION_STRING"))

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	otlpProtocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	otlpHeaders := parseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))

	// A connection string identifies Application Insights but is not an OTLP endpoint.
	// OTLP must be sent to a Collector or to Azure Monitor OTLP ingestion endpoints.
	useStdout := os.Getenv("OTEL_EXPORTER_STDOUT") == "true" || otlpEndpoint == ""

	return Config{
		ServiceName:           serviceName,
		ServiceVersion:        serviceVersion,
		Environment:           environment,
		OtlpEndpoint:          otlpEndpoint,
		OtlpProtocol:          otlpProtocol,
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

	loggerProvider, err := initLoggerProvider(ctx, cfg, res)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)
		_ = meterProvider.Shutdown(ctx)
		return nil, nil, fmt.Errorf("failed to initialize logger provider: %w", err)
	}

	// Register global providers and W3C trace context propagator
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	global.SetLoggerProvider(loggerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Route stdlib slog through the OTel logger. slog.InfoContext(ctx, ...) now
	// carries trace_id/span_id automatically and is exported via OTLP.
	slog.SetDefault(slog.New(otelslog.NewHandler(loggerName)))

	provider := &Provider{
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
		loggerProvider: loggerProvider,
	}

	shutdown := func(shutdownCtx context.Context) error {
		var errs []error
		if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("error shutting down tracer provider: %w", err))
		}
		if err := meterProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("error shutting down meter provider: %w", err))
		}
		if err := loggerProvider.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("error shutting down logger provider: %w", err))
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
	} else if cfg.OtlpProtocol == "grpc" {
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(grpcEndpoint(cfg.OtlpEndpoint))}
		if len(cfg.OtlpHeaders) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.OtlpHeaders))
		}
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	} else {
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(cfg.OtlpEndpoint),
		}
		if len(cfg.OtlpHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.OtlpHeaders))
		}
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") {
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
	} else if cfg.OtlpProtocol == "grpc" {
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(grpcEndpoint(cfg.OtlpEndpoint))}
		if len(cfg.OtlpHeaders) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.OtlpHeaders))
		}
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		var exp sdkmetric.Exporter
		exp, err = otlpmetricgrpc.New(ctx, opts...)
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
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") {
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

func initLoggerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	var exporter sdklog.Exporter
	var err error

	if cfg.UseStdout {
		writer := cfg.StdoutWriter
		if writer == nil {
			writer = os.Stdout
		}
		exporter, err = stdoutlog.New(stdoutlog.WithWriter(writer), stdoutlog.WithPrettyPrint())
	} else if cfg.OtlpProtocol == "grpc" {
		opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(grpcEndpoint(cfg.OtlpEndpoint))}
		if len(cfg.OtlpHeaders) > 0 {
			opts = append(opts, otlploggrpc.WithHeaders(cfg.OtlpHeaders))
		}
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") {
			opts = append(opts, otlploggrpc.WithInsecure())
		}
		exporter, err = otlploggrpc.New(ctx, opts...)
	} else {
		opts := []otlploghttp.Option{otlploghttp.WithEndpoint(cfg.OtlpEndpoint)}
		if len(cfg.OtlpHeaders) > 0 {
			opts = append(opts, otlploghttp.WithHeaders(cfg.OtlpHeaders))
		}
		if !strings.HasPrefix(cfg.OtlpEndpoint, "https://") {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		exporter, err = otlploghttp.New(ctx, opts...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	), nil
}

func grpcEndpoint(endpoint string) string {
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	return strings.TrimSuffix(endpoint, "/")
}

// sanitizeValue strips spaces, quotes, and dollar signs from configuration strings.
func sanitizeValue(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimPrefix(s, "$")
	s = strings.Trim(s, `"'`)
	return strings.TrimSpace(s)
}

// parseAppInsightsConnectionString extracts IngestionEndpoint and InstrumentationKey from Azure Connection String.
func parseAppInsightsConnectionString(connStr string) (string, string) {
	var endpoint, ikey string
	connStr = sanitizeValue(connStr)
	parts := strings.Split(connStr, ";")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(sanitizeValue(kv[0]))
		val := sanitizeValue(kv[1])
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
