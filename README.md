# Go Fiber Hello World & Azure ACR Automation Project

This repository contains a lightweight **Go** web application built with the **[Fiber](https://gofiber.io/)** framework, along with Python automation scripts for building, tagging, publishing to **Azure Container Registry (ACR)**, and deploying to **Azure Container Instances (ACI)**.

## Project Structure

- [`main.go`](main.go): Go HTTP server implementation with `/` and `/health` endpoints exposed on port `8080`.
- [`main_test.go`](main_test.go): Unit tests for the Go Fiber application.
- [`go.mod`](go.mod): Go module dependency definitions.
- [`go.sum`](go.sum): Checksums of module dependencies.
- [`Dockerfile`](Dockerfile): Multi-stage Dockerfile provided for packaging the Go application into a minimal distroless image.
- [`build.py`](build.py): Python automation script for Docker build, Azure ACR login, image tagging, pushing, and ACI deployment.
- [`test_build.py`](test_build.py): Unit tests for the `build.py` automation script.

---

## Local Execution with Docker (Manual)

To build and run the Go application container manually using Docker:

```bash
# Build local Docker image
docker build -t app-azure:v1.0.0 .

# Run container on port 8080
docker run -d -p 8080:8080 --name fiber-app app-azure:v1.0.0
```

---

## Python Automation Script (`build.py`)

The `build.py` script automates the complete workflow:
1. Building local Docker image (`docker build`).
2. Authenticating with Azure Container Registry (`az acr login`).
3. Tagging remote image (`docker tag`).
4. Pushing image to registry (`docker push`).
5. (Optional) Deploying container instance to Azure (`az container create`).

### Usage Examples

```bash
# Full workflow (Build -> ACR Login -> Tag -> Push)
python3 build.py

# Simulation mode without executing shell commands (Dry Run)
python3 build.py --dry-run

# Workflow with automated deployment to Azure Container Instances (ACI)
python3 build.py --deploy

# Custom parameters execution
python3 build.py --registry myregistry --image myapp --tag v1.2.0 --dry-run
```

---

## Running Unit Tests

### Go Server Tests
```bash
go test -v ./...
```

### Python Automation Tests
```bash
python3 -m unittest test_build.py
```

---

## API Verification

Once the application is running, test the HTTP endpoints:

```bash
curl http://localhost:8080/
```

Expected JSON response:
```json
{
  "message": "Hello World from Go and Fiber!",
  "status": "success"
}
```

Health check endpoint:
```bash
curl http://localhost:8080/health
```

Expected JSON response:
```json
{
  "status": "healthy"
}
```
  