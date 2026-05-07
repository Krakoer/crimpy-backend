package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TagHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewTagHandler(queries *db.Queries, pool *pgxpool.Pool) *TagHandler {
	return &TagHandler{queries: queries, pool: pool}
}

type TagResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CreateTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type UpdateTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func tagToResponse(t db.Tag) TagResponse {
	return TagResponse{
		ID:        t.ID.String(),
		Name:      t.Name,
		Color:     t.Color,
		CreatedAt: t.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt: t.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// CreateTag godoc
// @Summary Create a tag
// @Description Create a new tag in the coach's tag library. Requires a validated coach account.
// @Tags Tags
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateTagRequest true "Tag data"
// @Success 201 {object} TagResponse "Tag created"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/coach/tags [post]
func (h *TagHandler) CreateTag(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var req CreateTagRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}
	if req.Color == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Color is required"})
	}

	tag, err := h.queries.CreateTag(context.Background(), db.CreateTagParams{
		CoachID: coachUUID,
		Name:    req.Name,
		Color:   req.Color,
	})
	if err != nil {
		slog.Error("failed to create tag", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create tag"})
	}

	return c.Status(fiber.StatusCreated).JSON(tagToResponse(tag))
}

// GetTags godoc
// @Summary List coach's tags
// @Description Get all tags in the authenticated coach's library.
// @Tags Tags
// @Produce json
// @Security BearerAuth
// @Success 200 {array} TagResponse "List of tags"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Router /api/coach/tags [get]
func (h *TagHandler) GetTags(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	tags, err := h.queries.GetCoachTags(context.Background(), coachUUID)
	if err != nil {
		slog.Error("failed to retrieve tags", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve tags"})
	}

	result := make([]TagResponse, 0, len(tags))
	for _, t := range tags {
		result = append(result, tagToResponse(t))
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

// UpdateTag godoc
// @Summary Update a tag
// @Description Update a tag in the coach's library. Only the owning coach can update.
// @Tags Tags
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Tag ID"
// @Param request body UpdateTagRequest true "Updated tag data"
// @Success 200 {object} TagResponse "Updated tag"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Tag not found"
// @Router /api/coach/tags/{id} [put]
func (h *TagHandler) UpdateTag(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var tagUUID pgtype.UUID
	if err := tagUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid tag ID"})
	}

	existing, err := h.queries.GetTag(context.Background(), tagUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tag not found"})
		}
		slog.Error("failed to retrieve tag for update", "tag_id", tagUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve tag"})
	}

	if existing.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req UpdateTagRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}
	if req.Color == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Color is required"})
	}

	updated, err := h.queries.UpdateTag(context.Background(), db.UpdateTagParams{
		ID:    tagUUID,
		Name:  req.Name,
		Color: req.Color,
	})
	if err != nil {
		slog.Error("failed to update tag", "tag_id", tagUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update tag"})
	}

	return c.Status(fiber.StatusOK).JSON(tagToResponse(updated))
}

// DeleteTag godoc
// @Summary Delete a tag
// @Description Delete a tag from the coach's library. Only the owning coach can delete. Automatically unassigns from all exercises.
// @Tags Tags
// @Produce json
// @Security BearerAuth
// @Param id path string true "Tag ID"
// @Success 200 {object} map[string]string "Tag deleted"
// @Failure 400 {object} map[string]string "Invalid tag ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Tag not found"
// @Router /api/coach/tags/{id} [delete]
func (h *TagHandler) DeleteTag(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var tagUUID pgtype.UUID
	if err := tagUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid tag ID"})
	}

	existing, err := h.queries.GetTag(context.Background(), tagUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tag not found"})
		}
		slog.Error("failed to retrieve tag for delete", "tag_id", tagUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve tag"})
	}

	if existing.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteTag(context.Background(), tagUUID); err != nil {
		slog.Error("failed to delete tag", "tag_id", tagUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete tag"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Tag deleted successfully"})
}

// AssignTag godoc
// @Summary Assign a tag to an exercise
// @Description Assign a tag to an exercise. Both must belong to the authenticated coach.
// @Tags Tags
// @Produce json
// @Security BearerAuth
// @Param exercise_id path string true "Exercise ID"
// @Param tag_id path string true "Tag ID"
// @Success 200 {object} map[string]string "Tag assigned"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Exercise or tag not found"
// @Router /api/coach/exercises/{exercise_id}/tags/{tag_id} [post]
func (h *TagHandler) AssignTag(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	exerciseUUID, tagUUID, ok := parseExerciseAndTagIDs(c, h.queries, coachUUID)
	if !ok {
		return nil
	}

	if err := h.queries.AssignTagToExercise(context.Background(), db.AssignTagToExerciseParams{
		ExerciseID: exerciseUUID,
		TagID:      tagUUID,
	}); err != nil {
		slog.Error("failed to assign tag", "exercise_id", exerciseUUID.String(), "tag_id", tagUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to assign tag"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Tag assigned successfully"})
}

// UnassignTag godoc
// @Summary Unassign a tag from an exercise
// @Description Remove a tag from an exercise. Both must belong to the authenticated coach.
// @Tags Tags
// @Produce json
// @Security BearerAuth
// @Param exercise_id path string true "Exercise ID"
// @Param tag_id path string true "Tag ID"
// @Success 200 {object} map[string]string "Tag unassigned"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Exercise or tag not found"
// @Router /api/coach/exercises/{exercise_id}/tags/{tag_id} [delete]
func (h *TagHandler) UnassignTag(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	exerciseUUID, tagUUID, ok := parseExerciseAndTagIDs(c, h.queries, coachUUID)
	if !ok {
		return nil
	}

	if err := h.queries.UnassignTagFromExercise(context.Background(), db.UnassignTagFromExerciseParams{
		ExerciseID: exerciseUUID,
		TagID:      tagUUID,
	}); err != nil {
		slog.Error("failed to unassign tag", "exercise_id", exerciseUUID.String(), "tag_id", tagUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to unassign tag"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Tag unassigned successfully"})
}

func parseExerciseAndTagIDs(c fiber.Ctx, queries *db.Queries, coachUUID pgtype.UUID) (pgtype.UUID, pgtype.UUID, bool) {
	var exerciseUUID pgtype.UUID
	if err := exerciseUUID.Scan(c.Params("exercise_id")); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid exercise ID"})
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	var tagUUID pgtype.UUID
	if err := tagUUID.Scan(c.Params("tag_id")); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid tag ID"})
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	exercise, err := queries.GetExercise(context.Background(), exerciseUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Exercise not found"})
		} else {
			c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercise"})
		}
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if exercise.CoachID.Bytes != coachUUID.Bytes {
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	tag, err := queries.GetTag(context.Background(), tagUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Tag not found"})
		} else {
			c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve tag"})
		}
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if tag.CoachID.Bytes != coachUUID.Bytes {
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
		return pgtype.UUID{}, pgtype.UUID{}, false
	}

	return exerciseUUID, tagUUID, true
}
