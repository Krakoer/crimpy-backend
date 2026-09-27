package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func setupExerciseLinkApp(t *testing.T, prefix string) (app *fiber.App, coachToken string) {
	t.Helper()
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	_, coachToken = testutil.CreateTestValidatedCoachUser(t, pool, queries, prefix+"coach@test.com")
	app = testutil.SetupFiberApp(testutil.HandlerConfig{
		ExerciseHandler: handler.NewExerciseHandler(queries, pool),
	})
	return app, coachToken
}

// postExerciseLink creates an exercise with the given video_link and answers the
// status and the value that came back stored.
func postExerciseLink(t *testing.T, app *fiber.App, token, link string) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"name": "Pull up", "video_link": link})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/coach/exercises", body, token))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		return resp.StatusCode, ""
	}
	var created map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&created)
	stored, _ := created["video_link"].(string)
	return resp.StatusCode, stored
}

// The reason this validation exists: the value is joined onto training items, so
// it reaches the athlete and, through a frozen session prescription, a coach who
// took that athlete over. A javascript: url stored here would run in theirs.
func TestExerciseHandler_RefusesANonHttpVideoLink(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink1")

	for _, link := range []string{
		"javascript:alert(document.cookie)",
		"JavaScript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"intent://example.com/x",
		"mailto:coach@example.com",
		"//evil.com/x",
		"javascript:void(0)//example.com/x",
	} {
		status, _ := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %q, got %d", link, status)
		}
	}
}

func TestExerciseHandler_RefusesProseInTheVideoLink(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink2")

	// A sentence with a full stop in it is still a sentence: without the
	// whitespace test it would be stored as a host that resolves to nothing.
	for _, link := range []string{
		"ask me for the video",
		"Ask me. I will show you",
		"3 series de 10. Cf. la video",
	} {
		status, _ := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %q, got %d", link, status)
		}
	}
}

func TestExerciseHandler_StoresAnHttpVideoLinkAsTyped(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink3")

	for _, link := range []string{
		"https://example.com/pull-up",
		"http://example.com/pull-up",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	} {
		status, stored := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusCreated {
			t.Fatalf("Expected 201 for %q, got %d", link, status)
		}
		if stored != link {
			t.Errorf("Expected %q stored as typed, got %q", link, stored)
		}
	}
}

// What a coach actually types. Stored as https rather than refused, so the
// field keeps working, and normalized here so every client reads one form.
func TestExerciseHandler_NormalizesASchemeLessVideoLink(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink4")

	cases := map[string]string{
		"www.youtube.com/watch?v=abc": "https://www.youtube.com/watch?v=abc",
		"youtu.be/dQw4w9WgXcQ":        "https://youtu.be/dQw4w9WgXcQ",
		"example.com:8080/v":          "https://example.com:8080/v",
	}
	for link, want := range cases {
		status, stored := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusCreated {
			t.Fatalf("Expected 201 for %q, got %d", link, status)
		}
		if stored != want {
			t.Errorf("Expected %q stored as %q, got %q", link, want, stored)
		}
	}
}

// Clearing the field is not the same as typing something invalid into it.
func TestExerciseHandler_TakesAnEmptyVideoLink(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink5")

	for _, link := range []string{"", "   "} {
		status, stored := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusCreated {
			t.Errorf("Expected 201 for a cleared link, got %d", status)
		}
		if stored != "" {
			t.Errorf("Expected a cleared link to stay empty, got %q", stored)
		}
	}
}

func TestExerciseHandler_UpdateValidatesTheVideoLink(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink6")

	body, _ := json.Marshal(map[string]interface{}{"name": "Pull up", "video_link": "https://example.com/pull-up"})
	created, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/coach/exercises", body, coachToken))
	if err != nil || created.StatusCode != fiber.StatusCreated {
		t.Fatalf("Failed to create the exercise: err=%v status=%v", err, created.StatusCode)
	}
	var exercise map[string]interface{}
	json.NewDecoder(created.Body).Decode(&exercise)
	id := exercise["id"].(string)

	bad, _ := json.Marshal(map[string]interface{}{"name": "Pull up", "video_link": "javascript:alert(1)"})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/coach/exercises/"+id, bad, coachToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("Expected 400 updating onto a javascript link, got %d", resp.StatusCode)
	}

	good, _ := json.Marshal(map[string]interface{}{"name": "Pull up", "video_link": "www.youtube.com/watch?v=abc"})
	ok, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPut, "/api/coach/exercises/"+id, good, coachToken))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var updated map[string]interface{}
	json.NewDecoder(ok.Body).Decode(&updated)
	if updated["video_link"] != "https://www.youtube.com/watch?v=abc" {
		t.Errorf("Expected the update to normalize the link, got %v", updated["video_link"])
	}
}

// A value the function accepts has to be one the database and a browser will
// both take. A control byte used to pass the scheme-less branch untested and
// fail at the insert, so the coach got a 500 for a payload problem.
func TestExerciseHandler_RefusesAnUnstorableVideoLink(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink7")

	for name, link := range map[string]string{
		"nul byte, no scheme":   "example.com/a\x00b",
		"nul byte, with scheme": "https://example.com/a\x00b",
		"vertical tab":          "example.com/a\vb",
		"form feed":             "example.com/a\fb",
		"port out of range":     "example.com:99999/v",
		// url.Parse cuts the fragment off before its own control byte check, so
		// these reached the insert and came back as a 500 for a bad payload.
		"nul in fragment, no scheme":   "example.com/v#a\x00b",
		"nul in fragment, with scheme": "https://example.com/v#a\x00b",
		"delete in fragment":           "https://example.com/v#a\x7fb",
		"unit separator in query":      "example.com/v?q=a\x1fb",
	} {
		status, _ := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %s, got %d", name, status)
		}
	}
}

// The clients test whitespace with \s, which covers more than the ASCII kinds.
// A link this accepted and they refused to render would be a working button on
// one surface and nothing at all on the other, with no error shown anywhere.
func TestExerciseHandler_RefusesTheWhitespaceTheClientsRefuse(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink8")

	for name, link := range map[string]string{
		"non breaking space": "www.youtube.com/watch?v=dQw4w9\u00a0WgXcQ",
		"ideographic space":  "www.youtube.com/watch?v=a\u3000b",
		"line separator":     "www.youtube.com/watch?v=a\u2028b",
		"byte order mark":    "www.youtube.com/watch?v=a\ufeffb",
		"zero width space":   "www.youtube.com/watch?v=a\u200bb",
	} {
		status, _ := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for a %s, got %d", name, status)
		}
	}
}

// An octet above 255 is not an address the clients agree on: the portal's URL
// constructor refuses it and renders nothing, while the app hands it to a
// browser that cannot resolve it. The backend decides what is stored, so
// refusing it here is what keeps the two from diverging.
func TestExerciseHandler_RefusesAnOutOfRangeIPv4(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	app, coachToken := setupExerciseLinkApp(t, "exlink9")

	for _, link := range []string{
		"192.168.1.300/demo.mp4",
		"999.1.1.1/demo.mp4",
		"192.168.1.256:8080/v",
	} {
		status, _ := postExerciseLink(t, app, coachToken, link)
		if status != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for %q, got %d", link, status)
		}
	}

	// The supported form still works, so the range check did not take the
	// legitimate case with it.
	status, stored := postExerciseLink(t, app, coachToken, "192.168.1.5:8080/demo.mp4")
	if status != fiber.StatusCreated || stored != "https://192.168.1.5:8080/demo.mp4" {
		t.Errorf("Expected the in-range address to be stored, got %d %q", status, stored)
	}
}
