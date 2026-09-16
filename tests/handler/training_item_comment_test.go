package handler_test

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestTrainingItemComment_RejectsOverLength(t *testing.T) {
	longComment := strings.Repeat("a", 2001)
	status, result := createTrainingWithItems(t, "itemcommentover@test.com", []map[string]interface{}{
		{"type": "exercise", "comment": longComment},
	})

	if status != fiber.StatusBadRequest {
		t.Fatalf("Expected 400 for a comment over the limit, got %d: %v", status, result)
	}
	if !strings.Contains(result["error"].(string), "2000") {
		t.Errorf("Expected the refusal to name the limit, got %v", result["error"])
	}
}

// The old cap silently truncated at 200 runes instead of refusing. A comment
// past that but under the new limit must round trip whole.
func TestTrainingItemComment_AllowsPastOldLimit(t *testing.T) {
	comment := strings.Repeat("a", 500)
	status, result := createTrainingWithItems(t, "itemcommentpastold@test.com", []map[string]interface{}{
		{"type": "exercise", "comment": comment},
	})

	if status != fiber.StatusCreated {
		t.Fatalf("Expected 201 for a comment past the old 200 limit, got %d: %v", status, result)
	}
	item := firstItem(t, result)
	if item["comment"] != comment {
		t.Errorf("Expected the comment to round trip in full, got %v", item["comment"])
	}
}

// The column is type agnostic: a coach comment matters most on the types that
// repeat, not only on exercise.
func TestTrainingItemComment_RoundTripsOnRepeaterHangboardRepAndEmom(t *testing.T) {
	for _, itemType := range []string{"repeater", "hangboard_rep", "emom"} {
		t.Run(itemType, func(t *testing.T) {
			item := map[string]interface{}{"type": itemType, "comment": "hold at the top"}
			if itemType == "emom" {
				item["interval_seconds"] = 60
			}
			status, result := createTrainingWithItems(t, "itemcomment-"+itemType+"@test.com", []map[string]interface{}{item})

			if status != fiber.StatusCreated {
				t.Fatalf("Expected 201 for a %s carrying a comment, got %d: %v", itemType, status, result)
			}
			got := firstItem(t, result)
			if got["comment"] != "hold at the top" {
				t.Errorf("Expected the comment to round trip on a %s, got %v", itemType, got["comment"])
			}
		})
	}
}
