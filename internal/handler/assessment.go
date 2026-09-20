package handler

import (
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type AssessmentHandler struct {
	queries *db.Queries
}

type CreateAssessmentRequest struct {
	SessionID    string   `json:"session_id"`
	AssessmentID string   `json:"assessment_id"`
	RightValue   *float32 `json:"right_value,omitempty"`
	LeftValue    *float32 `json:"left_value,omitempty"`
	GripPosition *int32   `json:"grip_position,omitempty"`
}

func NewAssessmentHandler(queries *db.Queries) *AssessmentHandler {
	return &AssessmentHandler{queries: queries}
}

func (h *AssessmentHandler) ownedAssessment() ownedResource[db.Assessment] {
	return ownedResource[db.Assessment]{
		label: "Assessment",
		fetch: h.queries.GetAssessment,
		owner: func(a db.Assessment) pgtype.UUID { return a.UserID },
	}
}

// CreateAssessment godoc
// @Summary Create an assessment linked to an existing session
// @Description Create a new assessment result for the authenticated user, linked to an existing session they own
// @Tags Assessment
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateAssessmentRequest true "Assessment details"
// @Success 201 {object} AssessmentResponse "Assessment created"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/assessments [post]
func (h *AssessmentHandler) CreateAssessment(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req CreateAssessmentRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.SessionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "session_id is required"})
	}

	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(req.SessionID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session_id"})
	}

	session, err := h.queries.GetSession(c.Context(), sessionUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	if session.UserID.Bytes != userUUID.Bytes {
		slog.Warn("access denied to session for assessment", "user_id", userID, "session_id", req.SessionID)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
	}

	var rightValue, leftValue pgtype.Float4
	var gripPosition pgtype.Int4

	if req.RightValue != nil {
		rightValue.Float32 = *req.RightValue
		rightValue.Valid = true
	}
	if req.LeftValue != nil {
		leftValue.Float32 = *req.LeftValue
		leftValue.Valid = true
	}
	if req.GripPosition != nil {
		gripPosition.Int32 = *req.GripPosition
		gripPosition.Valid = true
	}

	assessmentUUID, ok := requireRecordableAssessment(c, h.queries, req.AssessmentID, userUUID)
	if !ok {
		return nil
	}

	assessment, err := h.queries.CreateAssessment(c.Context(), db.CreateAssessmentParams{
		UserID:       userUUID,
		AssessmentID: assessmentUUID,
		RightValue:   rightValue,
		LeftValue:    leftValue,
		SessionID:    session.ID,
		GripPosition: gripPosition,
	})
	if err != nil {
		slog.Error("failed to create assessment", "user_id", userID, "session_id", req.SessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
	}

	// The definition is loaded rather than echoed from the request, so the
	// response carries the same shape as every other assessment read path
	// instead of the raw row, whose Go field names would leak as PascalCase.
	definition, err := h.queries.GetAssessmentDefinition(c.Context(), assessmentUUID)
	if err != nil {
		slog.Error("failed to retrieve assessment definition", "assessment_id", assessmentUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
	}

	return c.Status(fiber.StatusCreated).JSON(assessmentToResponse(assessmentResult{
		Assessment:         assessment,
		Label:              definition.Label,
		Unit:               definition.Unit,
		PerHand:            definition.PerHand,
		BodyweightRelative: definition.BodyweightRelative,
		TrainingID:         definition.TrainingID,
	}))
}

// GetAssessments godoc
// @Summary Get all assessments for the current user
// @Description Retrieve all assessment results for the authenticated user, ordered by session date descending. Each row carries the weigh-in a bodyweight relative score is divided by, the last one taken at or before the session, with the date it was taken so a reader can tell a fresh denominator from a stale one.
// @Tags Assessment
// @Produce json
// @Security BearerAuth
// @Success 200 {array} AssessmentListItem "List of assessments"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/assessments [get]
func (h *AssessmentHandler) GetAssessments(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	assessments, err := h.queries.GetUserAssessments(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve assessments", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
	}

	if assessments == nil {
		assessments = []db.GetUserAssessmentsRow{}
	}
	return c.JSON(assessmentRowsToListItems(assessments))
}

// DeleteAssessment godoc
// @Summary Delete an assessment
// @Description Delete an assessment. User must own the associated session.
// @Tags Assessment
// @Produce json
// @Security BearerAuth
// @Param id path string true "Assessment ID (UUID)"
// @Success 200 {object} map[string]string "Deleted successfully"
// @Failure 400 {object} map[string]string "Invalid ID"
// @Failure 404 {object} map[string]string "Not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/assessments/{id} [delete]
func (h *AssessmentHandler) DeleteAssessment(c fiber.Ctx) error {
	_, id, ok := h.ownedAssessment().require(c)
	if !ok {
		return nil
	}

	if err := h.queries.DeleteAssessment(c.Context(), id); err != nil {
		slog.Error("failed to delete assessment", "assessment_id", id.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete assessment"})
	}

	return c.JSON(fiber.Map{"message": "Assessment deleted successfully"})
}

// requireRecordableAssessment resolves the assessment a result names and refuses
// it unless the caller may record against it: one Crimpy ships, their own, or a
// coach's prescribed to them by a program. An unknown id and a foreign one are
// refused alike, so the endpoint does not report which assessments exist.
func requireRecordableAssessment(c fiber.Ctx, queries *db.Queries, assessmentID string, userID pgtype.UUID) (pgtype.UUID, bool) {
	var assessmentUUID pgtype.UUID
	if assessmentID == "" {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "assessment_id is required"})
		return assessmentUUID, false
	}
	if err := assessmentUUID.Scan(assessmentID); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid assessment_id"})
		return assessmentUUID, false
	}
	// The same query the recordable listing is served from, asked about one
	// assessment rather than all of them, so an assessment the athlete was
	// offered is never refused here.
	rows, err := queries.GetRecordableAssessmentDefinitions(c.Context(), db.GetRecordableAssessmentDefinitionsParams{
		AssessmentID: assessmentUUID,
		UserID:       userID,
	})
	if err != nil {
		slog.Error("failed to check assessment", "assessment_id", assessmentID, "error", err)
		c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
		return assessmentUUID, false
	}
	if len(rows) == 0 {
		slog.Warn("assessment not recordable", "user_id", userID.String(), "assessment_id", assessmentID)
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Unknown assessment_id"})
		return assessmentUUID, false
	}
	return assessmentUUID, true
}

// AssessmentSnapshotItem is one assessment as it stood on a date: the last value
// measured for it at or before then, per grip and per hand, with the definition
// that names and formats it.
//
// The date each hand was measured travels with the value, because the snapshot
// carries a result forward until a newer one replaces it. Two dates compared
// side by side would otherwise read a value last measured months before the
// second date as an unchanged result, which is not what it is.
type AssessmentSnapshotItem struct {
	AssessmentID string `json:"assessment_id"`
	Label        string `json:"label"`
	Unit         string `json:"unit" enums:"kilograms,seconds,repetitions"`
	PerHand      bool   `json:"per_hand"`
	// Whether the result reads as a ratio to the bodyweight it was pulled at
	// rather than as an absolute load. Display only, see the column comment.
	BodyweightRelative bool `json:"bodyweight_relative"`
	// The training the assessment is run from, absent on the ones Crimpy ships.
	TrainingID      *string  `json:"training_id,omitempty"`
	GripPosition    int32    `json:"grip_position"`
	RightValue      *float32 `json:"right_value,omitempty"`
	RightMeasuredAt *string  `json:"right_measured_at,omitempty"`
	// The weight in effect when this hand was measured, which is the denominator
	// its ratio has to be read against. Not the snapshot's bodyweight_kg: a value
	// carried forward from an earlier session was pulled at the weight of that
	// day, and dividing it by a later one gives a number the athlete never
	// achieved. Absent when no weigh-in precedes the measurement.
	RightBodyweightKg *float32 `json:"right_bodyweight_kg,omitempty"`
	// When that weigh-in was taken. A denominator is only worth dividing by while
	// it is near the result it divides, and a weight on its own cannot say how
	// near it was: the last weigh-in at or before a result can be the same
	// morning or months earlier. Absent exactly when the weight is.
	RightBodyweightMeasuredAt *string  `json:"right_bodyweight_measured_at,omitempty"`
	LeftValue                 *float32 `json:"left_value,omitempty"`
	LeftMeasuredAt            *string  `json:"left_measured_at,omitempty"`
	LeftBodyweightKg          *float32 `json:"left_bodyweight_kg,omitempty"`
	LeftBodyweightMeasuredAt  *string  `json:"left_bodyweight_measured_at,omitempty"`
}

// AssessmentSnapshotResponse is what an athlete had measured as of a date. The
// bodyweight is the one in effect on that date, and is what the athlete weighed
// then rather than what any particular result was pulled at: the weight a ratio
// is read against travels with the value, on the item. Absent when nothing had
// been recorded by then, which a reader has to say out loud rather than divide
// by.
type AssessmentSnapshotResponse struct {
	Date         string                   `json:"date"`
	BodyweightKg *float32                 `json:"bodyweight_kg,omitempty"`
	Results      []AssessmentSnapshotItem `json:"results"`
}

// parseAssessmentSnapshotDate reads the date a snapshot is asked as of. A plain
// day means the end of it, so a session recorded that afternoon is included:
// a coach picking "14 March" means the state of things that evening, not at
// midnight when nothing had happened yet.
func parseAssessmentSnapshotDate(raw string) (pgtype.Timestamptz, error) {
	var asOf pgtype.Timestamptz
	if raw == "" {
		return asOf, errors.New("date is required")
	}
	if day, err := time.Parse(time.DateOnly, raw); err == nil {
		endOfDay := day.UTC().Add(24*time.Hour - time.Nanosecond)
		return pgtype.Timestamptz{Time: endOfDay, Valid: true}, nil
	}
	// A query string decoder reads a raw "+" as a space, so an RFC3339 instant
	// with a numeric offset arrives here as "2026-03-02T10:00:00 02:00" unless
	// the caller percent encoded it. A valid instant holds no space of its own,
	// so putting the plus back cannot turn one value into another, and it means
	// the endpoint accepts the format its documentation promises rather than
	// only the ones that survive the wire.
	instant, err := time.Parse(time.RFC3339, strings.ReplaceAll(raw, " ", "+"))
	if err != nil {
		return asOf, errors.New("date must be a YYYY-MM-DD day or an RFC3339 instant")
	}
	return pgtype.Timestamptz{Time: instant.UTC(), Valid: true}, nil
}

// measuredBodyweight reads the weight a value was pulled at. The query answers
// zero when no weigh-in precedes the measurement, which is not a weight: the
// column's own check keeps a real one strictly above zero. Turned into an absent
// field here so the sentinel never leaves this package.
func measuredBodyweight(weightKg float32) *float32 {
	if weightKg <= 0 {
		return nil
	}
	return &weightKg
}

// bodyweightMeasuredAt reads when the denominator beside it was weighed. Tied to
// the weight rather than answered on its own, so a caller never receives a date
// for a weight it was not given and cannot read the two as describing different
// weigh-ins.
func bodyweightMeasuredAt(weightKg *float32, measuredAt pgtype.Timestamptz) *string {
	if weightKg == nil || !measuredAt.Valid {
		return nil
	}
	formatted := measuredAt.Time.UTC().Format(time.RFC3339)
	return &formatted
}

// assessmentSnapshotAt answers with the athlete's results as of the date the
// caller asked for, having already established they may read them. Shared by
// the athlete's own endpoint and the coach's, so the two cannot drift in what
// they mean by a date or in the shape they return.
func assessmentSnapshotAt(c fiber.Ctx, queries *db.Queries, userUUID pgtype.UUID) error {
	asOf, err := parseAssessmentSnapshotDate(c.Query("date"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	rows, err := queries.GetUserAssessmentValuesAtDate(c.Context(), db.GetUserAssessmentValuesAtDateParams{
		UserID: userUUID,
		AsOf:   asOf,
	})
	if err != nil {
		slog.Error("failed to retrieve assessment snapshot", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
	}

	response := AssessmentSnapshotResponse{
		Date:    asOf.Time.UTC().Format(time.RFC3339),
		Results: make([]AssessmentSnapshotItem, 0, len(rows)),
	}
	for _, row := range rows {
		item := AssessmentSnapshotItem{
			AssessmentID:       row.AssessmentID.String(),
			Label:              row.Label,
			Unit:               row.Unit,
			PerHand:            row.PerHand,
			BodyweightRelative: row.BodyweightRelative,
			GripPosition:       row.GripPosition,
		}
		if row.TrainingID.Valid {
			trainingID := row.TrainingID.String()
			item.TrainingID = &trainingID
		}
		if row.RightValue.Valid {
			item.RightValue = &row.RightValue.Float32
		}
		if row.RightMeasuredAt.Valid {
			measured := row.RightMeasuredAt.Time.UTC().Format(time.RFC3339)
			item.RightMeasuredAt = &measured
		}
		item.RightBodyweightKg = measuredBodyweight(row.RightBodyweightKg)
		item.RightBodyweightMeasuredAt = bodyweightMeasuredAt(item.RightBodyweightKg, row.RightBodyweightMeasuredAt)
		if row.LeftValue.Valid {
			item.LeftValue = &row.LeftValue.Float32
		}
		if row.LeftMeasuredAt.Valid {
			measured := row.LeftMeasuredAt.Time.UTC().Format(time.RFC3339)
			item.LeftMeasuredAt = &measured
		}
		item.LeftBodyweightKg = measuredBodyweight(row.LeftBodyweightKg)
		item.LeftBodyweightMeasuredAt = bodyweightMeasuredAt(item.LeftBodyweightKg, row.LeftBodyweightMeasuredAt)
		response.Results = append(response.Results, item)
	}

	bodyweight, err := queries.GetUserBodyweightAtDate(c.Context(), db.GetUserBodyweightAtDateParams{
		UserID: userUUID,
		AsOf:   asOf,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("failed to retrieve bodyweight at date", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
	}
	if err == nil {
		response.BodyweightKg = &bodyweight.WeightKg
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

// GetMyAssessmentSnapshot godoc
// @Summary My assessment results as of a date
// @Description The last value measured for each assessment, grip and hand at or before the given date. Each hand carries the date it was measured, the bodyweight in effect then, which is the denominator a bodyweight relative score is read against, and the date that weigh-in was taken, which says how stale the denominator is; the snapshot's own bodyweight_kg is what the athlete weighed on the date asked for. Reading two dates gives the two sides of a comparison.
// @Tags Assessment
// @Produce json
// @Security BearerAuth
// @Param date query string true "The day to read the results as of, YYYY-MM-DD or RFC3339"
// @Success 200 {object} AssessmentSnapshotResponse "The results as of that date"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/assessments/at [get]
func (h *AssessmentHandler) GetMyAssessmentSnapshot(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}
	return assessmentSnapshotAt(c, h.queries, userUUID)
}
