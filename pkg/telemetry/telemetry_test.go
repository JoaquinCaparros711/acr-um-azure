package telemetry

import (
	"context"
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

func Test_Should_InitializeAndShutdownTelemetry_When_ValidConfigProvided(t *testing.T) {
	// Arrange
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "v1.0.0",
		Environment:    "unit-test",
		UseStdout:      true,
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
