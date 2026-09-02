// main.go - Fiber application entry point
package main

import (
	"context"
	"crimpy/backend/internal/database"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	applogger "crimpy/backend/internal/logger"
	"crimpy/backend/internal/middleware"
	"crimpy/backend/internal/utils"
	"log"
	"log/slog"
	"os"
	"time"

	_ "crimpy/backend/docs"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// @title Crimpy API
// @version 1.0
// @description Backend API for Crimpy climbing training application
// @termsOfService https://api.crimpy.app/terms

// @contact.name API Support
// @contact.email support@crimpy.com

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host api.crimpy.app
// @BasePath /
// @schemes https

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

// healthCheckTimeout bounds the database ping behind /health so an unreachable
// database answers fast instead of stalling the probe.
const healthCheckTimeout = 2 * time.Second

// pruneExpiredRefreshTokens deletes expired refresh tokens daily. Rotation issues
// a new row on every refresh, so without this the table grows without bound.
func pruneExpiredRefreshTokens(queries *db.Queries) {
	for {
		if err := queries.DeleteExpiredRefreshTokens(context.Background()); err != nil {
			slog.Error("failed to prune expired refresh tokens", "error", err)
		}
		time.Sleep(24 * time.Hour)
	}
}

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

	go pruneExpiredRefreshTokens(queries)

	// Initialize handlers
	authHandler := handler.NewAuthHandler(queries)
	adminHandler := handler.NewAdminHandler(queries)
	sessionHandler := handler.NewSessionHandler(queries, pool)
	assessmentHandler := handler.NewAssessmentHandler(queries)
	assessmentDefinitionHandler := handler.NewAssessmentDefinitionHandler(queries, pool)
	sensorConfigHandler := handler.NewSensorConfigHandler(queries)
	builtinWeightHandler := handler.NewBuiltinTrainingWeightHandler(queries)
	pinnedHandler := handler.NewPinnedBuiltinTrainingHandler(queries)
	coachHandler := handler.NewCoachHandler(queries, pool)
	exerciseHandler := handler.NewExerciseHandler(queries, pool)
	tagHandler := handler.NewTagHandler(queries, pool)
	trainingHandler := handler.NewTrainingHandler(queries, pool)
	programHandler := handler.NewProgramHandler(queries, pool)
	availabilityHandler := handler.NewAvailabilityHandler(queries, pool)

	// Create Fiber app
	app := fiber.New()

	// Register global middleware
	app.Use(logger.New(logger.Config{
		Stream: applogger.Output,
		Format: "${time} | ${status} | ${latency} | ${ip} | ${method} ${path}\n",
	}))
	app.Use(recover.New())
	app.Use(middleware.RequestContext())
	// Response bodies are deliberately not logged: handlers already log the
	// cause of a failure, and bodies can carry user data.
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		if status := c.Response().StatusCode(); status >= 400 {
			slog.Warn("request failed",
				"status", status,
				"method", c.Method(),
				"path", c.Path(),
				"ip", c.IP(),
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

	// Readiness probe used by container healthchecks and uptime monitoring.
	// Reports unavailable when the database pool cannot serve queries, so a
	// rolling update never routes traffic to an instance that cannot work.
	app.Get("/health", func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), healthCheckTimeout)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			slog.Warn("health check failed", "error", err)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Swagger documentation
	app.Get("/swagger/*", adaptor.HTTPHandler(httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	)))

	// Auth routes (public). Rate limited per IP to slow credential and token guessing.
	authLimiter := limiter.New(limiter.Config{
		Max:        20,
		Expiration: time.Minute,
		Next:       func(c fiber.Ctx) bool { return env == "test" },
	})
	app.Post("/auth/register", authLimiter, authHandler.Register)
	app.Post("/auth/login", authLimiter, authHandler.Login)
	app.Post("/auth/refresh", authLimiter, authHandler.Refresh)
	app.Post("/auth/logout", authHandler.Logout)
	app.Post("/auth/verify", authLimiter, authHandler.VerifyEmail)
	app.Post("/auth/resend-verification", authLimiter, authHandler.ResendVerificationEmail)

	// Protected routes - require authentication
	api := app.Group("/api", middleware.AuthMiddleware())

	// Auth protected routes
	api.Get("/user", authHandler.GetCurrentUser)
	api.Put("/auth/change-password", authHandler.ChangePassword)

	// Assessment routes
	api.Post("/assessments", assessmentHandler.CreateAssessment)
	api.Get("/assessments", assessmentHandler.GetAssessments)
	api.Delete("/assessments/:id", assessmentHandler.DeleteAssessment)
	api.Get("/assessment-definitions", assessmentDefinitionHandler.GetAssessmentDefinitions)
	api.Post("/assessment-definitions", assessmentDefinitionHandler.CreateAssessmentDefinition)
	api.Put("/assessment-definitions/:id", assessmentDefinitionHandler.UpdateAssessmentDefinition)
	api.Delete("/assessment-definitions/:id", assessmentDefinitionHandler.DeleteAssessmentDefinition)

	// Session routes
	api.Post("/sessions", sessionHandler.CreateSession)
	api.Get("/sessions", sessionHandler.GetSessions)
	api.Get("/sessions/:id", sessionHandler.GetSession)
	api.Put("/sessions/:id", sessionHandler.UpdateSession)
	api.Delete("/sessions/:id", sessionHandler.DeleteSession)
	api.Put("/sessions/:id/coach-reply/read", sessionHandler.MarkCoachReplyRead)

	// Enrollment routes
	api.Post("/coach/enrollment-token", coachHandler.GenerateEnrollmentToken)
	api.Get("/enrollment/:token", coachHandler.GetEnrollmentTokenInfo)
	api.Post("/enrollment/:token/accept", coachHandler.AcceptEnrollment)
	api.Get("/coach/enrollments", coachHandler.GetCoachEnrollments)
	api.Get("/user/enrollment", coachHandler.GetUserEnrollment)
	api.Delete("/coach/enrollments/:user_id", coachHandler.UnenrollUser)
	api.Delete("/user/enrollment", coachHandler.LeaveCoach)

	// Training routes (unified for all users)
	api.Post("/trainings", trainingHandler.CreateTraining)
	api.Get("/trainings", trainingHandler.GetTrainings)
	api.Get("/trainings/:id", trainingHandler.GetTraining)
	api.Put("/trainings/:id", trainingHandler.UpdateTraining)
	api.Delete("/trainings/:id", trainingHandler.DeleteTraining)

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
	api.Put("/coach/clients/:user_id/sessions/:session_id/reply", coachHandler.SetClientSessionReply)
	api.Get("/coach/clients/:user_id/assessments", coachHandler.GetClientAssessments)

	// Program routes (coach manages, coachee reads)
	api.Post("/coach/clients/:user_id/programs", programHandler.CreateProgram)
	api.Get("/coach/clients/:user_id/programs", programHandler.GetPrograms)
	api.Get("/coach/clients/:user_id/programs/:program_id", programHandler.GetProgram)
	api.Put("/coach/clients/:user_id/programs/:program_id", programHandler.UpdateProgram)
	api.Delete("/coach/clients/:user_id/programs/:program_id", programHandler.DeleteProgram)
	api.Get("/user/programs", programHandler.GetMyPrograms)
	api.Get("/user/programs/:program_id", programHandler.GetMyProgram)
	api.Put("/coach/clients/:user_id/programs/:program_id/weeks/:week_number", programHandler.UpsertWeek)
	api.Get("/coach/clients/:user_id/programs/:program_id/weeks", programHandler.GetWeeks)
	api.Get("/coach/clients/:user_id/programs/:program_id/weeks/:week_number", programHandler.GetWeek)
	api.Delete("/coach/clients/:user_id/programs/:program_id/weeks/:week_number", programHandler.DeleteWeek)
	api.Get("/user/programs/:program_id/weeks", programHandler.GetMyWeeks)
	api.Get("/user/programs/:program_id/weeks/:week_number", programHandler.GetMyWeek)
	api.Get("/user/programs/:program_id/trainings/:training_id", programHandler.GetMyProgramTraining)

	// Availability routes (coachee declares, coach reads)
	api.Get("/user/availability", availabilityHandler.GetMyAvailability)
	api.Put("/user/availability/:week_start", availabilityHandler.UpsertMyWeekAvailability)
	api.Get("/user/availability-reminder", availabilityHandler.GetMyAvailabilityReminder)
	api.Get("/coach/clients/:user_id/availability", availabilityHandler.GetClientAvailability)
	api.Get("/coach/availability-reminder", availabilityHandler.GetAvailabilityReminder)
	api.Put("/coach/availability-reminder", availabilityHandler.SetAvailabilityReminder)

	// Sensor config routes
	api.Post("/sensor-configs", sensorConfigHandler.CreateSensorConfig)
	api.Get("/sensor-configs", sensorConfigHandler.GetSensorConfigs)
	api.Put("/sensor-configs/:id", sensorConfigHandler.UpdateSensorConfig)
	api.Delete("/sensor-configs/:id", sensorConfigHandler.DeleteSensorConfig)

	// Builtin training weight routes
	api.Post("/builtin-training-weights", builtinWeightHandler.CreateBuiltinTrainingWeight)
	api.Get("/builtin-training-weights", builtinWeightHandler.GetBuiltinTrainingWeights)
	api.Put("/builtin-training-weights/:id", builtinWeightHandler.UpdateBuiltinTrainingWeight)
	api.Delete("/builtin-training-weights/:id", builtinWeightHandler.DeleteBuiltinTrainingWeight)

	// Pinned builtin training routes
	api.Post("/pinned-builtin-trainings", pinnedHandler.PinBuiltinTraining)
	api.Get("/pinned-builtin-trainings", pinnedHandler.GetPinnedBuiltinTrainings)
	api.Delete("/pinned-builtin-trainings/:builtin_training_id", pinnedHandler.UnpinBuiltinTraining)

	// Admin routes
	api.Get("/admin/coaches/pending", adminHandler.GetPendingCoaches)
	api.Put("/admin/coaches/:id/validate", adminHandler.ValidateCoach)
	api.Put("/admin/coaches/:id/reject", adminHandler.RejectCoach)
	api.Get("/admin/users", adminHandler.ListUsers)
	api.Delete("/admin/users/:id", adminHandler.DeleteUser)

	// Read port from environment or default to 3000
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Fatal(app.Listen(":"+port, fiber.ListenConfig{DisableStartupMessage: env == "production"}))
}
