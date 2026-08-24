package testutil

import (
	"bytes"
	"context"
	"crimpy/backend/internal/database"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"crimpy/backend/internal/utils"
	"fmt"
	"io"
	"net/http"
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

	t.Setenv("ENV", "test")

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

	// Delete programs (weeks and sessions cascade). Runs before trainings, since
	// a week session holds a training down with ON DELETE RESTRICT.
	_, err = pool.Exec(ctx, "DELETE FROM coach_programs WHERE coach_id IN (SELECT id FROM users WHERE email LIKE '%test%')")
	if err != nil {
		t.Logf("Warning: Failed to clean up coach_programs: %v", err)
	}

	// Delete trainings (items cascade)
	_, err = pool.Exec(ctx, "DELETE FROM trainings WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%test%')")
	if err != nil {
		t.Logf("Warning: Failed to clean up trainings: %v", err)
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
	AuthHandler interface {
		Register(fiber.Ctx) error
		Login(fiber.Ctx) error
		Refresh(fiber.Ctx) error
		Logout(fiber.Ctx) error
		ChangePassword(fiber.Ctx) error
		GetCurrentUser(fiber.Ctx) error
	}
	AdminHandler interface {
		GetPendingCoaches(fiber.Ctx) error
		ValidateCoach(fiber.Ctx) error
		RejectCoach(fiber.Ctx) error
	}
	SessionHandler interface {
		CreateSession(fiber.Ctx) error
		GetSessions(fiber.Ctx) error
		GetSession(fiber.Ctx) error
		UpdateSession(fiber.Ctx) error
		DeleteSession(fiber.Ctx) error
	}
	ExerciseHandler interface {
		CreateExercise(fiber.Ctx) error
		GetExercises(fiber.Ctx) error
		GetExercise(fiber.Ctx) error
		UpdateExercise(fiber.Ctx) error
		DeleteExercise(fiber.Ctx) error
		SetExerciseFavorite(fiber.Ctx) error
		GetFavoriteExercises(fiber.Ctx) error
	}
	TagHandler interface {
		CreateTag(fiber.Ctx) error
		GetTags(fiber.Ctx) error
		UpdateTag(fiber.Ctx) error
		DeleteTag(fiber.Ctx) error
		AssignTag(fiber.Ctx) error
		UnassignTag(fiber.Ctx) error
	}
	TrainingHandler interface {
		CreateTraining(fiber.Ctx) error
		GetTrainings(fiber.Ctx) error
		GetTraining(fiber.Ctx) error
		UpdateTraining(fiber.Ctx) error
		DeleteTraining(fiber.Ctx) error
	}
	CoachHandler interface {
		GetCoachEnrollments(fiber.Ctx) error
		GetClientSessions(fiber.Ctx) error
		GetClientSession(fiber.Ctx) error
		GetClientAssessments(fiber.Ctx) error
	}
	ProgramHandler interface {
		CreateProgram(fiber.Ctx) error
		GetPrograms(fiber.Ctx) error
		GetProgram(fiber.Ctx) error
		UpdateProgram(fiber.Ctx) error
		DeleteProgram(fiber.Ctx) error
		GetMyPrograms(fiber.Ctx) error
		GetMyProgram(fiber.Ctx) error
		UpsertWeek(fiber.Ctx) error
		GetWeeks(fiber.Ctx) error
		GetWeek(fiber.Ctx) error
		DeleteWeek(fiber.Ctx) error
		GetMyWeeks(fiber.Ctx) error
		GetMyWeek(fiber.Ctx) error
		GetMyProgramTraining(fiber.Ctx) error
	}
	BuiltinTrainingWeightHandler interface {
		CreateBuiltinTrainingWeight(fiber.Ctx) error
		GetBuiltinTrainingWeights(fiber.Ctx) error
		UpdateBuiltinTrainingWeight(fiber.Ctx) error
		DeleteBuiltinTrainingWeight(fiber.Ctx) error
	}
}

func SetupFiberApp(config HandlerConfig) *fiber.App {
	app := fiber.New()

	// Mirror the production middleware stack so handlers get the same
	// request-scoped context they get when served by cmd/api.
	app.Use(middleware.RequestContext())

	// Public routes
	if config.AuthHandler != nil {
		app.Post("/auth/register", config.AuthHandler.Register)
		app.Post("/auth/login", config.AuthHandler.Login)
		app.Post("/auth/refresh", config.AuthHandler.Refresh)
		app.Post("/auth/logout", config.AuthHandler.Logout)
	}

	// Protected routes
	api := app.Group("/api", middleware.AuthMiddleware())

	if config.AuthHandler != nil {
		api.Get("/user", config.AuthHandler.GetCurrentUser)
		api.Put("/auth/change-password", config.AuthHandler.ChangePassword)
	}

	if config.SessionHandler != nil {
		api.Post("/sessions", config.SessionHandler.CreateSession)
		api.Get("/sessions", config.SessionHandler.GetSessions)
		api.Get("/sessions/:id", config.SessionHandler.GetSession)
		api.Put("/sessions/:id", config.SessionHandler.UpdateSession)
		api.Delete("/sessions/:id", config.SessionHandler.DeleteSession)
	}

	if config.AdminHandler != nil {
		api.Get("/admin/coaches/pending", config.AdminHandler.GetPendingCoaches)
		api.Put("/admin/coaches/:id/validate", config.AdminHandler.ValidateCoach)
		api.Put("/admin/coaches/:id/reject", config.AdminHandler.RejectCoach)
	}

	if config.CoachHandler != nil {
		api.Get("/coach/enrollments", config.CoachHandler.GetCoachEnrollments)
		api.Get("/coach/clients/:user_id/sessions", config.CoachHandler.GetClientSessions)
		api.Get("/coach/clients/:user_id/sessions/:session_id", config.CoachHandler.GetClientSession)
		api.Get("/coach/clients/:user_id/assessments", config.CoachHandler.GetClientAssessments)
	}

	if config.ExerciseHandler != nil {
		api.Post("/coach/exercises", config.ExerciseHandler.CreateExercise)
		api.Get("/coach/exercises", config.ExerciseHandler.GetExercises)
		api.Get("/coach/exercises/favorites", config.ExerciseHandler.GetFavoriteExercises)
		api.Get("/coach/exercises/:id", config.ExerciseHandler.GetExercise)
		api.Put("/coach/exercises/:id", config.ExerciseHandler.UpdateExercise)
		api.Delete("/coach/exercises/:id", config.ExerciseHandler.DeleteExercise)
		api.Put("/coach/exercises/:id/favorite", config.ExerciseHandler.SetExerciseFavorite)
	}

	if config.TagHandler != nil {
		api.Post("/coach/tags", config.TagHandler.CreateTag)
		api.Get("/coach/tags", config.TagHandler.GetTags)
		api.Put("/coach/tags/:id", config.TagHandler.UpdateTag)
		api.Delete("/coach/tags/:id", config.TagHandler.DeleteTag)
		api.Post("/coach/exercises/:exercise_id/tags/:tag_id", config.TagHandler.AssignTag)
		api.Delete("/coach/exercises/:exercise_id/tags/:tag_id", config.TagHandler.UnassignTag)
	}

	if config.TrainingHandler != nil {
		api.Post("/trainings", config.TrainingHandler.CreateTraining)
		api.Get("/trainings", config.TrainingHandler.GetTrainings)
		api.Get("/trainings/:id", config.TrainingHandler.GetTraining)
		api.Put("/trainings/:id", config.TrainingHandler.UpdateTraining)
		api.Delete("/trainings/:id", config.TrainingHandler.DeleteTraining)
	}

	if config.ProgramHandler != nil {
		api.Post("/coach/clients/:user_id/programs", config.ProgramHandler.CreateProgram)
		api.Get("/coach/clients/:user_id/programs", config.ProgramHandler.GetPrograms)
		api.Get("/coach/clients/:user_id/programs/:program_id", config.ProgramHandler.GetProgram)
		api.Put("/coach/clients/:user_id/programs/:program_id", config.ProgramHandler.UpdateProgram)
		api.Delete("/coach/clients/:user_id/programs/:program_id", config.ProgramHandler.DeleteProgram)
		api.Get("/user/programs", config.ProgramHandler.GetMyPrograms)
		api.Get("/user/programs/:program_id", config.ProgramHandler.GetMyProgram)
		api.Put("/coach/clients/:user_id/programs/:program_id/weeks/:week_number", config.ProgramHandler.UpsertWeek)
		api.Get("/coach/clients/:user_id/programs/:program_id/weeks", config.ProgramHandler.GetWeeks)
		api.Get("/coach/clients/:user_id/programs/:program_id/weeks/:week_number", config.ProgramHandler.GetWeek)
		api.Delete("/coach/clients/:user_id/programs/:program_id/weeks/:week_number", config.ProgramHandler.DeleteWeek)
		api.Get("/user/programs/:program_id/weeks", config.ProgramHandler.GetMyWeeks)
		api.Get("/user/programs/:program_id/weeks/:week_number", config.ProgramHandler.GetMyWeek)
		api.Get("/user/programs/:program_id/trainings/:training_id", config.ProgramHandler.GetMyProgramTraining)
	}

	if config.BuiltinTrainingWeightHandler != nil {
		api.Post("/builtin-training-weights", config.BuiltinTrainingWeightHandler.CreateBuiltinTrainingWeight)
		api.Get("/builtin-training-weights", config.BuiltinTrainingWeightHandler.GetBuiltinTrainingWeights)
		api.Put("/builtin-training-weights/:id", config.BuiltinTrainingWeightHandler.UpdateBuiltinTrainingWeight)
		api.Delete("/builtin-training-weights/:id", config.BuiltinTrainingWeightHandler.DeleteBuiltinTrainingWeight)
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

	// Verify email for test users
	err = queries.VerifyUserEmail(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Failed to verify test user email: %v", err)
	}

	token, err := utils.GenerateJWT(user.ID.String(), user.Email, user.IsAdmin, user.IsCoach)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	return user.ID.String(), token
}

// CreateTestAdminUser creates a test admin user and returns a JWT token
func CreateTestAdminUser(t *testing.T, queries *db.Queries, email string) (string, string) {
	t.Helper()

	password := "password123"
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user, err := queries.CreateAdminUser(context.Background(), db.CreateAdminUserParams{
		Email:     email,
		Password:  hashedPassword,
		Firstname: "Admin",
		Lastname:  "User",
	})
	if err != nil {
		t.Fatalf("Failed to create test admin user: %v", err)
	}

	token, err := utils.GenerateJWT(user.ID.String(), user.Email, user.IsAdmin, user.IsCoach)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	return user.ID.String(), token
}

// CreateTestCoachUser creates a test coach user (unvalidated) and returns a JWT token
func CreateTestCoachUser(t *testing.T, queries *db.Queries, email string) (string, string) {
	t.Helper()

	password := "password123"
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user, err := queries.CreateCoachUser(context.Background(), db.CreateCoachUserParams{
		Email:     email,
		Password:  hashedPassword,
		Firstname: "Coach",
		Lastname:  "User",
	})
	if err != nil {
		t.Fatalf("Failed to create test coach user: %v", err)
	}

	// Verify email for test users
	err = queries.VerifyUserEmail(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Failed to verify test coach email: %v", err)
	}

	token, err := utils.GenerateJWT(user.ID.String(), user.Email, user.IsAdmin, user.IsCoach)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	return user.ID.String(), token
}

// CreateTestValidatedCoachUser creates a validated coach user and returns the user ID and JWT token
func CreateTestValidatedCoachUser(t *testing.T, pool *pgxpool.Pool, queries *db.Queries, email string) (string, string) {
	t.Helper()

	userID, token := CreateTestCoachUser(t, queries, email)

	_, err := pool.Exec(context.Background(), "UPDATE users SET coach_validated = true WHERE id = $1", userID)
	if err != nil {
		t.Fatalf("Failed to validate test coach: %v", err)
	}

	return userID, token
}

// GetAuthHeader returns a Bearer token header
func GetAuthHeader(token string) string {
	return fmt.Sprintf("Bearer %s", token)
}

// NewRequest creates an HTTP request with proper headers for Fiber v3
func NewRequest(method, url string, body io.Reader) *http.Request {
	req, _ := http.NewRequest(method, url, body)
	req.Host = "localhost"
	return req
}

// NewRequestWithAuth creates an HTTP request with auth header and proper headers for Fiber v3
func NewRequestWithAuth(method, url string, body io.Reader, token string) *http.Request {
	req := NewRequest(method, url, body)
	req.Header.Set("Authorization", GetAuthHeader(token))
	return req
}

// NewJSONRequest creates an HTTP request with JSON content type and proper headers for Fiber v3
func NewJSONRequest(method, url string, body []byte) *http.Request {
	req := NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// NewJSONRequestWithAuth creates an HTTP request with JSON and auth headers for Fiber v3
func NewJSONRequestWithAuth(method, url string, body []byte, token string) *http.Request {
	req := NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", GetAuthHeader(token))
	return req
}
