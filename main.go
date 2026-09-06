package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"acr-um-azure/pkg/telemetry"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// AppDependencies contains injected dependencies for clean architecture and testing.
type AppDependencies struct {
	TelemetryConfig telemetry.Config
}

// SetupApp initializes and configures the Fiber application HTTP routes and middleware.
func SetupApp(deps AppDependencies) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "Hello World Fiber App v1.0 (with OpenTelemetry)",
		DisableStartupMessage: false,
	})

	// Global Middlewares
	app.Use(recover.New())
	app.Use(telemetry.FiberMiddleware(deps.TelemetryConfig.ServiceName))

	// Register Routes
	registerRoutes(app)

	return app
}

func registerRoutes(app *fiber.App) {
	// Root endpoint GET /
	app.Get("/", handleRoot)

	// Health check endpoint GET /health
	app.Get("/health", handleHealth)

	// Telemetry demonstration endpoint GET /api/v1/telemetry-demo
	app.Get("/api/v1/telemetry-demo", handleTelemetryDemo)
}

func handleRoot(c *fiber.Ctx) error {
	ctx := c.UserContext()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("app.handler", "root"))

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":   "success",
		"message":  "Hello World from Go and Fiber with OpenTelemetry Traces & Metrics!",
		"trace_id": span.SpanContext().TraceID().String(),
	})
}

func handleHealth(c *fiber.Ctx) error {
	ctx := c.UserContext()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("app.health.status", "healthy"))

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"trace_id":  span.SpanContext().TraceID().String(),
	})
}

func handleTelemetryDemo(c *fiber.Ctx) error {
	ctx := c.UserContext()

	// Child Span 1: Simulated Database Query
	childCtx, dbSpan := telemetry.StartSpan(ctx, "simulate_database_query",
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "SELECT"),
	)
	time.Sleep(10 * time.Millisecond) // Simulated non-blocking work
	dbSpan.End()

	// Child Span 2: Simulated External Azure Service Call
	_, apiSpan := telemetry.StartSpan(childCtx, "simulate_azure_service_call",
		attribute.String("http.target", "https://management.azure.com"),
		attribute.String("cloud.provider", "azure"),
	)
	time.Sleep(15 * time.Millisecond) // Simulated external latency
	apiSpan.End()

	parentSpan := trace.SpanFromContext(ctx)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":      "success",
		"message":     "Distributed tracing demonstration with child spans completed",
		"trace_id":    parentSpan.SpanContext().TraceID().String(),
		"child_spans": []string{"simulate_database_query", "simulate_azure_service_call"},
	})
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initialize OpenTelemetry Provider (Azure Monitor / OTLP / Stdout)
	otelConfig := telemetry.LoadConfigFromEnv()
	_, shutdownTelemetry, err := telemetry.InitTelemetry(ctx, otelConfig)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize OpenTelemetry: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			log.Printf("[ERROR] Error during OpenTelemetry shutdown: %v", err)
		}
	}()

	// 2. Build Fiber App with Injected Dependencies
	app := SetupApp(AppDependencies{
		TelemetryConfig: otelConfig,
	})

	// 3. Graceful Server Shutdown Handler
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-shutdownChan
		log.Println("[Server] Received termination signal. Gracefully shutting down HTTP server...")
		if err := app.ShutdownWithTimeout(5 * time.Second); err != nil {
			log.Printf("[Server] Error during HTTP server shutdown: %v", err)
		}
	}()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("[Server] Server started on http://0.0.0.0:%s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Printf("[Server] HTTP listener closed: %v", err)
	}
}
