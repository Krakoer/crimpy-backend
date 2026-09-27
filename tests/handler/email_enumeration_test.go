package handler_test

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/handler"
	"crimpy/backend/internal/utils"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setupEnumerationApp(t *testing.T) (*pgxpool.Pool, *db.Queries, *fiber.App) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		AuthHandler: handler.NewAuthHandler(queries, pool),
	})
	return pool, queries, app
}

func postAuth(t *testing.T, app *fiber.App, path string, payload map[string]interface{}) (int, map[string]interface{}) {
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

func registration(email string, isCoach bool) map[string]interface{} {
	return map[string]interface{}{
		"email":     email,
		"password":  "newcomer123",
		"firstname": "Newcomer",
		"lastname":  "Tester",
		"is_coach":  isCoach,
	}
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func createUnverifiedUser(t *testing.T, queries *db.Queries, email string) db.User {
	t.Helper()
	hashed, _ := utils.HashPassword("password123")
	user, err := queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:     email,
		Password:  hashed,
		Firstname: "Test",
		Lastname:  "User",
	})
	if err != nil {
		t.Fatalf("Failed to create unverified user: %v", err)
	}
	return user
}

type verificationState struct {
	token  *string
	sentAt *time.Time
}

func readVerificationState(t *testing.T, pool *pgxpool.Pool, email string) verificationState {
	t.Helper()
	var state verificationState
	err := pool.QueryRow(context.Background(),
		`SELECT verification_token, verification_email_sent_at FROM users WHERE email = $1`, email,
	).Scan(&state.token, &state.sentAt)
	if err != nil {
		t.Fatalf("Failed to read verification state: %v", err)
	}
	return state
}

var createdAtShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? [+-]\d{4} \S+$`)

func TestAuthHandler_Register_TakenAddressAnswersLikeANewAccount(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	testutil.CreateTestUser(t, queries, "taken-owner@test.com")

	newStatus, fresh := postAuth(t, app, "/auth/register", registration("taken-newcomer@test.com", false))
	takenStatus, taken := postAuth(t, app, "/auth/register", registration(" Taken-Owner@test.com ", false))

	if newStatus != fiber.StatusCreated || takenStatus != fiber.StatusCreated {
		t.Fatalf("Expected 201 for both, got %d and %d", newStatus, takenStatus)
	}
	if fresh["message"] != taken["message"] {
		t.Errorf("Expected the same message, got %q and %q", fresh["message"], taken["message"])
	}

	freshUser, _ := fresh["user"].(map[string]interface{})
	takenUser, _ := taken["user"].(map[string]interface{})
	if freshUser == nil || takenUser == nil {
		t.Fatalf("Expected a user in both answers, got %v and %v", fresh, taken)
	}
	if got, want := sortedKeys(takenUser), sortedKeys(freshUser); len(got) != len(want) {
		t.Errorf("Expected the same user keys, got %v and %v", want, got)
	} else {
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("Expected the same user keys, got %v and %v", want, got)
				break
			}
		}
	}
	for _, key := range []string{"email_verified", "is_admin", "is_coach", "coach_validated"} {
		if freshUser[key] != takenUser[key] {
			t.Errorf("Expected %s to match, got %v and %v", key, freshUser[key], takenUser[key])
		}
	}
	if takenUser["email"] != "taken-owner@test.com" || takenUser["firstname"] != "Newcomer" {
		t.Errorf("Expected the taken answer to echo the request, got %v", takenUser)
	}
	for _, created := range []interface{}{freshUser["created_at"], takenUser["created_at"]} {
		if s, _ := created.(string); !createdAtShape.MatchString(s) {
			t.Errorf("Expected created_at shaped like a stored timestamp, got %q", created)
		}
	}

	owner, _ := queries.GetUserByEmail(context.Background(), "taken-owner@test.com")
	if takenUser["id"] == owner.ID.String() {
		t.Error("Expected the taken answer not to carry the owner's id")
	}
}

func TestAuthHandler_Register_TakenAddressKeepsTheOwnersAccount(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	testutil.CreateTestCoachUser(t, queries, "taken-keep@test.com")

	postAuth(t, app, "/auth/register", registration("taken-keep@test.com", false))

	owner, err := queries.GetUserByEmail(context.Background(), "taken-keep@test.com")
	if err != nil {
		t.Fatalf("Failed to load owner: %v", err)
	}
	if owner.Firstname != "Coach" || !owner.IsCoach {
		t.Errorf("Expected the owner's account to be untouched, got %+v", owner)
	}
	if utils.CheckPassword(owner.Password, "password123") != nil {
		t.Error("Expected the owner's password to be unchanged")
	}
}

// An unverified owner is most likely the person retrying, so they get the
// verification link again rather than a note about the attempt.
func TestAuthHandler_Register_TakenUnverifiedAddressResendsVerification(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	createUnverifiedUser(t, queries, "taken-unverified@test.com")

	postAuth(t, app, "/auth/register", registration("taken-unverified@test.com", false))

	if state := readVerificationState(t, pool, "taken-unverified@test.com"); state.token == nil || state.sentAt == nil {
		t.Error("Expected a new verification link to be issued")
	}
}

func TestAuthHandler_Register_TakenUnverifiedAddressRespectsCooldown(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	createUnverifiedUser(t, queries, "taken-cooldown@test.com")
	postAuth(t, app, "/auth/resend-verification", map[string]interface{}{"email": "taken-cooldown@test.com"})
	before := readVerificationState(t, pool, "taken-cooldown@test.com")

	status, _ := postAuth(t, app, "/auth/register", registration("taken-cooldown@test.com", false))
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}

	after := readVerificationState(t, pool, "taken-cooldown@test.com")
	if before.token == nil || after.token == nil || *before.token != *after.token {
		t.Error("Expected the link sent within the cooldown to stay the valid one")
	}
}

func TestAuthHandler_ResendVerification_AnswersTheSameForEveryAddress(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	testutil.CreateTestUser(t, queries, "resend-verified@test.com")
	createUnverifiedUser(t, queries, "resend-unverified@test.com")

	cases := []string{"resend-nobody@test.com", "resend-verified@test.com", "resend-unverified@test.com", "resend-unverified@test.com"}
	var first map[string]interface{}
	for i, email := range cases {
		status, response := postAuth(t, app, "/auth/resend-verification", map[string]interface{}{"email": email})
		if status != fiber.StatusOK {
			t.Errorf("Expected 200 for %s (call %d), got %d", email, i, status)
		}
		if first == nil {
			first = response
		} else if response["message"] != first["message"] {
			t.Errorf("Expected the same message for %s (call %d), got %q", email, i, response["message"])
		}
	}
}

func TestAuthHandler_ResendVerification_SendsOnlyOutsideTheCooldown(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	createUnverifiedUser(t, queries, "resend-cooldown@test.com")

	postAuth(t, app, "/auth/resend-verification", map[string]interface{}{"email": "resend-cooldown@test.com"})
	first := readVerificationState(t, pool, "resend-cooldown@test.com")
	if first.token == nil {
		t.Fatal("Expected the first request to issue a link")
	}

	postAuth(t, app, "/auth/resend-verification", map[string]interface{}{"email": "resend-cooldown@test.com"})
	second := readVerificationState(t, pool, "resend-cooldown@test.com")
	if second.token == nil || *second.token != *first.token {
		t.Error("Expected a request within the cooldown to leave the sent link in place")
	}

	if _, err := pool.Exec(context.Background(), `UPDATE users SET verification_email_sent_at = now() - interval '11 minutes' WHERE email = $1`, "resend-cooldown@test.com"); err != nil {
		t.Fatalf("Failed to move the cooldown back: %v", err)
	}
	postAuth(t, app, "/auth/resend-verification", map[string]interface{}{"email": "resend-cooldown@test.com"})
	third := readVerificationState(t, pool, "resend-cooldown@test.com")
	if third.token == nil || *third.token == *first.token {
		t.Error("Expected a request after the cooldown to issue a new link")
	}
}

func TestAuthHandler_ResendVerification_MissingEmail(t *testing.T) {
	pool, _, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	if status, _ := postAuth(t, app, "/auth/resend-verification", map[string]interface{}{"email": " "}); status != fiber.StatusBadRequest {
		t.Errorf("Expected 400, got %d", status)
	}
}

// Outside the test environment a missing Resend key fails every send, which is
// the path a Resend outage takes.
func TestAuthHandler_Register_TakenAddressHidesAFailedEmail(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	testutil.CreateTestUser(t, queries, "taken-outage@test.com")
	t.Setenv("ENV", "development")
	t.Setenv("RESEND_API_KEY", "")

	status, response := postAuth(t, app, "/auth/register", registration("taken-outage@test.com", false))
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201, got %d", status)
	}
	if response["message"] != "User registered successfully. Please check your email to verify your account." {
		t.Errorf("Expected the plain registration message, got %q", response["message"])
	}
}

func accountNoticeSentAt(t *testing.T, pool *pgxpool.Pool, email string) *time.Time {
	t.Helper()
	var sentAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT account_notice_sent_at FROM users WHERE email = $1`, email).Scan(&sentAt); err != nil {
		t.Fatalf("Failed to read account notice: %v", err)
	}
	return sentAt
}

// Every attempt on a verified address would otherwise email its owner, which
// turns registration into a way to flood an inbox.
func TestAuthHandler_Register_TakenVerifiedAddressNoticeHasACooldown(t *testing.T) {
	pool, queries, app := setupEnumerationApp(t)
	defer testutil.CleanupTestDB(t, pool)

	testutil.CreateTestUser(t, queries, "taken-notice@test.com")

	postAuth(t, app, "/auth/register", registration("taken-notice@test.com", false))
	first := accountNoticeSentAt(t, pool, "taken-notice@test.com")
	if first == nil {
		t.Fatal("Expected the first attempt to notify the owner")
	}

	status, _ := postAuth(t, app, "/auth/register", registration("taken-notice@test.com", false))
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 within the cooldown, got %d", status)
	}
	if second := accountNoticeSentAt(t, pool, "taken-notice@test.com"); second == nil || !second.Equal(*first) {
		t.Error("Expected an attempt within the cooldown to send no second notice")
	}

	if _, err := pool.Exec(context.Background(), `UPDATE users SET account_notice_sent_at = now() - interval '11 minutes' WHERE email = $1`, "taken-notice@test.com"); err != nil {
		t.Fatalf("Failed to move the cooldown back: %v", err)
	}
	postAuth(t, app, "/auth/register", registration("taken-notice@test.com", false))
	if third := accountNoticeSentAt(t, pool, "taken-notice@test.com"); third == nil || !third.After(*first) {
		t.Error("Expected an attempt after the cooldown to notify the owner again")
	}
}
