package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

// SetupApp initializes and configures the Fiber application HTTP routes and middleware.
func SetupApp() *fiber.App {
	app := fiber.New(fiber.Config{
		AppName: "Hello World Fiber App v1.0",
	})

	// Root endpoint GET /
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status":  "success",
			"message": "Hello World from Go and Fiber!",
		})
	})

	// Health check endpoint GET /health
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status": "healthy",
		})
	})

	return app
}

func main() {
	app := SetupApp()

	log.Println("Server started on http://localhost:8080")
	log.Fatal(app.Listen(":8080"))
}
