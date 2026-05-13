// main.go - Fiber application entry point
package main

import (
	"crimpy/backend/internal/database"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/internal/handler/sync"
	applogger "crimpy/backend/internal/logger"
	"crimpy/backend/internal/middleware"
	"crimpy/backend/internal/utils"
	"log"
	"log/slog"
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
	env := os.Getenv("ENV")
	logCloser := applogger.Init(env)
	defer logCloser.Close()

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
	syncHandler := sync.NewSyncHandler(queries)
	coachHandler := handler.NewCoachHandler(queries, pool)
	exerciseHandler := handler.NewExerciseHandler(queries, pool)
	tagHandler := handler.NewTagHandler(queries, pool)
	coachTrainingHandler := handler.NewCoachTrainingHandler(queries, pool)
	programHandler := handler.NewProgramHandler(queries, pool)

	// Create Fiber app
	app := fiber.New()

	// Register global middleware
	app.Use(logger.New(logger.Config{
		Stream: applogger.Output,
		Format: "${time} | ${status} | ${latency} | ${ip} | ${method} ${path}\n",
	}))
	app.Use(recover.New())
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		if status := c.Response().StatusCode(); status >= 400 {
			slog.Warn("request failed",
				"status", status,
				"method", c.Method(),
				"path", c.Path(),
				"ip", c.IP(),
				"body", string(c.Response().Body()),
			)
		}
		return err
	})

	// CORS middleware - allow frontend to connect
	app.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "https://crimpy.app", "https://*.crimpy.app"},
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
	app.Post("/auth/verify", authHandler.VerifyEmail)
	app.Post("/auth/resend-verification", authHandler.ResendVerificationEmail)

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

	// Enrollment routes
	api.Post("/coach/enrollment-token", coachHandler.GenerateEnrollmentToken)
	api.Get("/enrollment/:token", coachHandler.GetEnrollmentTokenInfo)
	api.Post("/enrollment/:token/accept", coachHandler.AcceptEnrollment)
	api.Get("/coach/enrollments", coachHandler.GetCoachEnrollments)
	api.Get("/user/enrollment", coachHandler.GetUserEnrollment)
	api.Delete("/coach/enrollments/:user_id", coachHandler.UnenrollUser)
	api.Delete("/user/enrollment", coachHandler.LeaveCoach)

	// Coach training template routes
	api.Post("/coach/trainings", coachTrainingHandler.CreateCoachTraining)
	api.Get("/coach/trainings", coachTrainingHandler.GetCoachTrainings)
	api.Get("/coach/trainings/:id", coachTrainingHandler.GetCoachTraining)
	api.Put("/coach/trainings/:id", coachTrainingHandler.UpdateCoachTraining)
	api.Delete("/coach/trainings/:id", coachTrainingHandler.DeleteCoachTraining)

	// Exercise library routes (coach only)
	api.Post("/coach/exercises", exerciseHandler.CreateExercise)
	api.Get("/coach/exercises", exerciseHandler.GetExercises)
	api.Get("/coach/exercises/favorites", exerciseHandler.GetFavoriteExercises)
	api.Get("/coach/exercises/:id", exerciseHandler.GetExercise)
	api.Put("/coach/exercises/:id", exerciseHandler.UpdateExercise)
	api.Delete("/coach/exercises/:id", exerciseHandler.DeleteExercise)
	api.Put("/coach/exercises/:id/favorite", exerciseHandler.SetExerciseFavorite)
	api.Post("/coach/exercises/:exercise_id/tags/:tag_id", tagHandler.AssignTag)
	api.Delete("/coach/exercises/:exercise_id/tags/:tag_id", tagHandler.UnassignTag)

	// Tag library routes (coach only)
	api.Post("/coach/tags", tagHandler.CreateTag)
	api.Get("/coach/tags", tagHandler.GetTags)
	api.Put("/coach/tags/:id", tagHandler.UpdateTag)
	api.Delete("/coach/tags/:id", tagHandler.DeleteTag)

	// Coaching panel routes
	api.Get("/coach/clients/:user_id/sessions", coachHandler.GetClientSessions)
	api.Get("/coach/clients/:user_id/sessions/:session_id", coachHandler.GetClientSession)
	api.Get("/coach/clients/:user_id/assessments", coachHandler.GetClientAssessments)

	// Program routes (coach manages, coachee reads)
	api.Post("/coach/clients/:user_id/programs", programHandler.CreateProgram)
	api.Get("/coach/clients/:user_id/programs", programHandler.GetPrograms)
	api.Get("/coach/clients/:user_id/programs/:program_id", programHandler.GetProgram)
	api.Put("/coach/clients/:user_id/programs/:program_id", programHandler.UpdateProgram)
	api.Delete("/coach/clients/:user_id/programs/:program_id", programHandler.DeleteProgram)
	api.Get("/user/programs", programHandler.GetMyPrograms)
	api.Get("/user/programs/:program_id", programHandler.GetMyProgram)

	// Admin routes
	api.Get("/admin/coaches/pending", adminHandler.GetPendingCoaches)
	api.Put("/admin/coaches/:id/validate", adminHandler.ValidateCoach)
	api.Put("/admin/coaches/:id/reject", adminHandler.RejectCoach)
	api.Get("/admin/users", adminHandler.ListUsers)
	api.Delete("/admin/users/:id", adminHandler.DeleteUser)

	// Sync routes
	api.Get("/sync/summary", syncHandler.GetSummary)
	api.Get("/sync/pull", syncHandler.Pull)
	api.Post("/sync/push", syncHandler.Push)
	api.Post("/sync/migrate", syncHandler.Migrate)

	// Read port from environment or default to 3000
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Fatal(app.Listen(":"+port), fiber.ListenConfig{DisableStartupMessage: os.Getenv("ENV") == "production"})
}
