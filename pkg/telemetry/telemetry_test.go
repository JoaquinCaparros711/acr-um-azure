package telemetry

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func Test_Should_LoadConfigFromEnv_When_EnvironmentVariablesAreProvided(t *testing.T) {
	// Arrange
	_ = os.Setenv("OTEL_SERVICE_NAME", "custom-service")
	_ = os.Setenv("IMAGE_TAG", "v2.5.0")
	_ = os.Setenv("ENVIRONMENT", "staging")
	_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4318")
	_ = os.Setenv("OTEL_EXPORTER_STDOUT", "false")
	defer func() {
		_ = os.Unsetenv("OTEL_SERVICE_NAME")
		_ = os.Unsetenv("IMAGE_TAG")
		_ = os.Unsetenv("ENVIRONMENT")
		_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		_ = os.Unsetenv("OTEL_EXPORTER_STDOUT")
	}()

	// Act
	cfg := LoadConfigFromEnv()

	// Assert
	assert.Equal(t, "custom-service", cfg.ServiceName)
	assert.Equal(t, "v2.5.0", cfg.ServiceVersion)
	assert.Equal(t, "staging", cfg.Environment)
	assert.Equal(t, "http://otel-collector:4318", cfg.OtlpEndpoint)
	assert.False(t, cfg.UseStdout)
}

func Test_Should_ParseAppInsightsConnectionString_When_Provided(t *testing.T) {
	// Arrange
	connStr := "InstrumentationKey=11111111-2222-3333-4444-555555555555;IngestionEndpoint=https://eastus-8.in.applicationinsights.azure.com/;LiveEndpoint=https://eastus.livediagnostics.monitor.azure.com/"
	_ = os.Setenv("APPLICATIONINSIGHTS_CONNECTION_STRING", connStr)
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	defer func() {
		_ = os.Unsetenv("APPLICATIONINSIGHTS_CONNECTION_STRING")
	}()

	// Act
	cfg := LoadConfigFromEnv()

	// Assert
	assert.Equal(t, "eastus-8.in.applicationinsights.azure.com", cfg.OtlpEndpoint)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", cfg.OtlpHeaders["x-api-key"])
}

func Test_Should_InitializeAndShutdownTelemetry_When_ValidConfigProvided(t *testing.T) {
	// Arrange
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "v1.0.0",
		Environment:    "unit-test",
		UseStdout:      true,
		StdoutWriter:   io.Discard,
		ExportInterval: 1 * time.Second,
	}

	// Act
	provider, shutdown, err := InitTelemetry(ctx, cfg)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, provider)
	require.NotNil(t, shutdown)

	// Clean Shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err = shutdown(shutdownCtx)
	assert.NoError(t, err)
}

func Test_Should_InjectTraceHeaderAndRecordMetrics_When_RequestPassesThroughMiddleware(t *testing.T) {
	// Arrange
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-middleware-service",
		ServiceVersion: "v1.0.0",
		Environment:    "test",
		UseStdout:      true,
		StdoutWriter:   io.Discard,
		ExportInterval: 1 * time.Second,
	}
	_, shutdown, err := InitTelemetry(ctx, cfg)
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()

	app := fiber.New()
	app.Use(FiberMiddleware(cfg.ServiceName))
	app.Get("/test-route", func(c *fiber.Ctx) error {
		reqCtx := c.UserContext()
		_, span := StartSpan(reqCtx, "inner-computation", attribute.String("calc.type", "hash"))
		defer span.End()

		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test-route", nil)

	// Act
	resp, err := app.Test(req, -1)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	traceID := resp.Header.Get("X-Trace-Id")
	assert.NotEmpty(t, traceID)
	assert.Len(t, traceID, 32)
}

func Test_Should_CaptureErrorAndStatusAttributes_When_HandlerReturnsError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-error-service",
		ServiceVersion: "v1.0.0",
		Environment:    "test",
		UseStdout:      true,
		StdoutWriter:   io.Discard,
		ExportInterval: 1 * time.Second,
	}
	_, shutdown, err := InitTelemetry(ctx, cfg)
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()

	app := fiber.New()
	app.Use(FiberMiddleware(cfg.ServiceName))
	app.Get("/error-500", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "database connection failed")
	})
	app.Get("/error-400", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusBadRequest, "invalid parameter")
	})

	// Act 1: 500 Internal Server Error
	req500 := httptest.NewRequest("GET", "/error-500", nil)
	resp500, err500 := app.Test(req500, -1)
	require.NoError(t, err500)
	assert.Equal(t, 500, resp500.StatusCode)
	assert.NotEmpty(t, resp500.Header.Get("X-Trace-Id"))

	// Act 2: 400 Bad Request
	req400 := httptest.NewRequest("GET", "/error-400", nil)
	resp400, err400 := app.Test(req400, -1)
	require.NoError(t, err400)
	assert.Equal(t, 400, resp400.StatusCode)
	assert.NotEmpty(t, resp400.Header.Get("X-Trace-Id"))
}

func Test_Should_ParseHeadersCorrectly(t *testing.T) {
	// Arrange
	raw := "x-api-key=secret123,Authorization=Bearer tokenXYZ"

	// Act
	parsed := parseHeaders(raw)

	// Assert
	assert.Equal(t, "secret123", parsed["x-api-key"])
	assert.Equal(t, "Bearer tokenXYZ", parsed["Authorization"])
}
