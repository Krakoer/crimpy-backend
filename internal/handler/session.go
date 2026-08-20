package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// How a session came to exist. Set by the client from the code path that
// produced it, never picked by a user, and mirrored by the sessions_origin_check
// constraint.
const (
	originPlayed = "played"
	originLogged = "logged"
)

// activityCount bounds the activity label shared with the app: 0 hangboard,
// 1 climbing, 2 stretching, 3 workout, 4 other. It mirrors the
// sessions_activity_check constraint in schema/schema.sql.
const activityCount = int32(5)

func isValidActivity(activity int32) bool {
	return activity >= 0 && activity < activityCount
}

type SessionHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

// parseSessionDate accepts both precisions the app sends, and always returns UTC.
func parseSessionDate(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	return t.UTC(), err
}

// optionalUUID converts an omitted or empty id into a null UUID, so a session
// with no template links stores nulls rather than failing to parse.
func optionalUUID(raw *string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if raw == nil || *raw == "" {
		return id, nil
	}
	if err := id.Scan(*raw); err != nil {
		return id, err
	}
	return id, nil
}

// authorizeSessionLinks checks the template links a session claims to have been
// played from. A training is usable when the caller owns it or when a program
// assigned to them prescribes it, and a program session when it sits in such a
// program. An unknown id is refused exactly like a foreign one, so the endpoint
// cannot be used to probe which ids exist.
func (h *SessionHandler) authorizeSessionLinks(c fiber.Ctx, userUUID, trainingID, programSessionID pgtype.UUID) bool {
	userID := userUUID.String()

	if trainingID.Valid {
		count, err := h.queries.CountAccessibleTraining(c.Context(), db.CountAccessibleTrainingParams{
			TrainingID: trainingID,
			UserID:     userUUID,
		})
		if err != nil {
			slog.Error("failed to authorize session training", "user_id", userID, "training_id", trainingID.String(), "error", err)
			c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
			return false
		}
		if count == 0 {
			slog.Warn("access denied", "resource", "Training", "user_id", userID, "id", trainingID.String())
			c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
			return false
		}
	}

	if programSessionID.Valid {
		count, err := h.queries.CountAccessibleProgramSession(c.Context(), db.CountAccessibleProgramSessionParams{
			ProgramSessionID: programSessionID,
			UserID:           userUUID,
		})
		if err != nil {
			slog.Error("failed to authorize program session", "user_id", userID, "program_session_id", programSessionID.String(), "error", err)
			c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
			return false
		}
		if count == 0 {
			slog.Warn("access denied", "resource", "Program session", "user_id", userID, "id", programSessionID.String())
			c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
			return false
		}
	}

	return true
}

func NewSessionHandler(queries *db.Queries, pool *pgxpool.Pool) *SessionHandler {
	return &SessionHandler{
		queries: queries,
		pool:    pool,
	}
}

func (h *SessionHandler) ownedSession() ownedResource[db.Session] {
	return ownedResource[db.Session]{
		label:      "Session",
		fetch:      h.queries.GetSession,
		owner:      func(s db.Session) pgtype.UUID { return s.UserID },
		allowAdmin: true,
	}
}

type CreateSessionRequest struct {
	Name              string              `json:"name"`
	Notes             string              `json:"notes"`
	Date              string              `json:"date,omitempty"`
	IsAssessment      bool                `json:"is_assessment"`
	Activity          int32               `json:"activity"`
	Origin            string              `json:"origin,omitempty"`
	TrainingID        *string             `json:"training_id,omitempty"`
	ProgramSessionID  *string             `json:"program_session_id,omitempty"`
	Duration          int32               `json:"duration"`
	RepeaterSets      *int32              `json:"repeater_sets,omitempty"`
	RepeaterReps      *int32              `json:"repeater_reps,omitempty"`
	RepeaterWorkTime  *int32              `json:"repeater_work_time,omitempty"`
	RepeaterRestTime  *int32              `json:"repeater_rest_time,omitempty"`
	RepeaterSetRest   *int32              `json:"repeater_set_rest,omitempty"`
	RepeaterSplitHand *bool               `json:"repeater_split_hand,omitempty"`
	RepDatas          []RepDataRequest    `json:"rep_datas,omitempty"`
	Assessments       []AssessmentRequest `json:"assessments,omitempty"`
}

type RepDataRequest struct {
	AverageWeight float32 `json:"average_weight"`
	IsRest        bool    `json:"is_rest"`
	RightHand     bool    `json:"right_hand"`
	Duration      int32   `json:"duration"`
	TargetWeight  float32 `json:"target_weight"`
	Index         int32   `json:"index"`
	GripPosition  int32   `json:"grip_position"`
	EdgeSizeMm    *int32  `json:"edge_size_mm,omitempty"`
}

type AssessmentRequest struct {
	Type         int32    `json:"type"`
	RightValue   *float32 `json:"right_value,omitempty"`
	LeftValue    *float32 `json:"left_value,omitempty"`
	GripPosition *int32   `json:"grip_position,omitempty"`
}

type UpdateSessionRequest struct {
	Name     string `json:"name"`
	Notes    string `json:"notes"`
	Duration int32  `json:"duration"`
	// Only logged sessions send a date. Omitted, the stored one is kept, which is
	// what played sessions rely on since their date is fixed by the run.
	Date string `json:"date,omitempty"`
}

// SessionResponse is the JSON a session is returned as. It is a mapped shape
// rather than the generated row, so reads speak the same snake_case the request
// bodies do.
type SessionResponse struct {
	ID               string  `json:"id"`
	UserID           string  `json:"user_id"`
	Name             string  `json:"name"`
	Notes            string  `json:"notes"`
	Date             string  `json:"date"`
	IsAssessment     bool    `json:"is_assessment"`
	Activity         int32   `json:"activity"`
	Origin           string  `json:"origin"`
	TrainingID       *string `json:"training_id,omitempty"`
	ProgramSessionID *string `json:"program_session_id,omitempty"`
	// Prescription is what the athlete was asked to do, frozen when the session
	// was created. Absent on a session run from nothing, and on rows created
	// before the snapshot existed.
	Prescription      json.RawMessage `json:"prescription,omitempty" swaggertype:"object"`
	Duration          int32           `json:"duration"`
	RepeaterSets      *int32          `json:"repeater_sets,omitempty"`
	RepeaterReps      *int32          `json:"repeater_reps,omitempty"`
	RepeaterWorkTime  *int32          `json:"repeater_work_time,omitempty"`
	RepeaterRestTime  *int32          `json:"repeater_rest_time,omitempty"`
	RepeaterSetRest   *int32          `json:"repeater_set_rest,omitempty"`
	RepeaterSplitHand *bool           `json:"repeater_split_hand,omitempty"`
	UpdatedAt         string          `json:"updated_at"`
}

// SessionListItem is a session as the list endpoints return it, with the rep
// count the list query carries alongside the row.
type SessionListItem struct {
	SessionResponse
	RepCount int64 `json:"rep_count"`
}

// sessionFields maps the columns every session shape shares. The list row and
// the plain row are separate generated types holding the same columns, so the
// two mappers below feed this rather than duplicating it.
type sessionFields struct {
	ID                pgtype.UUID
	UserID            pgtype.UUID
	Name              string
	Notes             string
	Date              pgtype.Timestamptz
	IsAssessment      bool
	Activity          int32
	Origin            string
	TrainingID        pgtype.UUID
	ProgramSessionID  pgtype.UUID
	Prescription      []byte
	Duration          int32
	RepeaterSets      pgtype.Int4
	RepeaterReps      pgtype.Int4
	RepeaterWorkTime  pgtype.Int4
	RepeaterRestTime  pgtype.Int4
	RepeaterSetRest   pgtype.Int4
	RepeaterSplitHand pgtype.Bool
	UpdatedAt         pgtype.Timestamptz
}

func optionalUUIDString(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := id.String()
	return &s
}

func optionalInt32(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func (f sessionFields) toResponse() SessionResponse {
	resp := SessionResponse{
		ID:               f.ID.String(),
		UserID:           f.UserID.String(),
		Name:             f.Name,
		Notes:            f.Notes,
		Date:             f.Date.Time.UTC().Format(time.RFC3339),
		IsAssessment:     f.IsAssessment,
		Activity:         f.Activity,
		Origin:           f.Origin,
		TrainingID:       optionalUUIDString(f.TrainingID),
		ProgramSessionID: optionalUUIDString(f.ProgramSessionID),
		Duration:         f.Duration,
		RepeaterSets:     optionalInt32(f.RepeaterSets),
		RepeaterReps:     optionalInt32(f.RepeaterReps),
		RepeaterWorkTime: optionalInt32(f.RepeaterWorkTime),
		RepeaterRestTime: optionalInt32(f.RepeaterRestTime),
		RepeaterSetRest:  optionalInt32(f.RepeaterSetRest),
		UpdatedAt:        f.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if len(f.Prescription) > 0 {
		resp.Prescription = json.RawMessage(f.Prescription)
	}
	if f.RepeaterSplitHand.Valid {
		resp.RepeaterSplitHand = &f.RepeaterSplitHand.Bool
	}
	return resp
}

func sessionToResponse(s db.Session) SessionResponse {
	return sessionFields(s).toResponse()
}

func sessionRowToListItem(r db.GetUserSessionsRow) SessionListItem {
	return SessionListItem{
		SessionResponse: sessionFields{
			ID:                r.ID,
			UserID:            r.UserID,
			Name:              r.Name,
			Notes:             r.Notes,
			Date:              r.Date,
			IsAssessment:      r.IsAssessment,
			Activity:          r.Activity,
			Origin:            r.Origin,
			TrainingID:        r.TrainingID,
			ProgramSessionID:  r.ProgramSessionID,
			Prescription:      r.Prescription,
			Duration:          r.Duration,
			RepeaterSets:      r.RepeaterSets,
			RepeaterReps:      r.RepeaterReps,
			RepeaterWorkTime:  r.RepeaterWorkTime,
			RepeaterRestTime:  r.RepeaterRestTime,
			RepeaterSetRest:   r.RepeaterSetRest,
			RepeaterSplitHand: r.RepeaterSplitHand,
			UpdatedAt:         r.UpdatedAt,
		}.toResponse(),
		RepCount: r.RepCount,
	}
}

// RepDataResponse is a repetition as the session endpoints return it.
type RepDataResponse struct {
	ID            string  `json:"id"`
	SessionID     string  `json:"session_id"`
	AverageWeight float32 `json:"average_weight"`
	TargetWeight  float32 `json:"target_weight"`
	Duration      int32   `json:"duration"`
	Index         int32   `json:"index"`
	IsRest        bool    `json:"is_rest"`
	RightHand     bool    `json:"right_hand"`
	GripPosition  int32   `json:"grip_position"`
	EdgeSizeMm    *int32  `json:"edge_size_mm,omitempty"`
	UpdatedAt     string  `json:"updated_at"`
}

func repDataToResponse(r db.RepData) RepDataResponse {
	return RepDataResponse{
		ID:            r.ID.String(),
		SessionID:     r.SessionID.String(),
		AverageWeight: r.AverageWeight,
		TargetWeight:  r.TargetWeight,
		Duration:      r.Duration,
		Index:         r.Index,
		IsRest:        r.IsRest,
		RightHand:     r.RightHand,
		GripPosition:  r.GripPosition,
		EdgeSizeMm:    optionalInt32(r.EdgeSizeMm),
		UpdatedAt:     r.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func repDatasToResponses(rows []db.RepData) []RepDataResponse {
	items := make([]RepDataResponse, 0, len(rows))
	for _, r := range rows {
		items = append(items, repDataToResponse(r))
	}
	return items
}

// AssessmentResponse is an assessment result as the endpoints return it.
type AssessmentResponse struct {
	ID           string   `json:"id"`
	UserID       string   `json:"user_id"`
	SessionID    string   `json:"session_id"`
	Type         int32    `json:"type"`
	RightValue   *float32 `json:"right_value,omitempty"`
	LeftValue    *float32 `json:"left_value,omitempty"`
	GripPosition *int32   `json:"grip_position,omitempty"`
	UpdatedAt    string   `json:"updated_at"`
}

func assessmentToResponse(a db.Assessment) AssessmentResponse {
	resp := AssessmentResponse{
		ID:           a.ID.String(),
		UserID:       a.UserID.String(),
		SessionID:    a.SessionID.String(),
		Type:         a.Type,
		GripPosition: optionalInt32(a.GripPosition),
		UpdatedAt:    a.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if a.RightValue.Valid {
		resp.RightValue = &a.RightValue.Float32
	}
	if a.LeftValue.Valid {
		resp.LeftValue = &a.LeftValue.Float32
	}
	return resp
}

// AssessmentListItem is an assessment as the list endpoints return it, with the
// date of the session it was measured in.
type AssessmentListItem struct {
	AssessmentResponse
	SessionDate string `json:"session_date"`
}

func assessmentRowsToListItems(rows []db.GetUserAssessmentsRow) []AssessmentListItem {
	items := make([]AssessmentListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, AssessmentListItem{
			AssessmentResponse: assessmentToResponse(db.Assessment{
				ID:           r.ID,
				UserID:       r.UserID,
				Type:         r.Type,
				RightValue:   r.RightValue,
				LeftValue:    r.LeftValue,
				SessionID:    r.SessionID,
				GripPosition: r.GripPosition,
				UpdatedAt:    r.UpdatedAt,
			}),
			SessionDate: r.SessionDate.Time.UTC().Format(time.RFC3339),
		})
	}
	return items
}

func assessmentsToResponses(rows []db.Assessment) []AssessmentResponse {
	items := make([]AssessmentResponse, 0, len(rows))
	for _, a := range rows {
		items = append(items, assessmentToResponse(a))
	}
	return items
}

func sessionRowsToListItems(rows []db.GetUserSessionsRow) []SessionListItem {
	items := make([]SessionListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, sessionRowToListItem(r))
	}
	return items
}

// PrescriptionSnapshot is what the athlete was asked to do, resolved once when
// the session is created. The training and the coach's overrides stay editable
// afterwards, so nothing but this copy still describes the prescription the
// session was actually run from.
type PrescriptionSnapshot struct {
	TrainingID   string  `json:"training_id"`
	Title        string  `json:"title"`
	Description  *string `json:"description,omitempty"`
	TrainingType string  `json:"training_type"`
	Goal         *string `json:"goal,omitempty"`
	Comment      *string `json:"comment,omitempty"`
	// ProgramSessionID and CoachNotes are set only when the session was played
	// from a coach's program week.
	ProgramSessionID *string                `json:"program_session_id,omitempty"`
	CoachNotes       *string                `json:"coach_notes,omitempty"`
	Items            []TrainingItemResponse `json:"items"`
}

// buildPrescriptionSnapshot resolves the training the session was run from, with
// the program session's per-item overrides already merged in, so a later edit of
// either cannot rewrite what was prescribed.
func buildPrescriptionSnapshot(ctx context.Context, qtx *db.Queries, trainingID, programSessionID pgtype.UUID) ([]byte, error) {
	training, err := qtx.GetTraining(ctx, trainingID)
	if err != nil {
		return nil, err
	}

	rows, err := qtx.GetTrainingItems(ctx, trainingID)
	if err != nil {
		return nil, err
	}

	var zeroParent pgtype.UUID
	snapshot := PrescriptionSnapshot{
		TrainingID:   training.ID.String(),
		Title:        training.Title,
		TrainingType: training.TrainingType,
		Items:        buildTrainingItemTree(rows, zeroParent),
	}
	if training.Description.Valid {
		snapshot.Description = &training.Description.String
	}
	if training.Goal.Valid {
		snapshot.Goal = &training.Goal.String
	}
	if training.Comment.Valid {
		snapshot.Comment = &training.Comment.String
	}

	if programSessionID.Valid {
		programSession, err := qtx.GetCoachProgramWeekSession(ctx, programSessionID)
		if err != nil {
			return nil, err
		}
		id := programSession.ID.String()
		snapshot.ProgramSessionID = &id
		if programSession.Notes.Valid {
			snapshot.CoachNotes = &programSession.Notes.String
		}

		overrides, err := qtx.GetCoachProgramSessionOverrides(ctx, programSessionID)
		if err != nil {
			return nil, err
		}
		byItem := make(map[string]json.RawMessage, len(overrides))
		for _, o := range overrides {
			byItem[o.ItemID.String()] = json.RawMessage(o.Overrides)
		}
		if err := mergeItemOverrides(snapshot.Items, byItem); err != nil {
			return nil, err
		}
	}

	return json.Marshal(snapshot)
}

// mergeItemOverrides walks the item tree and applies the override each item
// carries, if any.
func mergeItemOverrides(items []TrainingItemResponse, byItem map[string]json.RawMessage) error {
	for i := range items {
		if raw, ok := byItem[items[i].ID]; ok {
			if err := mergeItemOverride(items[i].overridable(), raw); err != nil {
				return err
			}
		}
		if err := mergeItemOverrides(items[i].Items, byItem); err != nil {
			return err
		}
	}
	return nil
}

// CreateSession godoc
// @Summary Create a new session
// @Description Create a new training session for the authenticated user with optional rep data and assessments. A session sent with a training_id freezes the prescription it was run from onto the session, with the program session overrides merged in, so later edits of the training cannot rewrite it.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateSessionRequest true "Session details"
// @Success 201 {object} SessionResponse "Session created successfully"
// @Failure 400 {object} map[string]string "Invalid request or validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Training or program session not available to the user"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions [post]
func (h *SessionHandler) CreateSession(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req CreateSessionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}

	if !isValidActivity(req.Activity) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid activity"})
	}

	origin := req.Origin
	if origin == "" {
		origin = originLogged
	}
	if origin != originPlayed && origin != originLogged {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Origin must be played or logged"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	trainingID, err := optionalUUID(req.TrainingID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid training ID"})
	}
	programSessionID, err := optionalUUID(req.ProgramSessionID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid program session ID"})
	}
	// Only a played session was run from a prescription. The coach's week edits
	// are frozen against this link, so a logged session holding one would let an
	// athlete lock their coach out of a session they never played.
	if origin != originPlayed && programSessionID.Valid {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Only a played session can reference a program session",
		})
	}

	if !h.authorizeSessionLinks(c, userUUID, trainingID, programSessionID) {
		return nil
	}

	sessionDate := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	if req.Date != "" {
		t, err := parseSessionDate(req.Date)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid date format"})
		}
		sessionDate = pgtype.Timestamptz{Time: t, Valid: true}
	}

	var repeaterSets, repeaterReps, repeaterWorkTime, repeaterRestTime, repeaterSetRest pgtype.Int4
	var repeaterSplitHand pgtype.Bool

	if req.RepeaterSets != nil {
		repeaterSets.Int32 = *req.RepeaterSets
		repeaterSets.Valid = true
	}
	if req.RepeaterReps != nil {
		repeaterReps.Int32 = *req.RepeaterReps
		repeaterReps.Valid = true
	}
	if req.RepeaterWorkTime != nil {
		repeaterWorkTime.Int32 = *req.RepeaterWorkTime
		repeaterWorkTime.Valid = true
	}
	if req.RepeaterRestTime != nil {
		repeaterRestTime.Int32 = *req.RepeaterRestTime
		repeaterRestTime.Valid = true
	}
	if req.RepeaterSetRest != nil {
		repeaterSetRest.Int32 = *req.RepeaterSetRest
		repeaterSetRest.Valid = true
	}
	if req.RepeaterSplitHand != nil {
		repeaterSplitHand.Bool = *req.RepeaterSplitHand
		repeaterSplitHand.Valid = true
	}

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)

	var prescription []byte
	if trainingID.Valid {
		prescription, err = buildPrescriptionSnapshot(c.Context(), qtx, trainingID, programSessionID)
		if err != nil {
			slog.Error("failed to snapshot session prescription", "user_id", userID, "training_id", trainingID.String(), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
		}
	}

	session, err := qtx.CreateSession(c.Context(), db.CreateSessionParams{
		UserID:            userUUID,
		Name:              req.Name,
		Notes:             req.Notes,
		Date:              sessionDate,
		IsAssessment:      req.IsAssessment,
		Activity:          req.Activity,
		Origin:            origin,
		TrainingID:        trainingID,
		ProgramSessionID:  programSessionID,
		Prescription:      prescription,
		Duration:          req.Duration,
		RepeaterSets:      repeaterSets,
		RepeaterReps:      repeaterReps,
		RepeaterWorkTime:  repeaterWorkTime,
		RepeaterRestTime:  repeaterRestTime,
		RepeaterSetRest:   repeaterSetRest,
		RepeaterSplitHand: repeaterSplitHand,
	})
	if err != nil {
		slog.Error("failed to create session", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}

	for _, rd := range req.RepDatas {
		var edgeSizeMm pgtype.Int4
		if rd.EdgeSizeMm != nil {
			edgeSizeMm.Int32 = *rd.EdgeSizeMm
			edgeSizeMm.Valid = true
		}

		_, err := qtx.CreateRepData(c.Context(), db.CreateRepDataParams{
			UserID:        userUUID,
			AverageWeight: rd.AverageWeight,
			SessionID:     session.ID,
			IsRest:        rd.IsRest,
			RightHand:     rd.RightHand,
			Duration:      rd.Duration,
			TargetWeight:  rd.TargetWeight,
			Index:         rd.Index,
			GripPosition:  rd.GripPosition,
			EdgeSizeMm:    edgeSizeMm,
		})
		if err != nil {
			slog.Error("failed to create rep data", "user_id", userID, "session_id", session.ID, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create rep data"})
		}
	}

	for _, a := range req.Assessments {
		var rightValue, leftValue pgtype.Float4
		var gripPosition pgtype.Int4

		if a.RightValue != nil {
			rightValue.Float32 = *a.RightValue
			rightValue.Valid = true
		}
		if a.LeftValue != nil {
			leftValue.Float32 = *a.LeftValue
			leftValue.Valid = true
		}
		if a.GripPosition != nil {
			gripPosition.Int32 = *a.GripPosition
			gripPosition.Valid = true
		}

		_, err := qtx.CreateAssessment(c.Context(), db.CreateAssessmentParams{
			UserID:       userUUID,
			Type:         a.Type,
			RightValue:   rightValue,
			LeftValue:    leftValue,
			SessionID:    session.ID,
			GripPosition: gripPosition,
		})
		if err != nil {
			slog.Error("failed to create assessment", "user_id", userID, "session_id", session.ID, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create assessment"})
		}
	}

	if err := tx.Commit(c.Context()); err != nil {
		slog.Error("failed to commit session transaction", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize session"})
	}

	return c.Status(fiber.StatusCreated).JSON(sessionToResponse(session))
}

// GetSessions godoc
// @Summary Get all sessions
// @Description Retrieve all training sessions for the authenticated user, each with its rep count
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {array} SessionListItem "List of sessions"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions [get]
func (h *SessionHandler) GetSessions(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	sessions, err := h.queries.GetUserSessions(c.Context(), userUUID)
	if err != nil {
		slog.Error("failed to retrieve sessions", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve sessions"})
	}

	return c.JSON(sessionRowsToListItems(sessions))
}

// GetSession godoc
// @Summary Get a session by ID
// @Description Retrieve a specific session by ID with all related rep data and assessments. User must own the session unless they are an admin.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} map[string]interface{} "Session details with rep_datas and assessments"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Router /api/sessions/{id} [get]
func (h *SessionHandler) GetSession(c fiber.Ctx) error {
	session, _, ok := h.ownedSession().require(c)
	if !ok {
		return nil
	}

	repDatas, _ := h.queries.GetSessionRepDatas(c.Context(), session.ID)
	assessments, _ := h.queries.GetSessionAssessments(c.Context(), session.ID)

	return c.JSON(fiber.Map{
		"session":     sessionToResponse(session),
		"rep_datas":   repDatasToResponses(repDatas),
		"assessments": assessmentsToResponses(assessments),
	})
}

// UpdateSession godoc
// @Summary Update a session
// @Description Update a session's name, notes, and duration. User must own the session unless they are an admin.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Param request body UpdateSessionRequest true "Updated session details"
// @Success 200 {object} SessionResponse "Updated session"
// @Failure 400 {object} map[string]string "Invalid request or session ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions/{id} [put]
func (h *SessionHandler) UpdateSession(c fiber.Ctx) error {
	_, sessionUUID, ok := h.ownedSession().require(c)
	if !ok {
		return nil
	}

	var req UpdateSessionRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	var date pgtype.Timestamptz
	if req.Date != "" {
		t, err := parseSessionDate(req.Date)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid date format"})
		}
		date = pgtype.Timestamptz{Time: t, Valid: true}
	}

	updated, err := h.queries.UpdateSession(c.Context(), db.UpdateSessionParams{
		ID:       sessionUUID,
		Name:     req.Name,
		Notes:    req.Notes,
		Duration: req.Duration,
		Date:     date,
	})
	if err != nil {
		slog.Error("failed to update session", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session"})
	}

	return c.JSON(sessionToResponse(updated))
}

// DeleteSession godoc
// @Summary Delete a session
// @Description Delete a session. User must own the session unless they are an admin.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} map[string]string "Session deleted successfully"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions/{id} [delete]
func (h *SessionHandler) DeleteSession(c fiber.Ctx) error {
	_, sessionUUID, ok := h.ownedSession().require(c)
	if !ok {
		return nil
	}

	if err := h.queries.DeleteSession(c.Context(), sessionUUID); err != nil {
		slog.Error("failed to delete session", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete session"})
	}

	return c.JSON(fiber.Map{"message": "Session deleted successfully"})
}
