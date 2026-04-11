// main.go - Fiber application entry point
package main

import (
	"crimpy/backend/internal/database"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/internal/middleware"
	"crimpy/backend/internal/utils"
	"log"
	"os"

	_ "crimpy/backend/docs"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// @title Crimpy API
// @version 1.0
// @description Backend API for Crimpy climbing training application
// @termsOfService https://api.portfolio-online.ovh/terms

// @contact.name API Support
// @contact.email support@crimpy.com

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host api.portfolio-online.ovh
// @BasePath /
// @schemes https

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

func main() {
	// Initialize database connection
	pool, err := database.NewConnection()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Create sqlc queries instance
	queries := db.New(pool)

	// Initialize admin account
	if err := utils.InitializeAdminAccount(queries); err != nil {
		log.Printf("Warning: Failed to initialize admin account: %v", err)
	}

	// Initialize handlers
	authHandler := handler.NewAuthHandler(queries)
	adminHandler := handler.NewAdminHandler(queries)
	trainingHandler := handler.NewTrainingHandler(queries)
	sessionHandler := handler.NewSessionHandler(queries)
	repeaterHandler := handler.NewRepeaterHandler(queries)

	// Create Fiber app
	app := fiber.New()

	// Register global middleware
	app.Use(logger.New())
	app.Use(recover.New())

	// CORS middleware - allow frontend to connect
	app.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: true,
	}))

	// Public routes
	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Crimpy Backend API"})
	})

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Swagger documentation
	app.Get("/swagger/*", adaptor.HTTPHandler(httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	)))

	// Auth routes (public)
	app.Post("/auth/register", authHandler.Register)
	app.Post("/auth/login", authHandler.Login)

	// Protected routes - require authentication
	api := app.Group("/api", middleware.AuthMiddleware())

	// Auth protected routes
	api.Get("/user", authHandler.GetCurrentUser)
	api.Put("/auth/change-password", authHandler.ChangePassword)

	// Training routes
	api.Post("/trainings", trainingHandler.CreateTraining)
	api.Get("/trainings", trainingHandler.GetTrainings)
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

	// Admin routes
	api.Get("/admin/coaches/pending", adminHandler.GetPendingCoaches)
	api.Put("/admin/coaches/:id/validate", adminHandler.ValidateCoach)
	api.Put("/admin/coaches/:id/reject", adminHandler.RejectCoach)

	// Read port from environment or default to 3000
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Fatal(app.Listen(":"+port), fiber.ListenConfig{DisableStartupMessage: os.Getenv("ENV") == "production"})
}
