package handler_test

import (
	"crimpy/backend/internal/handler"
	"crimpy/backend/tests/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A hangboard item configured per set and rep declares granularity 'set' and
// carries cycles * reps entries in every array, indexed set * reps + rep.
func perSetHangboardItem() map[string]interface{} {
	return map[string]interface{}{
		"type":               "repeater",
		"cycles":             2,
		"reps":               3,
		"worktime_seconds":   7,
		"rest_seconds":       3,
		"cycle_rest_seconds": 120,
		"hand":               "both",
		"granularity":        "set",
		"edge_sizes_mm":      []interface{}{20, 20, 18, 15, 15, 12},
		"loads": []map[string]interface{}{
			{"value": 10, "unit": "kg"},
			{"value": 11, "unit": "kg"},
			{"value": 12, "unit": "kg"},
			{"value": 13, "unit": "kg"},
			{"value": 14, "unit": "kg"},
			{"value": 15, "unit": "kg"},
		},
		"hand_positions": []interface{}{
			[]interface{}{"HC", "HC", "FC", "FC", "OC", "OC"},
		},
	}
}

// A split-hand item keeps the two hands in loads and left_loads, one entry per
// row in each, and one grip array per hand.
func splitHandItem() map[string]interface{} {
	return map[string]interface{}{
		"type":               "repeater",
		"cycles":             2,
		"reps":               3,
		"worktime_seconds":   7,
		"rest_seconds":       3,
		"cycle_rest_seconds": 120,
		"hand":               "split",
		"granularity":        "set",
		"edge_sizes_mm":      []interface{}{20, 20, 18, 15, 15, 12},
		"loads": []map[string]interface{}{
			{"value": 10, "unit": "kg"},
			{"value": 11, "unit": "kg"},
			{"value": 12, "unit": "kg"},
			{"value": 13, "unit": "kg"},
			{"value": 14, "unit": "kg"},
			{"value": 15, "unit": "kg"},
		},
		"left_loads": []map[string]interface{}{
			{"value": 5, "unit": "kg"},
			{"value": 6, "unit": "kg"},
			{"value": 7, "unit": "kg"},
			{"value": 8, "unit": "kg"},
			{"value": 9, "unit": "kg"},
			{"value": 10, "unit": "kg"},
		},
		"hand_positions": []interface{}{
			[]interface{}{"HC", "HC", "FC", "FC", "OC", "OC"},
			[]interface{}{"HC", "FC", "FC", "OC", "OC", "HC"},
		},
	}
}

func decodeItemArray(t *testing.T, item map[string]interface{}, field string) []interface{} {
	t.Helper()
	value, ok := item[field].([]interface{})
	if !ok {
		t.Fatalf("Expected %s to be an array, got %v", field, item[field])
	}
	return value
}

func decodeJSONBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}
	return decoded
}

// postTraining creates a training and returns the response, leaving the status
// assertion to the caller so rejection cases can use it too.
func postTraining(t *testing.T, app *fiber.App, token string, items []map[string]interface{}) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"title": "Hangboard",
		"items": items,
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return resp
}

func hangboardTestApp(t *testing.T, email string) (*fiber.App, string) {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	t.Cleanup(func() { testutil.CleanupTestDB(t, pool) })

	_, token := testutil.CreateTestUser(t, queries, email)

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})
	return app, token
}

func TestTrainingHandler_PerSetHangboardArraysRoundTrip(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat1@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{perSetHangboardItem()})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	created := decodeJSONBody(t, resp)
	trainingID := created["id"].(string)

	req := testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	fetched := decodeJSONBody(t, resp)

	items := fetched["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]interface{})

	if item["granularity"] != "set" {
		t.Errorf("Expected granularity to round-trip as set, got %v", item["granularity"])
	}
	if item["hand"] != "both" {
		t.Errorf("Expected hand to round-trip as both, got %v", item["hand"])
	}

	edges := decodeItemArray(t, item, "edge_sizes_mm")
	expectedEdges := []interface{}{float64(20), float64(20), float64(18), float64(15), float64(15), float64(12)}
	if !reflect.DeepEqual(edges, expectedEdges) {
		t.Errorf("Expected edge_sizes_mm %v, got %v", expectedEdges, edges)
	}

	loads := decodeItemArray(t, item, "loads")
	if len(loads) != 6 {
		t.Fatalf("Expected 6 loads, one per set and rep, got %d", len(loads))
	}
	lastLoad := loads[5].(map[string]interface{})
	if lastLoad["value"] != float64(15) {
		t.Errorf("Expected the last load to be 15, got %v", lastLoad["value"])
	}

	grips := decodeItemArray(t, item, "hand_positions")
	if len(grips) != 1 {
		t.Fatalf("Expected hand_positions to hold one array per hand, got %d", len(grips))
	}
	if len(grips[0].([]interface{})) != 6 {
		t.Errorf("Expected 6 grips, one per set and rep, got %d", len(grips[0].([]interface{})))
	}
}

func TestTrainingHandler_PerSetHangboardArraysSurviveUpdate(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat2@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{
		{
			"type":          "repeater",
			"cycles":        2,
			"reps":          3,
			"hand":          "both",
			"granularity":   "rep",
			"edge_sizes_mm": []interface{}{20, 18, 15},
		},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	created := decodeJSONBody(t, resp)
	trainingID := created["id"].(string)

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Per-set hangboard",
		"items": []map[string]interface{}{perSetHangboardItem()},
	})
	req := testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/trainings/%s", trainingID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	updated := decodeJSONBody(t, resp)

	item := updated["items"].([]interface{})[0].(map[string]interface{})
	if item["granularity"] != "set" {
		t.Errorf("Expected the update to widen the granularity to set, got %v", item["granularity"])
	}
	if len(decodeItemArray(t, item, "edge_sizes_mm")) != 6 {
		t.Errorf("Expected the update to widen edge_sizes_mm to 6 entries, got %v", item["edge_sizes_mm"])
	}
	if len(decodeItemArray(t, item, "loads")) != 6 {
		t.Errorf("Expected the update to widen loads to 6 entries, got %v", item["loads"])
	}
}

func TestTrainingHandler_SplitHandKeepsHandsApart(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat3@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{splitHandItem()})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	created := decodeJSONBody(t, resp)
	item := created["items"].([]interface{})[0].(map[string]interface{})

	loads := decodeItemArray(t, item, "loads")
	if len(loads) != 6 {
		t.Fatalf("Expected 6 right-hand loads, one per row, got %d", len(loads))
	}
	if loads[0].(map[string]interface{})["value"] != float64(10) {
		t.Errorf("Expected the first right load to be 10, got %v", loads[0])
	}

	leftLoads := decodeItemArray(t, item, "left_loads")
	if len(leftLoads) != 6 {
		t.Fatalf("Expected 6 left-hand loads, one per row, got %d", len(leftLoads))
	}
	if leftLoads[0].(map[string]interface{})["value"] != float64(5) {
		t.Errorf("Expected the first left load to be 5, got %v", leftLoads[0])
	}

	grips := decodeItemArray(t, item, "hand_positions")
	if len(grips) != 2 {
		t.Fatalf("Expected one grip array per hand, got %d", len(grips))
	}
	for hand, slots := range grips {
		if len(slots.([]interface{})) != 6 {
			t.Errorf("Expected 6 grips for hand %d, got %d", hand, len(slots.([]interface{})))
		}
	}
}

// The alternating mode is what the app's repeaters used to store as 'both'.
func TestTrainingHandler_AcceptsAlternateHand(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat4@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{
		{
			"type":        "repeater",
			"cycles":      3,
			"reps":        5,
			"hand":        "alternate",
			"granularity": "uniform",
			"loads":       []map[string]interface{}{{"value": 0, "unit": "max"}},
		},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	created := decodeJSONBody(t, resp)
	item := created["items"].([]interface{})[0].(map[string]interface{})
	if item["hand"] != "alternate" {
		t.Errorf("Expected hand to round-trip as alternate, got %v", item["hand"])
	}
}

func TestTrainingHandler_RejectsUnknownHandAndGranularity(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat5@test.com")

	cases := map[string]map[string]interface{}{
		"unknown hand": {
			"type": "repeater",
			"reps": 3,
			"hand": "both_hands",
		},
		"unknown granularity": {
			"type":        "repeater",
			"reps":        3,
			"granularity": "per_rep",
		},
	}

	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postTraining(t, app, token, []map[string]interface{}{item})
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
			}
		})
	}
}

// The granularity declares the row count, so an array of any other length is a
// client bug rather than a layout to be guessed at.
func TestTrainingHandler_RejectsArrayLengthMismatchingGranularity(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat6@test.com")

	cases := map[string]map[string]interface{}{
		"uniform item with per-rep loads": {
			"type":        "repeater",
			"cycles":      2,
			"reps":        3,
			"granularity": "uniform",
			"loads": []map[string]interface{}{
				{"value": 10, "unit": "kg"},
				{"value": 11, "unit": "kg"},
				{"value": 12, "unit": "kg"},
			},
		},
		"per-set item with per-rep edges": {
			"type":          "repeater",
			"cycles":        2,
			"reps":          3,
			"granularity":   "set",
			"edge_sizes_mm": []interface{}{20, 18, 15},
		},
		"interleaved split loads": {
			"type":        "repeater",
			"cycles":      1,
			"reps":        2,
			"hand":        "split",
			"granularity": "rep",
			"loads": []map[string]interface{}{
				{"value": 5, "unit": "kg"},
				{"value": 10, "unit": "kg"},
				{"value": 6, "unit": "kg"},
				{"value": 11, "unit": "kg"},
			},
		},
		"grips shorter than the row count": {
			"type":           "repeater",
			"cycles":         1,
			"reps":           3,
			"granularity":    "rep",
			"hand_positions": []interface{}{[]interface{}{"HC", "FC"}},
		},
		"more than two hands of grips": {
			"type":        "repeater",
			"cycles":      1,
			"reps":        1,
			"granularity": "uniform",
			"hand_positions": []interface{}{
				[]interface{}{"HC"},
				[]interface{}{"FC"},
				[]interface{}{"OC"},
			},
		},
	}

	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postTraining(t, app, token, []map[string]interface{}{item})
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
			}
		})
	}
}

func TestTrainingHandler_RejectsNonArrayConfigField(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat7@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{
		{
			"type":        "repeater",
			"reps":        3,
			"granularity": "rep",
			"loads":       map[string]interface{}{"value": 10, "unit": "kg"},
		},
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// An item that carries a configuration must say what layout it is written in.
// Inferring it from an array length is exactly what this format removes.
func TestTrainingHandler_RequiresGranularityOnConfiguredItem(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat8@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{
		{
			"type":  "repeater",
			"reps":  3,
			"loads": []map[string]interface{}{{"value": 10, "unit": "kg"}},
		},
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}

// An item with nothing configured has no layout to declare.
func TestTrainingHandler_AcceptsBareItemWithoutGranularity(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat9@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{
		{
			"type":             "repeater",
			"cycles":           2,
			"reps":             3,
			"worktime_seconds": 7,
			"rest_seconds":     3,
		},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}
}

// An empty array carries no values, the same as an absent one.
func TestTrainingHandler_AcceptsEmptyConfigArray(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat10@test.com")

	resp := postTraining(t, app, token, []map[string]interface{}{
		{
			"type":        "repeater",
			"cycles":      1,
			"reps":        3,
			"granularity": "rep",
			"loads":       []map[string]interface{}{},
		},
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}
}

// Only the modes that hang the hands separately carry a second hand.
func TestTrainingHandler_RejectsPerHandArraysOnSharedHandMode(t *testing.T) {
	app, token := hangboardTestApp(t, "hbformat11@test.com")

	cases := map[string]map[string]interface{}{
		"left_loads on both": {
			"type":        "repeater",
			"cycles":      1,
			"reps":        1,
			"hand":        "both",
			"granularity": "uniform",
			"loads":       []map[string]interface{}{{"value": 10, "unit": "kg"}},
			"left_loads":  []map[string]interface{}{{"value": 8, "unit": "kg"}},
		},
		"two grip arrays on right": {
			"type":        "repeater",
			"cycles":      1,
			"reps":        1,
			"hand":        "right",
			"granularity": "uniform",
			"hand_positions": []interface{}{
				[]interface{}{"HC"},
				[]interface{}{"OC"},
			},
		},
	}

	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postTraining(t, app, token, []map[string]interface{}{item})
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
			}
		})
	}
}
