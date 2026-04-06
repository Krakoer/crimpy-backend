package testutil

import (
	"context"
	"crimpy/backend/internal/database"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"crimpy/backend/internal/utils"
	"fmt"
	"os"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSetup contains all resources needed for testing
type TestSetup struct {
	App       *fiber.App
	Queries   *db.Queries
	Pool      *pgxpool.Pool
	AuthToken string
	UserID    string
}

// SetupTestDB initializes a test database connection
func SetupTestDB(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()

	// Use test database environment variables or defaults
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set, skipping integration tests")
	}

	pool, err := database.NewConnection()
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	queries := db.New(pool)
	return pool, queries
}

// CleanupTestDB removes test data and closes the connection
func CleanupTestDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	// Clean up test data in reverse order of foreign key dependencies
	ctx := context.Background()

	// Delete assessments
	_, err := pool.Exec(ctx, "DELETE FROM assessments WHERE session_id IN (SELECT id FROM sessions WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%test%'))")
	if err != nil {
		t.Logf("Warning: Failed to clean up assessments: %v", err)
	}

	// Delete rep_datas
	_, err = pool.Exec(ctx, "DELETE FROM rep_datas WHERE session_id IN (SELECT id FROM sessions WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%test%'))")
	if err != nil {
		t.Logf("Warning: Failed to clean up rep_datas: %v", err)
	}

	// Delete sessions
	_, err = pool.Exec(ctx, "DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%test%')")
	if err != nil {
		t.Logf("Warning: Failed to clean up sessions: %v", err)
	}

	// Delete rep_templates
	_, err = pool.Exec(ctx, "DELETE FROM rep_templates WHERE training_id IN (SELECT id FROM trainings WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%test%'))")
	if err != nil {
		t.Logf("Warning: Failed to clean up rep_templates: %v", err)
	}

	// Delete trainings
	_, err = pool.Exec(ctx, "DELETE FROM trainings WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%test%')")
	if err != nil {
		t.Logf("Warning: Failed to clean up trainings: %v", err)
	}

	// Delete repeaters (no user reference)
	_, err = pool.Exec(ctx, "DELETE FROM repeaters WHERE id > 0")
	if err != nil {
		t.Logf("Warning: Failed to clean up repeaters: %v", err)
	}

	// Delete test users
	_, err = pool.Exec(ctx, "DELETE FROM users WHERE email LIKE '%test%'")
	if err != nil {
		t.Logf("Warning: Failed to clean up users: %v", err)
	}

	pool.Close()
}

// SetupFiberApp creates a fiber app with routes for testing (used by handler tests)
type HandlerConfig struct {
	AuthHandler     interface {
		Register(fiber.Ctx) error
		Login(fiber.Ctx) error
	}
	TrainingHandler interface {
		CreateTraining(fiber.Ctx) error
		GetTrainings(fiber.Ctx) error
		GetTraining(fiber.Ctx) error
		UpdateTraining(fiber.Ctx) error
		DeleteTraining(fiber.Ctx) error
	}
	SessionHandler interface {
		CreateSession(fiber.Ctx) error
		GetSessions(fiber.Ctx) error
		GetSession(fiber.Ctx) error
		UpdateSession(fiber.Ctx) error
		DeleteSession(fiber.Ctx) error
	}
	RepeaterHandler interface {
		CreateRepeater(fiber.Ctx) error
		GetRepeater(fiber.Ctx) error
		UpdateRepeater(fiber.Ctx) error
		DeleteRepeater(fiber.Ctx) error
	}
}

func SetupFiberApp(config HandlerConfig) *fiber.App {
	app := fiber.New()

	// Public routes
	if config.AuthHandler != nil {
		app.Post("/auth/register", config.AuthHandler.Register)
		app.Post("/auth/login", config.AuthHandler.Login)
	}

	// Protected routes
	api := app.Group("/api", middleware.AuthMiddleware())

	if config.TrainingHandler != nil {
		api.Post("/trainings", config.TrainingHandler.CreateTraining)
		api.Get("/trainings", config.TrainingHandler.GetTrainings)
		api.Get("/trainings/:id", config.TrainingHandler.GetTraining)
		api.Put("/trainings/:id", config.TrainingHandler.UpdateTraining)
		api.Delete("/trainings/:id", config.TrainingHandler.DeleteTraining)
	}

	if config.SessionHandler != nil {
		api.Post("/sessions", config.SessionHandler.CreateSession)
		api.Get("/sessions", config.SessionHandler.GetSessions)
		api.Get("/sessions/:id", config.SessionHandler.GetSession)
		api.Put("/sessions/:id", config.SessionHandler.UpdateSession)
		api.Delete("/sessions/:id", config.SessionHandler.DeleteSession)
	}

	if config.RepeaterHandler != nil {
		api.Post("/repeaters", config.RepeaterHandler.CreateRepeater)
		api.Get("/repeaters/:id", config.RepeaterHandler.GetRepeater)
		api.Put("/repeaters/:id", config.RepeaterHandler.UpdateRepeater)
		api.Delete("/repeaters/:id", config.RepeaterHandler.DeleteRepeater)
	}

	return app
}

// CreateTestUser creates a test user and returns a JWT token
func CreateTestUser(t *testing.T, queries *db.Queries, email string) (string, string) {
	t.Helper()

	password := "password123"
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:     email,
		Password:  hashedPassword,
		Firstname: "Test",
		Lastname:  "User",
	})
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	token, err := utils.GenerateJWT(user.ID.String(), user.Email, user.IsAdmin)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	return user.ID.String(), token
}

// GetAuthHeader returns a Bearer token header
func GetAuthHeader(token string) string {
	return fmt.Sprintf("Bearer %s", token)
}
