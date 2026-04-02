// main.go - Fiber application entry point
package main

import (
	"crimpy/backend/internal/database"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/internal/middleware"
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
	trainingHandler := handler.NewTrainingHandler(queries)
	sessionHandler := handler.NewSessionHandler(queries)
	repeaterHandler := handler.NewRepeaterHandler(queries)

	// Create Fiber app
	app := fiber.New()

	// Register global middleware
	app.Use(logger.New())
	app.Use(recover.New())

	// Public routes
	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Crimpy Backend API"})
	})

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Auth routes (public)
	app.Post("/auth/register", authHandler.Register)
	app.Post("/auth/login", authHandler.Login)

	// Protected routes - require authentication
	api := app.Group("/api", middleware.AuthMiddleware())

	// Training routes
	api.Post("/trainings", trainingHandler.CreateTraining)
	api.Get("/trainings", trainingHandler.GetTrainings)
	api.Get("/trainings/favorites", trainingHandler.GetFavoriteTrainings)
	api.Get("/trainings/:id", trainingHandler.GetTraining)
	api.Put("/trainings/:id", trainingHandler.UpdateTraining)
	api.Delete("/trainings/:id", trainingHandler.DeleteTraining)

	// Session routes
	api.Post("/sessions", sessionHandler.CreateSession)
	api.Get("/sessions", sessionHandler.GetSessions)
	api.Get("/sessions/:id", sessionHandler.GetSession)
	api.Put("/sessions/:id", sessionHandler.UpdateSession)
	api.Delete("/sessions/:id", sessionHandler.DeleteSession)

	// Repeater routes
	api.Post("/repeaters", repeaterHandler.CreateRepeater)
	api.Get("/repeaters/:id", repeaterHandler.GetRepeater)
	api.Put("/repeaters/:id", repeaterHandler.UpdateRepeater)
	api.Delete("/repeaters/:id", repeaterHandler.DeleteRepeater)

	// Read port from environment or default to 3000
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Fatal(app.Listen(":"+port), fiber.ListenConfig{DisableStartupMessage: os.Getenv("ENV") == "production"})
}
