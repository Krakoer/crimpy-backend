package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ExerciseHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewExerciseHandler(queries *db.Queries, pool *pgxpool.Pool) *ExerciseHandler {
	return &ExerciseHandler{queries: queries, pool: pool}
}

type CreateExerciseRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Comment     *string `json:"comment"`
	VideoLink   *string `json:"video_link"`
}

type UpdateExerciseRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Comment     *string `json:"comment"`
	VideoLink   *string `json:"video_link"`
}

type ExerciseResponse struct {
	ID          string        `json:"id"`
	CoachID     string        `json:"coach_id"`
	Name        string        `json:"name"`
	Description *string       `json:"description"`
	Comment     *string       `json:"comment"`
	VideoLink   *string       `json:"video_link"`
	IsFavorite  bool          `json:"is_favorite"`
	Tags        []TagResponse `json:"tags"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
}

type ExerciseListResponse struct {
	Exercises []ExerciseResponse `json:"exercises"`
	Total     int64              `json:"total"`
	Limit     int32              `json:"limit"`
	Offset    int32              `json:"offset"`
}

func exerciseToResponse(e db.Exercise, tags []db.Tag) ExerciseResponse {
	tagResponses := make([]TagResponse, 0, len(tags))
	for _, t := range tags {
		tagResponses = append(tagResponses, tagToResponse(t))
	}
	resp := ExerciseResponse{
		ID:         e.ID.String(),
		CoachID:    e.CoachID.String(),
		Name:       e.Name,
		IsFavorite: e.IsFavorite,
		Tags:       tagResponses,
		CreatedAt:  e.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:  e.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if e.Description.Valid {
		resp.Description = &e.Description.String
	}
	if e.Comment.Valid {
		resp.Comment = &e.Comment.String
	}
	if e.VideoLink.Valid {
		resp.VideoLink = &e.VideoLink.String
	}
	return resp
}

func requireValidatedCoach(c fiber.Ctx, queries *db.Queries) (pgtype.UUID, bool) {
	if !middleware.IsCoach(c) {
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach account required"})
		return pgtype.UUID{}, false
	}

	userID := middleware.GetUserID(c)

	var coachUUID pgtype.UUID
	if err := coachUUID.Scan(userID); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
		return pgtype.UUID{}, false
	}

	coach, err := queries.GetUserByID(c.Context(), coachUUID)
	if err != nil {
		c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve coach"})
		return pgtype.UUID{}, false
	}

	if !coach.CoachValidated {
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach account not yet validated by admin"})
		return pgtype.UUID{}, false
	}

	return coachUUID, true
}

// CreateExercise godoc
// @Summary Create an exercise
// @Description Create a new exercise in the coach's exercise library. Requires a validated coach account.
// @Tags Exercises
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateExerciseRequest true "Exercise data"
// @Success 201 {object} ExerciseResponse "Exercise created"
// @Failure 400 {object} map[string]string "Invalid request body"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/coach/exercises [post]
func (h *ExerciseHandler) CreateExercise(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var req CreateExerciseRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}

	params := db.CreateExerciseParams{
		CoachID: coachUUID,
		Name:    req.Name,
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Comment != nil {
		params.Comment = pgtype.Text{String: *req.Comment, Valid: true}
	}
	if req.VideoLink != nil {
		params.VideoLink = pgtype.Text{String: *req.VideoLink, Valid: true}
	}

	exercise, err := h.queries.CreateExercise(c.Context(), params)
	if err != nil {
		slog.Error("failed to create exercise", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create exercise"})
	}

	return c.Status(fiber.StatusCreated).JSON(exerciseToResponse(exercise, nil))
}

// GetExercises godoc
// @Summary List coach's exercises
// @Description Get paginated exercises in the authenticated coach's library, with optional name and tag filters.
// @Tags Exercises
// @Produce json
// @Security BearerAuth
// @Param name query string false "Filter by name (case-insensitive substring)"
// @Param tags query string false "Filter by tag IDs (comma-separated UUIDs)"
// @Param limit query int false "Results per page (1-100, default 20)"
// @Param offset query int false "Number of results to skip (default 0)"
// @Success 200 {object} ExerciseListResponse "Paginated list of exercises"
// @Failure 400 {object} map[string]string "Invalid tag ID"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Router /api/coach/exercises [get]
func (h *ExerciseHandler) GetExercises(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	nameFilter := c.Query("name", "")

	limit := int32(20)
	if v, err := strconv.ParseInt(c.Query("limit", "20"), 10, 32); err == nil && v > 0 && v <= 100 {
		limit = int32(v)
	}
	offset := int32(0)
	if v, err := strconv.ParseInt(c.Query("offset", "0"), 10, 32); err == nil && v >= 0 {
		offset = int32(v)
	}

	tagIDs := make([]pgtype.UUID, 0)
	if tagsParam := c.Query("tags", ""); tagsParam != "" {
		for _, part := range strings.Split(tagsParam, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			var tagUUID pgtype.UUID
			if err := tagUUID.Scan(part); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid tag ID: " + part})
			}
			tagIDs = append(tagIDs, tagUUID)
		}
	}

	searchParams := db.SearchCoachExercisesParams{
		CoachID:    coachUUID,
		NameFilter: nameFilter,
		TagIds:     tagIDs,
		Lim:        limit,
		Off:        offset,
	}
	exercises, err := h.queries.SearchCoachExercises(c.Context(), searchParams)
	if err != nil {
		slog.Error("failed to retrieve exercises", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercises"})
	}

	countParams := db.CountCoachExercisesParams{
		CoachID:    coachUUID,
		NameFilter: nameFilter,
		TagIds:     tagIDs,
	}
	total, err := h.queries.CountCoachExercises(c.Context(), countParams)
	if err != nil {
		slog.Error("failed to count exercises", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercises"})
	}

	exerciseIDs := make([]pgtype.UUID, 0, len(exercises))
	for _, e := range exercises {
		exerciseIDs = append(exerciseIDs, e.ID)
	}

	tagRows, err := h.queries.GetExerciseTagsByIDs(c.Context(), exerciseIDs)
	if err != nil {
		slog.Error("failed to retrieve exercise tags", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercises"})
	}

	tagsByExercise := buildTagsByExerciseFromIDRows(tagRows)

	result := make([]ExerciseResponse, 0, len(exercises))
	for _, e := range exercises {
		result = append(result, exerciseToResponse(e, tagsByExercise[e.ID.String()]))
	}

	return c.Status(fiber.StatusOK).JSON(ExerciseListResponse{
		Exercises: result,
		Total:     total,
		Limit:     limit,
		Offset:    offset,
	})
}

// GetExercise godoc
// @Summary Get an exercise
// @Description Get a specific exercise by ID. Only accessible by the owning coach.
// @Tags Exercises
// @Produce json
// @Security BearerAuth
// @Param id path string true "Exercise ID"
// @Success 200 {object} ExerciseResponse "Exercise details"
// @Failure 400 {object} map[string]string "Invalid exercise ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Exercise not found"
// @Router /api/coach/exercises/{id} [get]
func (h *ExerciseHandler) GetExercise(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var exerciseUUID pgtype.UUID
	if err := exerciseUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid exercise ID"})
	}

	exercise, err := h.queries.GetExercise(c.Context(), exerciseUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Exercise not found"})
		}
		slog.Error("failed to retrieve exercise", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercise"})
	}

	if exercise.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	tags, err := h.queries.GetExerciseTags(c.Context(), exerciseUUID)
	if err != nil {
		slog.Error("failed to retrieve exercise tags", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercise"})
	}

	return c.Status(fiber.StatusOK).JSON(exerciseToResponse(exercise, tags))
}

// UpdateExercise godoc
// @Summary Update an exercise
// @Description Update an exercise in the coach's library. Only the owning coach can update.
// @Tags Exercises
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Exercise ID"
// @Param request body UpdateExerciseRequest true "Updated exercise data"
// @Success 200 {object} ExerciseResponse "Updated exercise"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Exercise not found"
// @Router /api/coach/exercises/{id} [put]
func (h *ExerciseHandler) UpdateExercise(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var exerciseUUID pgtype.UUID
	if err := exerciseUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid exercise ID"})
	}

	existing, err := h.queries.GetExercise(c.Context(), exerciseUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Exercise not found"})
		}
		slog.Error("failed to retrieve exercise for update", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercise"})
	}

	if existing.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req UpdateExerciseRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}

	params := db.UpdateExerciseParams{
		ID:   exerciseUUID,
		Name: req.Name,
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Comment != nil {
		params.Comment = pgtype.Text{String: *req.Comment, Valid: true}
	}
	if req.VideoLink != nil {
		params.VideoLink = pgtype.Text{String: *req.VideoLink, Valid: true}
	}

	updated, err := h.queries.UpdateExercise(c.Context(), params)
	if err != nil {
		slog.Error("failed to update exercise", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update exercise"})
	}

	tags, err := h.queries.GetExerciseTags(c.Context(), exerciseUUID)
	if err != nil {
		slog.Error("failed to retrieve exercise tags", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update exercise"})
	}

	return c.Status(fiber.StatusOK).JSON(exerciseToResponse(updated, tags))
}

// DeleteExercise godoc
// @Summary Delete an exercise
// @Description Delete an exercise from the coach's library. Only the owning coach can delete.
// @Tags Exercises
// @Produce json
// @Security BearerAuth
// @Param id path string true "Exercise ID"
// @Success 200 {object} map[string]string "Exercise deleted"
// @Failure 400 {object} map[string]string "Invalid exercise ID"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Exercise not found"
// @Router /api/coach/exercises/{id} [delete]
func (h *ExerciseHandler) DeleteExercise(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var exerciseUUID pgtype.UUID
	if err := exerciseUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid exercise ID"})
	}

	exercise, err := h.queries.GetExercise(c.Context(), exerciseUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Exercise not found"})
		}
		slog.Error("failed to retrieve exercise for delete", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercise"})
	}

	if exercise.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	if err := h.queries.DeleteExercise(c.Context(), exerciseUUID); err != nil {
		slog.Error("failed to delete exercise", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete exercise"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Exercise deleted successfully"})
}

type SetExerciseFavoriteRequest struct {
	IsFavorite bool `json:"is_favorite"`
}

// SetExerciseFavorite godoc
// @Summary Set exercise favorite status
// @Description Mark or unmark an exercise as favorite. Only the owning coach can update.
// @Tags Exercises
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Exercise ID"
// @Param request body SetExerciseFavoriteRequest true "Favorite status"
// @Success 200 {object} ExerciseResponse "Updated exercise"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Not a validated coach or not owner"
// @Failure 404 {object} map[string]string "Exercise not found"
// @Router /api/coach/exercises/{id}/favorite [put]
func (h *ExerciseHandler) SetExerciseFavorite(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	var exerciseUUID pgtype.UUID
	if err := exerciseUUID.Scan(c.Params("id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid exercise ID"})
	}

	existing, err := h.queries.GetExercise(c.Context(), exerciseUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Exercise not found"})
		}
		slog.Error("failed to retrieve exercise for favorite update", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercise"})
	}

	if existing.CoachID.Bytes != coachUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var req SetExerciseFavoriteRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	updated, err := h.queries.SetExerciseFavorite(c.Context(), db.SetExerciseFavoriteParams{
		ID:         exerciseUUID,
		IsFavorite: req.IsFavorite,
	})
	if err != nil {
		slog.Error("failed to update exercise favorite", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update exercise"})
	}

	tags, err := h.queries.GetExerciseTags(c.Context(), exerciseUUID)
	if err != nil {
		slog.Error("failed to retrieve exercise tags", "exercise_id", exerciseUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update exercise"})
	}

	return c.Status(fiber.StatusOK).JSON(exerciseToResponse(updated, tags))
}

// GetFavoriteExercises godoc
// @Summary List favorite exercises
// @Description Get all exercises marked as favorite in the authenticated coach's library.
// @Tags Exercises
// @Produce json
// @Security BearerAuth
// @Success 200 {array} ExerciseResponse "List of favorite exercises"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Router /api/coach/exercises/favorites [get]
func (h *ExerciseHandler) GetFavoriteExercises(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}

	exercises, err := h.queries.GetCoachFavoriteExercises(c.Context(), coachUUID)
	if err != nil {
		slog.Error("failed to retrieve favorite exercises", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercises"})
	}

	tagRows, err := h.queries.GetCoachExerciseTags(c.Context(), coachUUID)
	if err != nil {
		slog.Error("failed to retrieve exercise tags", "coach_id", coachUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve exercises"})
	}

	tagsByExercise := buildTagsByExercise(tagRows)

	result := make([]ExerciseResponse, 0, len(exercises))
	for _, e := range exercises {
		result = append(result, exerciseToResponse(e, tagsByExercise[e.ID.String()]))
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

func buildTagsByExercise(rows []db.GetCoachExerciseTagsRow) map[string][]db.Tag {
	m := make(map[string][]db.Tag)
	for _, row := range rows {
		key := row.ExerciseID.String()
		m[key] = append(m[key], db.Tag{
			ID:        row.ID,
			Name:      row.Name,
			Color:     row.Color,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}
	return m
}

func buildTagsByExerciseFromIDRows(rows []db.GetExerciseTagsByIDsRow) map[string][]db.Tag {
	m := make(map[string][]db.Tag)
	for _, row := range rows {
		key := row.ExerciseID.String()
		m[key] = append(m[key], db.Tag{
			ID:        row.ID,
			Name:      row.Name,
			Color:     row.Color,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}
	return m
}
