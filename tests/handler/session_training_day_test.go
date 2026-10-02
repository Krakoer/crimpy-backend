package handler_test

import (
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func postTrainingDaySession(t *testing.T, app *fiber.App, token string, extra map[string]interface{}) *http.Response {
	t.Helper()

	reqBody := map[string]interface{}{
		"name":     "Training day session",
		"notes":    "",
		"activity": 0,
		"duration": 600,
	}
	for k, v := range extra {
		reqBody[k] = v
	}
	body, _ := json.Marshal(reqBody)

	req := testutil.NewJSONRequest(http.MethodPost, "/api/sessions", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	return resp
}

// The day the device sent is the one stored: a hang begun at 00:30 in Paris on
// a Tuesday is 22:30 on Monday in UTC, and the athlete trained it on Monday.
func TestSessionHandler_CreateSession_StoresTheTrainingDaySent(t *testing.T) {
	app, token := rpeApp(t, "trainingday-sent@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{
		"date":         "2026-09-28T22:30:00Z",
		"training_day": "2026-09-28",
	})
	if created["training_day"] != "2026-09-28" {
		t.Fatalf("Expected training_day 2026-09-28, got %v", created["training_day"])
	}

	// West of UTC the training day sits a day before the UTC date: 21:00 in
	// New York is 01:00 the next day in UTC.
	west := createRPESession(t, app, token, map[string]interface{}{
		"date":         "2026-09-29T01:00:00Z",
		"training_day": "2026-09-28",
	})
	if west["training_day"] != "2026-09-28" {
		t.Fatalf("Expected training_day 2026-09-28, got %v", west["training_day"])
	}
}

// A client that sends no training day gets the rule applied in UTC, which is
// how the rows older than the column were filled.
func TestSessionHandler_CreateSession_DerivesTheTrainingDayWhenNoneIsSent(t *testing.T) {
	app, token := rpeApp(t, "trainingday-derived@test.com")

	early := createRPESession(t, app, token, map[string]interface{}{"date": "2026-09-29T03:59:00Z"})
	if early["training_day"] != "2026-09-28" {
		t.Fatalf("Expected a session before 04:00 UTC filed under the day before, got %v", early["training_day"])
	}
	late := createRPESession(t, app, token, map[string]interface{}{"date": "2026-09-29T04:00:00Z"})
	if late["training_day"] != "2026-09-29" {
		t.Fatalf("Expected a session from 04:00 UTC filed under its own day, got %v", late["training_day"])
	}
}

func TestSessionHandler_CreateSession_RefusesAnUnusableTrainingDay(t *testing.T) {
	app, token := rpeApp(t, "trainingday-refused@test.com")

	for _, day := range []string{"28/09/2026", "2026-09-28T00:00:00Z", "2026-09-26", "2026-09-30"} {
		resp := postTrainingDaySession(t, app, token, map[string]interface{}{
			"date":         "2026-09-28T12:00:00Z",
			"training_day": day,
		})
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for training_day %q, got %d", day, resp.StatusCode)
		}
	}

	// The widest the bounds allow on either side are still dates some zone
	// could have trained them on.
	for _, day := range []string{"2026-09-27", "2026-09-29"} {
		resp := postTrainingDaySession(t, app, token, map[string]interface{}{
			"date":         "2026-09-28T12:00:00Z",
			"training_day": day,
		})
		if resp.StatusCode != fiber.StatusCreated {
			t.Errorf("Expected 201 for training_day %q, got %d", day, resp.StatusCode)
		}
	}
}

func TestSessionHandler_UpdateSession_MovesTheTrainingDayWithTheDate(t *testing.T) {
	app, token := rpeApp(t, "trainingday-update@test.com")

	created := createRPESession(t, app, token, map[string]interface{}{
		"date":         "2026-09-28T22:30:00Z",
		"training_day": "2026-09-28",
	})
	id := created["id"].(string)

	// A date sent with its day stores both.
	resp, updated := updateRPESession(t, app, token, id, map[string]interface{}{
		"date":         "2026-09-24T23:00:00Z",
		"training_day": "2026-09-24",
	})
	if resp.StatusCode != fiber.StatusOK || updated["training_day"] != "2026-09-24" {
		t.Fatalf("Expected training_day 2026-09-24, got %d %v", resp.StatusCode, updated["training_day"])
	}

	// A request that moves nothing keeps the day it was filed under.
	resp, updated = updateRPESession(t, app, token, id, map[string]interface{}{"notes": "felt strong"})
	if resp.StatusCode != fiber.StatusOK || updated["training_day"] != "2026-09-24" {
		t.Fatalf("Expected the stored training_day kept, got %d %v", resp.StatusCode, updated["training_day"])
	}

	// A date sent alone takes its day with it, so the two cannot drift apart.
	resp, updated = updateRPESession(t, app, token, id, map[string]interface{}{"date": "2026-09-20T10:00:00Z"})
	if resp.StatusCode != fiber.StatusOK || updated["training_day"] != "2026-09-20" {
		t.Fatalf("Expected training_day re-derived as 2026-09-20, got %d %v", resp.StatusCode, updated["training_day"])
	}

	// A day sent alone is held to the stored date.
	resp, _ = updateRPESession(t, app, token, id, map[string]interface{}{"training_day": "2026-09-10"})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a day far from the stored date, got %d", resp.StatusCode)
	}
	resp, updated = updateRPESession(t, app, token, id, map[string]interface{}{"training_day": "2026-09-19"})
	if resp.StatusCode != fiber.StatusOK || updated["training_day"] != "2026-09-19" {
		t.Fatalf("Expected training_day 2026-09-19, got %d %v", resp.StatusCode, updated["training_day"])
	}
}

// The history list carries the day too, since it is what the portal files a
// session under.
func TestSessionHandler_ListSessions_CarriesTheTrainingDay(t *testing.T) {
	app, token := rpeApp(t, "trainingday-list@test.com")

	createRPESession(t, app, token, map[string]interface{}{
		"date":         "2026-09-28T22:30:00Z",
		"training_day": "2026-09-28",
	})

	req := testutil.NewRequestWithAuth(http.MethodGet, "/api/sessions", nil, token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to list sessions: %v", err)
	}
	var sessions []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	if len(sessions) != 1 || sessions[0]["training_day"] != "2026-09-28" {
		t.Fatalf("Expected one session carrying training_day 2026-09-28, got %v", sessions)
	}
}
