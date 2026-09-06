# Go Fiber & Azure OpenTelemetry Observability Project

This repository contains a production-grade **Go** web application built with the **[Fiber](https://gofiber.io/)** framework and instrumented with **[OpenTelemetry](https://opentelemetry.io/)** for distributed tracing and metrics, seamlessly integrating with **Azure Monitor / Application Insights**, along with CI/CD automation for **Azure Container Registry (ACR)** and **Azure Container Apps (ACA)**.

---

## Architecture & Project Structure

- [`main.go`](main.go): Go HTTP server with OpenTelemetry tracing and metrics instrumentation, child span demos, and graceful termination.
- [`main_test.go`](main_test.go): Comprehensive integration and endpoint unit tests adhering to AAA pattern.
- [`pkg/telemetry/telemetry.go`](pkg/telemetry/telemetry.go): OpenTelemetry TracerProvider and MeterProvider lifecycle management, resource attribution, and OTLP / stdout exporters.
- [`pkg/telemetry/middleware.go`](pkg/telemetry/middleware.go): Fiber HTTP middleware for W3C trace context extraction, latency recording, error tracking, and response header injection (`X-Trace-Id`).
- [`pkg/telemetry/telemetry_test.go`](pkg/telemetry/telemetry_test.go): Unit test suite for the telemetry provider and middleware.
- [`Dockerfile`](Dockerfile): Multi-stage containerization with distroless runtime (`linux/amd64`).
- [`.github/workflows/ci.yml`](.github/workflows/ci.yml): 3-stage CI/CD pipeline (Test -> Build & Push ACR -> Deploy Azure Container App).

---

## OpenTelemetry Features

### 1. Distributed Tracing
- **W3C TraceContext Propagation:** Extracts and propagates incoming trace headers across microservices.
- **Span Attributes:** Automatically captures HTTP method, route, status code, user agent, client IP, and process metadata.
- **Trace ID Injection:** Automatically attaches `X-Trace-Id` header to every HTTP response for end-to-end correlation.
- **Child Spans:** Support for nested business operations (demonstrated in `/api/v1/telemetry-demo`).

### 2. Metrics & Observability
- `http_server_requests_total`: Counter tracking total requests partitioned by method, route, and status code.
- `http_server_duration_milliseconds`: Explicit histogram measuring request execution latencies.
- `http_server_active_requests`: Real-time gauge for concurrent active connections.

---

## Environment Variables Configuration

| Variable | Description | Default |
| :--- | :--- | :--- |
| `OTEL_SERVICE_NAME` | Name of the service in Azure Monitor / OTLP | `acr-um-azure-app` |
| `ENVIRONMENT` | Deployment environment tag | `production` |
| `IMAGE_TAG` | Application version attached to telemetry | `v1.0.0` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP HTTP endpoint (e.g. OpenTelemetry Collector or Azure OTLP) | `""` *(stdout fallback)* |
| `OTEL_EXPORTER_STDOUT` | Force stdout printing of traces and metrics | `true` *(when endpoint is empty)* |
| `PORT` | HTTP server listening port | `8080` |

---

## Local Execution & Verification

### Run locally:
```bash
go run main.go
```

### Run with Docker:
```bash
docker build -t acr-um-azure:v1.0.0 .
docker run -p 8080:8080 -e OTEL_EXPORTER_STDOUT=true acr-um-azure:v1.0.0
```

### Test HTTP Endpoints:

1. **Root endpoint:**
```bash
curl -i http://localhost:8080/
```

2. **Health check endpoint:**
```bash
curl -i http://localhost:8080/health
```

3. **OpenTelemetry Child Spans Demo:**
```bash
curl -i http://localhost:8080/api/v1/telemetry-demo
```

---

## Running Unit & Integration Tests

```bash
go test -v -race -cover ./...
```