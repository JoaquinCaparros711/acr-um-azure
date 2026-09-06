# Go Fiber Microservice with OpenTelemetry & Azure Cloud Automation

This repository contains a high-performance **Go** REST API built with the **[Fiber](https://gofiber.io/)** framework, instrumented with **[OpenTelemetry](https://opentelemetry.io/)** for distributed tracing and application metrics, and fully integrated with **Microsoft Azure** (Azure Monitor, Application Insights, Azure Container Registry, and Azure Container Apps).

---

## Key Features

- **OpenTelemetry Distributed Tracing:**
  - Standard W3C TraceContext & Baggage context propagation.
  - Automatic `X-Trace-Id` response header injection.
  - Child spans demonstration for nested internal operations (`/api/v1/telemetry-demo`).
- **OpenTelemetry Application Metrics:**
  - `http_server_requests_total`: Counter tracking total requests tagged by method, path, and status code.
  - `http_server_duration_milliseconds`: Latency histogram.
  - `http_server_active_requests`: Real-time gauge for concurrent active connections.
- **Azure Observability Integration:**
  - Direct ingestion with **Azure Application Insights** via `APPLICATIONINSIGHTS_CONNECTION_STRING` or OTLP collector endpoint (`OTEL_EXPORTER_OTLP_ENDPOINT`).
  - Automatic fallback to stdout exporter for local development and offline testing.
- **Resilient Infrastructure:**
  - Non-blocking graceful server and telemetry provider shutdown (`SIGINT`, `SIGTERM`).
  - Distroless multi-stage containerization.
  - Automated CI/CD pipeline with semantic versioning.

---

## Project Structure

- [`main.go`](main.go): Server entry point, route definitions, and graceful shutdown lifecycle.
- [`main_test.go`](main_test.go): Unit tests for HTTP routes and trace metadata validation.
- [`pkg/telemetry/telemetry.go`](pkg/telemetry/telemetry.go): OpenTelemetry TracerProvider, MeterProvider, and Azure exporter factory.
- [`pkg/telemetry/middleware.go`](pkg/telemetry/middleware.go): High-performance Fiber middleware for tracing, metrics, and child spans.
- [`pkg/telemetry/telemetry_test.go`](pkg/telemetry/telemetry_test.go): Unit tests for telemetry package components.
- [`Dockerfile`](Dockerfile): Multi-stage container build targeting static distroless Debian 12.
- [`build.py`](build.py): Automation script for local Docker build, ACR login, tag, push, and Azure deployment.
- [`test_build.py`](test_build.py): Unit tests for build automation script.

---

## Telemetry & Azure Configuration

The application loads observability configuration from standard environment variables:

| Variable | Description | Default |
| :--- | :--- | :--- |
| `APPLICATIONINSIGHTS_CONNECTION_STRING` | Azure Application Insights connection string | _(empty)_ |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Remote OTLP HTTP collector endpoint | _(empty)_ |
| `OTEL_EXPORTER_OTLP_HEADERS` | Comma-separated headers (e.g. `x-api-key=...`) | _(empty)_ |
| `OTEL_SERVICE_NAME` | Service name reported in Azure Monitor | `acr-um-azure-app` |
| `ENVIRONMENT` | Deployment environment tag (`production`, `staging`, `dev`) | `production` |
| `OTEL_EXPORTER_STDOUT` | Force JSON telemetry logs to standard output | `true` (when no cloud endpoint is set) |
| `PORT` | HTTP server listening port | `8080` |

---

## Local Execution

### Option 1: Run Locally with Go (Stdout Telemetry)
```bash
# Telemetry automatically exports to stdout in local mode
go run main.go
```

### Option 2: Run with Azure Application Insights
```bash
export APPLICATIONINSIGHTS_CONNECTION_STRING="InstrumentationKey=your-key;IngestionEndpoint=https://eastus-8.in.applicationinsights.azure.com/"
export OTEL_SERVICE_NAME="acr-um-azure-app"
export ENVIRONMENT="staging"

go run main.go
```

### Option 3: Run with Docker
```bash
# Build Docker image
docker build -t app-azure:v1.0.0 .

# Run container on port 8080
docker run -d -p 8080:8080 --name fiber-app app-azure:v1.0.0
```

---

## API Endpoints & Verification

### 1. Root Endpoint (`GET /`)
```bash
curl -i http://localhost:8080/
```
**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Trace-Id: 4bf92f3577b34da6a3ce929d0e0e4736

{
  "message": "Hello World from Go and Fiber with OpenTelemetry Traces & Metrics!",
  "status": "success",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

### 2. Health Check (`GET /health`)
```bash
curl -i http://localhost:8080/health
```
**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Trace-Id: 5c885bb321d24c0ea3ce929d0e0e4736

{
  "status": "healthy",
  "timestamp": "2026-09-06T14:00:00Z",
  "trace_id": "5c885bb321d24c0ea3ce929d0e0e4736"
}
```

### 3. Distributed Tracing Demo with Child Spans (`GET /api/v1/telemetry-demo`)
```bash
curl -i http://localhost:8080/api/v1/telemetry-demo
```
**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Trace-Id: 9a01bf3577b34da6a3ce929d0e0e4736

{
  "child_spans": [
    "simulate_database_query",
    "simulate_azure_service_call"
  ],
  "message": "Distributed tracing demonstration with child spans completed",
  "status": "success",
  "trace_id": "9a01bf3577b34da6a3ce929d0e0e4736"
}
```

---

## Running Test Suites

### Go Unit & Integration Tests (with Race Detector & Coverage)
```bash
go test -v -race -cover ./...
```

### Python Automation Tests
```bash
python3 -m unittest test_build.py
```