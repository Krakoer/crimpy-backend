// main.go - Fiber application entry point
package main

import (
	"crimpy/backend/internal/database"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"log"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

func main() {
	// Initialize database connection
	pool, err := database.NewConnection()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Create sqlc queries instance
	queries := db.New(pool)

	// Initialize handlers
	authHandler := handler.NewAuthHandler(queries)

	// Create Fiber app
	app := fiber.New()

	// Register middleware
	app.Use(logger.New())
	app.Use(recover.New())

	// Routes
	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Crimpy Backend API"})
	})

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Auth routes
	app.Post("/auth/register", authHandler.Register)
	app.Post("/auth/login", authHandler.Login)

	// Read port from environment or default to 3000
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Fatal(app.Listen(":"+port), fiber.ListenConfig{DisableStartupMessage: os.Getenv("ENV") == "production"})
}
