package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// The setup a percent_bw freeze needs: an athlete who can record a weight and
// play a session from a training.
func setupBodyweightFreeze(t *testing.T, prefix string) (app *fiber.App, userToken, trainingID string) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	_, userToken = testutil.CreateTestUser(t, queries, prefix+"user@test.com")

	app = testutil.SetupFiberApp(testutil.HandlerConfig{
		SessionHandler:    handler.NewSessionHandler(queries, pool),
		TrainingHandler:   handler.NewTrainingHandler(queries, pool),
		BodyweightHandler: handler.NewBodyweightHandler(queries, pool),
	})

	// The athlete's own training: a session played straight from one, which is
	// the plain case for a percent_bw load, needs no coach and no program.
	status, training := postJSON(t, app, "/api/trainings", userToken, map[string]interface{}{
		"title": "Weighted pull ups",
		"items": []map[string]interface{}{
			{"type": "exercise", "reps": 5, "loads": []map[string]interface{}{{"value": 80, "unit": "percent_bw"}}},
		},
	})
	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 creating the training, got %d: %v", status, training)
	}
	return app, userToken, training["id"].(string)
}

func recordBodyweight(t *testing.T, app *fiber.App, userToken string, weight float64) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"weight_kg": weight})
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodPost, "/api/user/bodyweights", body, userToken))
	if err != nil {
		t.Fatalf("Recording the bodyweight failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected 201 recording the bodyweight, got %d", resp.StatusCode)
	}
}

func frozenBodyweight(t *testing.T, created map[string]interface{}) (float64, bool) {
	t.Helper()
	// A client's own prescription carries no resolved_against at all until
	// something is frozen into it, so its absence is the same answer as an
	// absent weight rather than a broken shape.
	inputs, ok := sessionPrescription(t, created)["resolved_against"].(map[string]interface{})
	if !ok {
		return 0, false
	}
	value, present := inputs["bodyweight_kg"]
	if !present {
		return 0, false
	}
	return value.(float64), true
}

// The device sends what it actually resolved the loads against, and that is
// what the session records, because the device can hold a measurement the
// server has not been told about yet.
func TestSessionBodyweight_FreezesWhatTheDeviceUsed(t *testing.T) {
	app, userToken, trainingID := setupBodyweightFreeze(t, "bwfreeze1")
	recordBodyweight(t, app, userToken, 70)

	created := playSession(t, app, userToken, map[string]interface{}{
		"training_id":   trainingID,
		"bodyweight_kg": 72.5,
	})

	frozen, present := frozenBodyweight(t, created)
	if !present {
		t.Fatalf("Expected the bodyweight frozen with the prescription")
	}
	if frozen != 72.5 {
		t.Errorf("Expected the weight the device used (72.5), got %v", frozen)
	}
}

// A client that sends none is answered from the series, so an older app still
// produces a session whose percent_bw loads can be restated.
func TestSessionBodyweight_FallsBackToTheSeries(t *testing.T) {
	app, userToken, trainingID := setupBodyweightFreeze(t, "bwfreeze2")
	recordBodyweight(t, app, userToken, 68.25)

	created := playSession(t, app, userToken, map[string]interface{}{"training_id": trainingID})

	frozen, present := frozenBodyweight(t, created)
	if !present {
		t.Fatalf("Expected the bodyweight frozen from the series")
	}
	if frozen != 68.25 {
		t.Errorf("Expected 68.25 from the series, got %v", frozen)
	}
}

// The latest measurement is the one in effect, not the first one recorded.
func TestSessionBodyweight_FreezesTheLatestOnFile(t *testing.T) {
	app, userToken, trainingID := setupBodyweightFreeze(t, "bwfreeze3")
	recordBodyweight(t, app, userToken, 70)
	recordBodyweight(t, app, userToken, 71.5)

	created := playSession(t, app, userToken, map[string]interface{}{"training_id": trainingID})

	frozen, _ := frozenBodyweight(t, created)
	if frozen != 71.5 {
		t.Errorf("Expected the latest weight (71.5), got %v", frozen)
	}
}

// Absent rather than zero: an athlete who has never weighed themselves has a
// session whose percent_bw loads nothing can restate, and a reader has to be
// able to say so rather than read 0kg as a real weight.
func TestSessionBodyweight_IsAbsentWhenNothingIsKnown(t *testing.T) {
	app, userToken, trainingID := setupBodyweightFreeze(t, "bwfreeze4")

	created := playSession(t, app, userToken, map[string]interface{}{"training_id": trainingID})

	if frozen, present := frozenBodyweight(t, created); present {
		t.Errorf("Expected no bodyweight on the prescription, got %v", frozen)
	}
}

// Frozen means frozen: a weight recorded after the session does not restate
// what that session's loads were read against.
func TestSessionBodyweight_SurvivesALaterMeasurement(t *testing.T) {
	app, userToken, trainingID := setupBodyweightFreeze(t, "bwfreeze5")
	recordBodyweight(t, app, userToken, 70)

	created := playSession(t, app, userToken, map[string]interface{}{"training_id": trainingID})
	recordBodyweight(t, app, userToken, 64)

	sessionID := created["id"].(string)
	resp, err := app.Test(testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/sessions/"+sessionID, nil, userToken))
	if err != nil {
		t.Fatalf("Re-reading the session failed: %v", err)
	}
	var detail map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&detail)
	// The detail endpoint wraps the session beside its reps and assessments.
	reread, ok := detail["session"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected a session on the detail read, got %v", detail)
	}

	frozen, present := frozenBodyweight(t, reread)
	if !present {
		t.Fatalf("Expected the frozen bodyweight on the re-read, got %v", reread)
	}
	if frozen != 70 {
		t.Errorf("Expected the session to keep the 70 it was played against, got %v", frozen)
	}
}

func TestSessionBodyweight_RefusesAWeightThatIsNotOne(t *testing.T) {
	app, userToken, trainingID := setupBodyweightFreeze(t, "bwfreeze6")

	for _, weight := range []float64{0, -1, 900} {
		resp := playSessionExpecting(t, app, userToken, map[string]interface{}{
			"training_id":   trainingID,
			"bodyweight_kg": weight,
		})
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Expected 400 for bodyweight_kg %v, got %d", weight, resp.StatusCode)
		}
	}
}

// A run of a training the server cannot read is exactly where a builtin lives,
// and a builtin can carry percent_bw loads like any other. Before this the
// weight was taken from the request, checked, and then dropped on this path.
func TestSessionBodyweight_FreezesOnAClientPrescription(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient1")
	recordBodyweight(t, app, userToken, 70)

	resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
		"prescription":  generatedPrescription("builtin:max-hangs:0"),
		"bodyweight_kg": 72.5,
	})
	detail := getSessionDetail(t, app, userToken, createdSessionID(t, resp))
	session := detail["session"].(map[string]interface{})

	frozen, present := frozenBodyweight(t, session)
	if !present {
		t.Fatalf("Expected the bodyweight frozen into the client prescription")
	}
	if frozen != 72.5 {
		t.Errorf("Expected the weight the device used (72.5), got %v", frozen)
	}
}

func TestSessionBodyweight_ClientPrescriptionFallsBackToTheSeries(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient2")
	recordBodyweight(t, app, userToken, 68.25)

	resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
		"prescription": generatedPrescription("builtin:max-hangs:0"),
	})
	detail := getSessionDetail(t, app, userToken, createdSessionID(t, resp))
	session := detail["session"].(map[string]interface{})

	frozen, present := frozenBodyweight(t, session)
	if !present {
		t.Fatalf("Expected the bodyweight frozen from the series")
	}
	if frozen != 68.25 {
		t.Errorf("Expected 68.25 from the series, got %v", frozen)
	}
}

func TestSessionBodyweight_IsAbsentFromAClientPrescriptionWhenNothingIsKnown(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient3")

	resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
		"prescription": generatedPrescription("builtin:max-hangs:0"),
	})
	detail := getSessionDetail(t, app, userToken, createdSessionID(t, resp))
	session := detail["session"].(map[string]interface{})

	if frozen, present := frozenBodyweight(t, session); present {
		t.Errorf("Expected no bodyweight on the prescription, got %v", frozen)
	}
}

// The prescription describes a training the server has never seen, so anything
// in it the server does not model is the only record of what was asked for.
// Writing the weight in must not cost that, which is why it edits the JSON
// rather than round tripping through the typed snapshot.
func TestSessionBodyweight_LeavesWhatTheServerDoesNotModelAlone(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient4")

	prescription := generatedPrescription("builtin:max-hangs:0")
	prescription["device_protocol"] = "max-hangs-v3"
	prescription["items"].([]map[string]interface{})[0]["device_notch"] = 42

	resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
		"prescription":  prescription,
		"bodyweight_kg": 71,
	})
	detail := getSessionDetail(t, app, userToken, createdSessionID(t, resp))
	session := detail["session"].(map[string]interface{})
	stored := sessionPrescription(t, session)

	if stored["device_protocol"] != "max-hangs-v3" {
		t.Errorf("Expected the unmodelled field to survive, got %v", stored["device_protocol"])
	}
	item := stored["items"].([]interface{})[0].(map[string]interface{})
	if item["device_notch"] != float64(42) {
		t.Errorf("Expected the unmodelled item field to survive, got %v", item["device_notch"])
	}
	if frozen, _ := frozenBodyweight(t, session); frozen != 71 {
		t.Errorf("Expected the weight frozen beside it, got %v", frozen)
	}
}

// A prescription that already names the weight it was resolved against is the
// device's own record of what happened. The series is what we happen to hold,
// which may be a month old, and overwriting with it would store a weight the
// loads were never read against.
func TestSessionBodyweight_KeepsTheWeightAPrescriptionAlreadyNames(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient5")
	recordBodyweight(t, app, userToken, 68)

	prescription := generatedPrescription("builtin:max-hangs:0")
	prescription["resolved_against"] = map[string]interface{}{"bodyweight_kg": 72.5}

	resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
		"prescription": prescription,
	})
	detail := getSessionDetail(t, app, userToken, createdSessionID(t, resp))
	session := detail["session"].(map[string]interface{})

	frozen, present := frozenBodyweight(t, session)
	if !present {
		t.Fatalf("Expected the prescription to keep its own bodyweight")
	}
	if frozen != 72.5 {
		t.Errorf("Expected the 72.5 the prescription named, got %v", frozen)
	}
}

// The request field still wins, since it is the device answering for this very
// run rather than something the prescription was built with earlier.
func TestSessionBodyweight_RequestBeatsAPrescriptionsOwnWeight(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient6")

	prescription := generatedPrescription("builtin:max-hangs:0")
	prescription["resolved_against"] = map[string]interface{}{"bodyweight_kg": 72.5}

	resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
		"prescription":  prescription,
		"bodyweight_kg": 70.1,
	})
	detail := getSessionDetail(t, app, userToken, createdSessionID(t, resp))
	session := detail["session"].(map[string]interface{})

	if frozen, _ := frozenBodyweight(t, session); frozen != 70.1 {
		t.Errorf("Expected the request's 70.1 to win, got %v", frozen)
	}
}

// A weight a prescription froze for itself is stored as it arrived, so this is
// the only place that can refuse one no athlete could weigh. Without the check
// a zero reaches the coach, who reads a percent_bw load as a ratio and divides
// by it.
func TestSessionBodyweight_RefusesAnImplausibleWeightAPrescriptionNames(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient7")
	recordBodyweight(t, app, userToken, 68)

	for _, weight := range []float64{0, -5, 900} {
		prescription := generatedPrescription("builtin:max-hangs:0")
		prescription["resolved_against"] = map[string]interface{}{"bodyweight_kg": weight}

		resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
			"prescription": prescription,
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for a frozen weight of %v, got %d", weight, resp.StatusCode)
		}
	}
}

// The refusal is about the weight being implausible, not about the key being
// there, so a weight on the boundary is still accepted and still wins over the
// series.
func TestSessionBodyweight_AcceptsAPrescriptionWeightOnTheBound(t *testing.T) {
	app, userToken, _ := setupBodyweightFreeze(t, "bwclient8")
	recordBodyweight(t, app, userToken, 68)

	prescription := generatedPrescription("builtin:max-hangs:0")
	prescription["resolved_against"] = map[string]interface{}{"bodyweight_kg": 500}

	resp := postGeneratedSession(t, app, userToken, map[string]interface{}{
		"prescription": prescription,
	})
	detail := getSessionDetail(t, app, userToken, createdSessionID(t, resp))
	session := detail["session"].(map[string]interface{})

	if frozen, _ := frozenBodyweight(t, session); frozen != 500 {
		t.Errorf("Expected the 500 the prescription named, got %v", frozen)
	}
}
