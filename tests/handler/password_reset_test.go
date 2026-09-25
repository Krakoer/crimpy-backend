package handler_test

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/internal/utils"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupPasswordResetApp(t *testing.T) (*pgxpool.Pool, *db.Queries, *fiber.App) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})
	return pool, queries, app
}

func postPublicJSON(t *testing.T, app *fiber.App, path string, payload map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := app.Test(testutil.NewJSONRequest(http.MethodPost, path, body))
	if err != nil {
		t.Fatalf("Request to %s failed: %v", path, err)
	}
	var response map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&response)
	return resp.StatusCode, response
}

// seedResetToken stores a reset token the test knows the raw value of, since
// the one the handler generates only ever leaves in the email.
func seedResetToken(t *testing.T, pool *pgxpool.Pool, userID, rawToken string, requestedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE users SET password_reset_token_hash = $2, password_reset_requested_at = $3 WHERE id = $1::uuid`,
		userID, utils.HashToken(rawToken), requestedAt)
	if err != nil {
		t.Fatalf("Failed to seed reset token: %v", err)
	}
}

func storedResetTokenHash(t *testing.T, pool *pgxpool.Pool, email string) *string {
	t.Helper()
	var hash *string
	err := pool.QueryRow(context.Background(),
		`SELECT password_reset_token_hash FROM users WHERE email = $1`, email).Scan(&hash)
	if err != nil {
		t.Fatalf("Failed to read reset token: %v", err)
	}
	return hash
}

func loginStatus(t *testing.T, app *fiber.App, email, password string) int {
	t.Helper()
	status, _ := postPublicJSON(t, app, "/auth/login", map[string]interface{}{"email": email, "password": password})
	return status
}

func TestAuthHandler_ForgotPassword_StoresTokenForKnownEmail(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	testutil.CreateTestUser(t, queries, "forgot-known@test.com")

	status, response := postPublicJSON(t, app, "/auth/forgot-password", map[string]interface{}{"email": " Forgot-Known@test.com "})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d", status)
	}
	if response["message"] == nil || response["message"] == "" {
		t.Error("Expected a message")
	}
	if storedResetTokenHash(t, pool, "forgot-known@test.com") == nil {
		t.Error("Expected a reset token to be stored")
	}
}

func TestAuthHandler_ForgotPassword_UnknownEmailAnswersTheSame(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	testutil.CreateTestUser(t, queries, "forgot-exists@test.com")

	knownStatus, known := postPublicJSON(t, app, "/auth/forgot-password", map[string]interface{}{"email": "forgot-exists@test.com"})
	unknownStatus, unknown := postPublicJSON(t, app, "/auth/forgot-password", map[string]interface{}{"email": "forgot-nobody@test.com"})

	if knownStatus != fiber.StatusOK || unknownStatus != fiber.StatusOK {
		t.Fatalf("Expected 200 for both, got %d and %d", knownStatus, unknownStatus)
	}
	if known["message"] != unknown["message"] {
		t.Errorf("Expected the same message, got %q and %q", known["message"], unknown["message"])
	}
}

func TestAuthHandler_ForgotPassword_MissingEmail(t *testing.T) {
	pool, _, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	status, _ := postPublicJSON(t, app, "/auth/forgot-password", map[string]interface{}{"email": "  "})
	if status != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", status)
	}
}

func TestAuthHandler_ForgotPassword_CooldownKeepsTheSentToken(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	userID, _ := testutil.CreateTestUser(t, queries, "forgot-cooldown@test.com")

	postPublicJSON(t, app, "/auth/forgot-password", map[string]interface{}{"email": "forgot-cooldown@test.com"})
	first := storedResetTokenHash(t, pool, "forgot-cooldown@test.com")

	status, _ := postPublicJSON(t, app, "/auth/forgot-password", map[string]interface{}{"email": "forgot-cooldown@test.com"})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200 within the cooldown, got %d", status)
	}
	second := storedResetTokenHash(t, pool, "forgot-cooldown@test.com")
	if first == nil || second == nil || *first != *second {
		t.Error("Expected a request within the cooldown to leave the sent token in place")
	}

	seedResetToken(t, pool, userID, "cooldown-over", time.Now().Add(-2*utils.PasswordResetResendCooldown))
	postPublicJSON(t, app, "/auth/forgot-password", map[string]interface{}{"email": "forgot-cooldown@test.com"})
	third := storedResetTokenHash(t, pool, "forgot-cooldown@test.com")
	if third == nil || *third == utils.HashToken("cooldown-over") {
		t.Error("Expected a request after the cooldown to replace the token")
	}
}

func TestAuthHandler_ResetPassword_Success(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	_, refreshToken := registerAndLogin(t, app, "reset-ok@test.com", "password123")
	user, err := queries.GetUserByEmail(context.Background(), "reset-ok@test.com")
	if err != nil {
		t.Fatalf("Failed to load user: %v", err)
	}
	seedResetToken(t, pool, user.ID.String(), "reset-ok-token", time.Now())

	status, response := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{
		"token":        "reset-ok-token",
		"new_password": "newpassword456",
	})
	if status != fiber.StatusOK {
		t.Fatalf("Expected 200, got %d: %v", status, response)
	}
	if response["is_coach"] != false {
		t.Errorf("Expected is_coach false, got %v", response["is_coach"])
	}

	if s := loginStatus(t, app, "reset-ok@test.com", "newpassword456"); s != fiber.StatusOK {
		t.Errorf("Expected login with the new password to succeed, got %d", s)
	}
	if s := loginStatus(t, app, "reset-ok@test.com", "password123"); s != fiber.StatusUnauthorized {
		t.Errorf("Expected login with the old password to fail, got %d", s)
	}
	if s, _ := postRefresh(t, app, refreshToken); s != fiber.StatusUnauthorized {
		t.Errorf("Expected sessions from before the reset to be revoked, got %d", s)
	}
	if storedResetTokenHash(t, pool, "reset-ok@test.com") != nil {
		t.Error("Expected the reset token to be cleared")
	}
}

func TestAuthHandler_ResetPassword_TokenIsSingleUse(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	userID, _ := testutil.CreateTestUser(t, queries, "reset-once@test.com")
	seedResetToken(t, pool, userID, "reset-once-token", time.Now())

	postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-once-token", "new_password": "firstnew123"})
	status, _ := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-once-token", "new_password": "secondnew123"})
	if status != fiber.StatusNotFound {
		t.Errorf("Expected 404 reusing a token, got %d", status)
	}
	if s := loginStatus(t, app, "reset-once@test.com", "firstnew123"); s != fiber.StatusOK {
		t.Errorf("Expected the first reset to stand, got %d", s)
	}
}

func TestAuthHandler_ResetPassword_ExpiredToken(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	userID, _ := testutil.CreateTestUser(t, queries, "reset-expired@test.com")
	seedResetToken(t, pool, userID, "reset-expired-token", time.Now().Add(-utils.PasswordResetTokenTTL-time.Minute))

	status, _ := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-expired-token", "new_password": "newpassword456"})
	if status != fiber.StatusNotFound {
		t.Errorf("Expected 404 for an expired token, got %d", status)
	}
	if s := loginStatus(t, app, "reset-expired@test.com", "password123"); s != fiber.StatusOK {
		t.Errorf("Expected the password to be unchanged, got %d", s)
	}
}

func TestAuthHandler_ResetPassword_UnknownToken(t *testing.T) {
	pool, _, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	status, _ := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "no-such-token", "new_password": "newpassword456"})
	if status != fiber.StatusNotFound {
		t.Errorf("Expected 404, got %d", status)
	}
}

func TestAuthHandler_ResetPassword_InvalidInputKeepsToken(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	userID, _ := testutil.CreateTestUser(t, queries, "reset-invalid@test.com")
	seedResetToken(t, pool, userID, "reset-invalid-token", time.Now())

	if status, _ := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"new_password": "newpassword456"}); status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 without a token, got %d", status)
	}
	if status, _ := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-invalid-token", "new_password": "short"}); status != fiber.StatusBadRequest {
		t.Errorf("Expected 400 for a short password, got %d", status)
	}
	if status, _ := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-invalid-token", "new_password": "newpassword456"}); status != fiber.StatusOK {
		t.Errorf("Expected a rejected attempt to leave the token usable, got %d", status)
	}
}

// A token only ever resets the account it was issued to.
func TestAuthHandler_ResetPassword_LeavesOtherAccountsAlone(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	ownerID, _ := testutil.CreateTestUser(t, queries, "reset-owner@test.com")
	testutil.CreateTestUser(t, queries, "reset-bystander@test.com")
	seedResetToken(t, pool, ownerID, "reset-owner-token", time.Now())

	postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-owner-token", "new_password": "newpassword456"})

	if s := loginStatus(t, app, "reset-bystander@test.com", "password123"); s != fiber.StatusOK {
		t.Errorf("Expected the other account's password to be unchanged, got %d", s)
	}
	if s := loginStatus(t, app, "reset-bystander@test.com", "newpassword456"); s != fiber.StatusUnauthorized {
		t.Errorf("Expected the new password not to open the other account, got %d", s)
	}
}

// The link reached the inbox, which is what verification proves.
func TestAuthHandler_ResetPassword_VerifiesEmail(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	hashed, _ := utils.HashPassword("password123")
	user, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:     "reset-unverified@test.com",
		Password:  hashed,
		Firstname: "Test",
		Lastname:  "User",
	})
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if s := loginStatus(t, app, "reset-unverified@test.com", "password123"); s != fiber.StatusForbidden {
		t.Fatalf("Expected an unverified account to be refused, got %d", s)
	}
	seedResetToken(t, pool, user.ID.String(), "reset-unverified-token", time.Now())

	postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-unverified-token", "new_password": "newpassword456"})

	if s := loginStatus(t, app, "reset-unverified@test.com", "newpassword456"); s != fiber.StatusOK {
		t.Errorf("Expected the account to be verified by the reset, got %d", s)
	}
}

func TestAuthHandler_ChangePassword_ClearsPendingResetToken(t *testing.T) {
	pool, queries, app := setupPasswordResetApp(t)
	defer testutil.CleanupTestDB(t, pool)

	userID, token := testutil.CreateTestUser(t, queries, "reset-changed@test.com")
	seedResetToken(t, pool, userID, "reset-changed-token", time.Now())

	body, _ := json.Marshal(map[string]interface{}{"old_password": "password123", "new_password": "changed456"})
	req := testutil.NewJSONRequest(http.MethodPut, "/api/auth/change-password", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	if resp, err := app.Test(req); err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Password change failed: %v", err)
	}

	status, _ := postPublicJSON(t, app, "/auth/reset-password", map[string]interface{}{"token": "reset-changed-token", "new_password": "newpassword456"})
	if status != fiber.StatusNotFound {
		t.Errorf("Expected a password change to void the pending reset link, got %d", status)
	}
}
