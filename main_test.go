package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"acr-um-azure/pkg/telemetry"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ResponsePayload struct {
	Status     string   `json:"status"`
	Message    string   `json:"message,omitempty"`
	TraceID    string   `json:"trace_id,omitempty"`
	Timestamp  string   `json:"timestamp,omitempty"`
	ChildSpans []string `json:"child_spans,omitempty"`
}

func testDependencies() AppDependencies {
	return AppDependencies{
		TelemetryConfig: telemetry.Config{
			ServiceName:    "acr-um-azure-test",
			ServiceVersion: "v1.0.0-test",
			Environment:    "test",
			UseStdout:      false,
		},
	}
}

func Test_Should_ReturnSuccessMessageAndTraceID_When_GetRootEndpoint(t *testing.T) {
	// Arrange
	ctx := context.Background()
	_, shutdown, err := telemetry.InitTelemetry(ctx, testDependencies().TelemetryConfig)
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()

	app := SetupApp(testDependencies())
	req := httptest.NewRequest("GET", "/", nil)

	// Act
	resp, err := app.Test(req, -1)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("X-Trace-Id"))

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload ResponsePayload
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	assert.Equal(t, "success", payload.Status)
	assert.Contains(t, payload.Message, "OpenTelemetry Traces & Metrics")
	assert.NotEmpty(t, payload.TraceID)
}

func Test_Should_ReturnHealthyStatusAndTraceID_When_GetHealthEndpoint(t *testing.T) {
	// Arrange
	ctx := context.Background()
	_, shutdown, err := telemetry.InitTelemetry(ctx, testDependencies().TelemetryConfig)
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()

	app := SetupApp(testDependencies())
	req := httptest.NewRequest("GET", "/health", nil)

	// Act
	resp, err := app.Test(req, -1)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("X-Trace-Id"))

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload ResponsePayload
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	assert.Equal(t, "healthy", payload.Status)
	assert.NotEmpty(t, payload.Timestamp)
	assert.NotEmpty(t, payload.TraceID)
}

func Test_Should_ExecuteChildSpansSuccessfully_When_GetTelemetryDemoEndpoint(t *testing.T) {
	// Arrange
	ctx := context.Background()
	_, shutdown, err := telemetry.InitTelemetry(ctx, testDependencies().TelemetryConfig)
	require.NoError(t, err)
	defer func() { _ = shutdown(ctx) }()

	app := SetupApp(testDependencies())
	req := httptest.NewRequest("GET", "/api/v1/telemetry-demo", nil)

	// Act
	resp, err := app.Test(req, -1)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("X-Trace-Id"))

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload ResponsePayload
	err = json.Unmarshal(bodyBytes, &payload)
	require.NoError(t, err)
	assert.Equal(t, "success", payload.Status)
	assert.Contains(t, payload.Message, "Distributed tracing demonstration")
	assert.Len(t, payload.ChildSpans, 2)
	assert.Contains(t, payload.ChildSpans, "simulate_database_query")
	assert.Contains(t, payload.ChildSpans, "simulate_azure_service_call")
}
