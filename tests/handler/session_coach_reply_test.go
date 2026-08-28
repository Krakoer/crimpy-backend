package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestCoachHandler_SetClientSessionReply_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "replycoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "replyuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	sessionID := createSessionWithNotes(t, app, userToken, "Felt heavy today")

	reply := writeCoachReply(t, app, coachToken, userID, sessionID, "Noted, I added a rest day", fiber.StatusOK)

	if reply["coach_reply"] != "Noted, I added a rest day" {
		t.Errorf("Expected the reply to be echoed back, got %v", reply["coach_reply"])
	}
	if reply["coach_reply_at"] == nil {
		t.Error("Expected coach_reply_at to be set alongside the reply")
	}
	if reply["coach_reply_read"] != false {
		t.Errorf("Expected a fresh reply to be unread, got %v", reply["coach_reply_read"])
	}
}

func TestCoachHandler_SetClientSessionReply_TooLong(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "longreplycoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "longreplyuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	sessionID := createSessionWithNotes(t, app, userToken, "Notes")

	long := ""
	for range 4001 {
		long += "a"
	}
	writeCoachReply(t, app, coachToken, userID, sessionID, long, fiber.StatusBadRequest)

	// The cap counts characters, so a reply of accented ones fits exactly as
	// many as an ASCII one does.
	accented := ""
	for range maxAcceptedCoachReply {
		accented += "\u00e9"
	}
	writeCoachReply(t, app, coachToken, userID, sessionID, accented, fiber.StatusOK)
}

// maxAcceptedCoachReply mirrors the handler's cap, so the test states the
// boundary it is checking rather than a number pulled from nowhere.
const maxAcceptedCoachReply = 4000

func TestCoachHandler_SetClientSessionReply_OtherCoachForbidden(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, _ := testutil.CreateTestValidatedCoachUser(t, pool, queries, "ownreplycoach@test.com")
	_, strangerToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "strangerreplycoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "isolationreplyuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	sessionID := createSessionWithNotes(t, app, userToken, "Notes")

	writeCoachReply(t, app, strangerToken, userID, sessionID, "Not mine to answer", fiber.StatusForbidden)
}

func TestCoachHandler_SetClientSessionReply_OtherClientSessionNotFound(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "crossreplycoach@test.com")
	enrolledID, _ := testutil.CreateTestUser(t, queries, "crossreplyenrolled@test.com")
	_, outsiderToken := testutil.CreateTestUser(t, queries, "crossreplyoutsider@test.com")
	enrollUserDirect(t, pool, coachID, enrolledID)
	outsiderSession := createSessionWithNotes(t, app, outsiderToken, "Notes")

	writeCoachReply(t, app, coachToken, enrolledID, outsiderSession, "Wrong athlete", fiber.StatusNotFound)
}

func TestCoachHandler_SetClientSessionReply_EmptyClearsIt(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "clearreplycoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "clearreplyuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	sessionID := createSessionWithNotes(t, app, userToken, "Notes")

	writeCoachReply(t, app, coachToken, userID, sessionID, "First answer", fiber.StatusOK)
	cleared := writeCoachReply(t, app, coachToken, userID, sessionID, "   ", fiber.StatusOK)

	if _, present := cleared["coach_reply"]; present {
		t.Errorf("Expected the reply to be gone, got %v", cleared["coach_reply"])
	}
	if _, present := cleared["coach_reply_at"]; present {
		t.Errorf("Expected the reply date to be gone, got %v", cleared["coach_reply_at"])
	}
}

func TestSessionHandler_MarkCoachReplyRead_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "readreplycoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "readreplyuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	sessionID := createSessionWithNotes(t, app, userToken, "Notes")
	writeCoachReply(t, app, coachToken, userID, sessionID, "Well done", fiber.StatusOK)

	read := markCoachReplyRead(t, app, userToken, sessionID, fiber.StatusOK)
	if read["coach_reply_read"] != true {
		t.Errorf("Expected the reply to read as seen, got %v", read["coach_reply_read"])
	}

	// The list is what the app reads its unread count from, so the flag has to
	// survive the trip through the listing query too.
	listReq := testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/sessions", nil, userToken)
	listResp, err := app.Test(listReq)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	var listed []map[string]interface{}
	json.NewDecoder(listResp.Body).Decode(&listed)
	if len(listed) != 1 {
		t.Fatalf("Expected one session in the listing, got %d", len(listed))
	}
	if listed[0]["coach_reply"] != "Well done" {
		t.Errorf("Expected the listing to carry the reply, got %v", listed[0]["coach_reply"])
	}
	if listed[0]["coach_reply_read"] != true {
		t.Errorf("Expected the listing to report the reply as read, got %v", listed[0]["coach_reply_read"])
	}
}

func TestSessionHandler_MarkCoachReplyRead_NoReply(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
	})

	_, userToken := testutil.CreateTestUser(t, queries, "noreplyuser@test.com")
	sessionID := createSessionWithNotes(t, app, userToken, "Notes")

	markCoachReplyRead(t, app, userToken, sessionID, fiber.StatusNotFound)
}

func TestSessionHandler_MarkCoachReplyRead_OtherUserForbidden(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "isolreadcoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "isolreadowner@test.com")
	_, strangerToken := testutil.CreateTestUser(t, queries, "isolreadstranger@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	sessionID := createSessionWithNotes(t, app, userToken, "Notes")
	writeCoachReply(t, app, coachToken, userID, sessionID, "Well done", fiber.StatusOK)

	markCoachReplyRead(t, app, strangerToken, sessionID, fiber.StatusForbidden)
}

func TestCoachHandler_SetClientSessionReply_RewriteMarksUnread(t *testing.T) {
	t.Setenv("JWT_SECRET", "devsecret")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler: handler.NewSessionHandler(queries, pool),
		CoachHandler:   handler.NewCoachHandler(queries, pool),
	})

	coachID, coachToken := testutil.CreateTestValidatedCoachUser(t, pool, queries, "rewritecoach@test.com")
	userID, userToken := testutil.CreateTestUser(t, queries, "rewriteuser@test.com")
	enrollUserDirect(t, pool, coachID, userID)
	sessionID := createSessionWithNotes(t, app, userToken, "Notes")

	writeCoachReply(t, app, coachToken, userID, sessionID, "First answer", fiber.StatusOK)
	markCoachReplyRead(t, app, userToken, sessionID, fiber.StatusOK)
	rewritten := writeCoachReply(t, app, coachToken, userID, sessionID, "Correction", fiber.StatusOK)

	if rewritten["coach_reply_read"] != false {
		t.Errorf("Expected a rewritten reply to be unread again, got %v", rewritten["coach_reply_read"])
	}
}

func createSessionWithNotes(t *testing.T, app *fiber.App, token, notes string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name":          "Session with notes",
		"notes":         notes,
		"is_assessment": false,
		"activity":      0,
		"duration":      1800,
	})
	req := testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/sessions", body, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create the session: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected status %d creating the session, got %d", fiber.StatusCreated, resp.StatusCode)
	}
	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	return created["id"].(string)
}

func writeCoachReply(t *testing.T, app *fiber.App, coachToken, userID, sessionID, reply string, expected int) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"reply": reply})
	url := fmt.Sprintf("/api/coach/clients/%s/sessions/%s/reply", userID, sessionID)
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, url, body, coachToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	if resp.StatusCode != expected {
		t.Fatalf("Expected status %d, got %d", expected, resp.StatusCode)
	}
	var decoded map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&decoded)
	return decoded
}

func markCoachReplyRead(t *testing.T, app *fiber.App, token, sessionID string, expected int) map[string]interface{} {
	t.Helper()
	url := fmt.Sprintf("/api/sessions/%s/coach-reply/read", sessionID)
	req := testutil.NewJSONRequestWithAuth(http.MethodPut, url, nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	if resp.StatusCode != expected {
		t.Fatalf("Expected status %d, got %d", expected, resp.StatusCode)
	}
	var decoded map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&decoded)
	return decoded
}
