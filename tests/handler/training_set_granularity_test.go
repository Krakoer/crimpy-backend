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

// A hangboard item configured per set and rep carries cycles * reps entries in
// its arrays, indexed set * reps + rep, instead of one entry per rep.
func perSetHangboardItem() map[string]interface{} {
	return map[string]interface{}{
		"type":               "repeater",
		"cycles":             2,
		"reps":               3,
		"worktime_seconds":   7,
		"rest_seconds":       3,
		"cycle_rest_seconds": 120,
		"hand":               "both",
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

// A split-hand item carries one configuration row per set and rep, with both
// hands interleaved into loads the way the coach portal writes them.
func splitHandInterleavedItem() map[string]interface{} {
	loads := make([]map[string]interface{}, 0, 12)
	for i := 0; i < 12; i++ {
		loads = append(loads, map[string]interface{}{"value": i, "unit": "kg"})
	}
	return map[string]interface{}{
		"type":               "repeater",
		"cycles":             2,
		"reps":               3,
		"worktime_seconds":   7,
		"rest_seconds":       3,
		"cycle_rest_seconds": 120,
		"hand":               "split",
		"edge_sizes_mm":      []interface{}{20, 20, 18, 15, 15, 12},
		"loads":              loads,
		"hand_positions": []interface{}{
			[]interface{}{"HC", "HC", "FC", "FC", "OC", "OC"},
			[]interface{}{"HC", "FC", "FC", "OC", "OC", "HC"},
		},
	}
}

// The mobile app writes split-hand loads as two arrays instead of interleaving.
func splitHandSeparateLoadsItem() map[string]interface{} {
	return map[string]interface{}{
		"type":             "repeater",
		"cycles":           2,
		"reps":             3,
		"worktime_seconds": 7,
		"hand":             "split",
		"loads": []map[string]interface{}{
			{"value": 10, "unit": "kg"},
			{"value": 11, "unit": "kg"},
			{"value": 12, "unit": "kg"},
		},
		"left_loads": []map[string]interface{}{
			{"value": 5, "unit": "kg"},
			{"value": 6, "unit": "kg"},
			{"value": 7, "unit": "kg"},
		},
		"hand_positions": []interface{}{"HC", "FC", "OC"},
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

func TestTrainingHandler_PerSetHangboardArraysRoundTrip(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "perset1@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Per-set hangboard",
		"items": []map[string]interface{}{perSetHangboardItem()},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	created := decodeJSONBody(t, resp)
	trainingID := created["id"].(string)

	req = testutil.NewRequest(http.MethodGet, fmt.Sprintf("/api/trainings/%s", trainingID), nil)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
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
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "perset2@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Per-rep hangboard",
		"items": []map[string]interface{}{
			{
				"type":          "repeater",
				"cycles":        2,
				"reps":          3,
				"hand":          "both",
				"edge_sizes_mm": []interface{}{20, 18, 15},
			},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	created := decodeJSONBody(t, resp)
	trainingID := created["id"].(string)

	body, _ = json.Marshal(map[string]interface{}{
		"title": "Per-set hangboard",
		"items": []map[string]interface{}{perSetHangboardItem()},
	})
	req = testutil.NewJSONRequest(http.MethodPut, fmt.Sprintf("/api/trainings/%s", trainingID), body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected %d, got %d", fiber.StatusOK, resp.StatusCode)
	}

	updated := decodeJSONBody(t, resp)

	item := updated["items"].([]interface{})[0].(map[string]interface{})
	if len(decodeItemArray(t, item, "edge_sizes_mm")) != 6 {
		t.Errorf("Expected the update to widen edge_sizes_mm to 6 entries, got %v", item["edge_sizes_mm"])
	}
	if len(decodeItemArray(t, item, "loads")) != 6 {
		t.Errorf("Expected the update to widen loads to 6 entries, got %v", item["loads"])
	}
}

func TestTrainingHandler_SplitHandLayoutsRoundTrip(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "perset3@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Split hangboard",
		"items": []map[string]interface{}{
			splitHandInterleavedItem(),
			splitHandSeparateLoadsItem(),
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("Expected %d, got %d", fiber.StatusCreated, resp.StatusCode)
	}

	created := decodeJSONBody(t, resp)
	items := created["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(items))
	}

	interleaved := items[0].(map[string]interface{})
	loads := decodeItemArray(t, interleaved, "loads")
	if len(loads) != 12 {
		t.Fatalf("Expected 12 loads, two hands per set and rep, got %d", len(loads))
	}
	if loads[0].(map[string]interface{})["value"] != float64(0) {
		t.Errorf("Expected the left load of the first rep at index 0, got %v", loads[0])
	}
	if loads[1].(map[string]interface{})["value"] != float64(1) {
		t.Errorf("Expected the right load of the first rep at index 1, got %v", loads[1])
	}
	if len(decodeItemArray(t, interleaved, "edge_sizes_mm")) != 6 {
		t.Errorf("Expected 6 edge sizes, one per set and rep, got %v", interleaved["edge_sizes_mm"])
	}
	grips := decodeItemArray(t, interleaved, "hand_positions")
	if len(grips) != 2 {
		t.Fatalf("Expected hand_positions to hold one array per hand, got %d", len(grips))
	}
	if interleaved["left_loads"] != nil {
		t.Errorf("Expected left_loads to stay unset on the interleaved layout, got %v", interleaved["left_loads"])
	}

	separate := items[1].(map[string]interface{})
	if len(decodeItemArray(t, separate, "loads")) != 3 {
		t.Errorf("Expected 3 right-hand loads, got %v", separate["loads"])
	}
	leftLoads := decodeItemArray(t, separate, "left_loads")
	if len(leftLoads) != 3 {
		t.Fatalf("Expected 3 left-hand loads, got %d", len(leftLoads))
	}
	if leftLoads[0].(map[string]interface{})["value"] != float64(5) {
		t.Errorf("Expected the first left load to be 5, got %v", leftLoads[0])
	}
	if len(decodeItemArray(t, separate, "hand_positions")) != 3 {
		t.Errorf("Expected the flat hand_positions form to survive, got %v", separate["hand_positions"])
	}
}

func TestTrainingHandler_RejectsNonArrayConfigField(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")

	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)

	_, token := testutil.CreateTestUser(t, queries, "perset4@test.com")

	app := testutil.SetupFiberApp(testutil.HandlerConfig{
		TrainingHandler: handler.NewTrainingHandler(queries, pool),
	})

	body, _ := json.Marshal(map[string]interface{}{
		"title": "Bad loads",
		"items": []map[string]interface{}{
			{
				"type":  "repeater",
				"reps":  3,
				"loads": map[string]interface{}{"value": 10, "unit": "kg"},
			},
		},
	})
	req := testutil.NewJSONRequest(http.MethodPost, "/api/trainings", body)
	req.Header.Set("Authorization", testutil.GetAuthHeader(token))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("Expected %d, got %d", fiber.StatusBadRequest, resp.StatusCode)
	}
}
