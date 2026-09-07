package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
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

// Which hand pulled a rep. A two handed hang is a state of its own rather than
// one of the single hands, which is what the boolean this replaced could not
// say. Mirrored by the rep_datas_hand_check constraint.
const (
	handLeft  = "left"
	handRight = "right"
	handBoth  = "both"
)

// Which open field a session item result answers, mirroring the
// session_item_results_field_check constraint.
const (
	itemResultFieldReps   = "reps"
	itemResultFieldCycles = "cycles"
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
	Name             string              `json:"name"`
	Notes            string              `json:"notes"`
	Date             string              `json:"date,omitempty"`
	IsAssessment     bool                `json:"is_assessment"`
	Activity         int32               `json:"activity"`
	Origin           string              `json:"origin,omitempty"`
	TrainingID       *string             `json:"training_id,omitempty"`
	ProgramSessionID *string             `json:"program_session_id,omitempty"`
	Duration         int32               `json:"duration"`
	RepDatas         []RepDataRequest    `json:"rep_datas,omitempty"`
	Assessments      []AssessmentRequest `json:"assessments,omitempty"`
	// ItemResults are the counts the run resolved for items the prescription
	// left open: an AMRAP the athlete measured by doing it, and the rounds an
	// emom was carried through.
	ItemResults []SessionItemResultRequest `json:"item_results,omitempty"`
	// Samples is the force curve the sensor recorded. Accepted on an assessment
	// only: it is what a critical force or an MVC result means, and on any other
	// session it would be bulk nothing reads.
	Samples *SessionSamplesRequest `json:"samples,omitempty"`
}

// SessionSamplesRequest is a force curve as the app records it: one start
// instant, then a millisecond offset and a kilogram reading per sample. Two
// parallel arrays rather than an object per point, which carries the same curve
// in about a fifth of the bytes.
type SessionSamplesRequest struct {
	T0 string    `json:"t0"`
	Ms []int32   `json:"ms"`
	Kg []float32 `json:"kg"`
}

// maxSessionSamples caps a curve at roughly two hours of the sensor's output,
// well past the longest protocol, so a malformed or hostile body cannot push an
// unbounded document into the row.
const maxSessionSamples = 60000

// encodeSessionSamples checks a curve over and returns it as the JSON the column
// stores, or nil when the request carried none.
func encodeSessionSamples(req *CreateSessionRequest) ([]byte, error) {
	if req.Samples == nil {
		return nil, nil
	}
	s := req.Samples
	if !req.IsAssessment {
		return nil, errors.New("Samples are only accepted on an assessment session")
	}
	if len(s.Ms) != len(s.Kg) {
		return nil, errors.New("Samples must carry as many offsets as readings")
	}
	if len(s.Ms) == 0 {
		return nil, errors.New("Samples must carry at least one reading")
	}
	if len(s.Ms) > maxSessionSamples {
		return nil, fmt.Errorf("Samples must carry at most %d readings", maxSessionSamples)
	}
	if _, err := parseSessionDate(s.T0); err != nil {
		return nil, errors.New("Invalid samples t0")
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil, errors.New("Invalid samples")
	}
	return encoded, nil
}

// SessionItemResultRequest is one count a run answered an open item with. The
// item is named by its id in the frozen prescription, and Occurrence tells the
// passes apart when the item sits inside a block that repeats.
type SessionItemResultRequest struct {
	TrainingItemID string `json:"training_item_id"`
	Occurrence     int32  `json:"occurrence"`
	Field          string `json:"field" enums:"reps,cycles"`
	Value          int32  `json:"value"`
}

type RepDataRequest struct {
	AverageWeight float32 `json:"average_weight"`
	IsRest        bool    `json:"is_rest"`
	// Hand is which hand pulled the rep: "left", "right" or "both". A two handed
	// hang is a state of its own, not one of the single hands.
	Hand         string  `json:"hand"`
	Duration     int32   `json:"duration"`
	TargetWeight float32 `json:"target_weight"`
	Index        int32   `json:"index"`
	GripPosition int32   `json:"grip_position"`
	EdgeSizeMm   *int32  `json:"edge_size_mm,omitempty"`
	// TrainingItemID is the prescription item this rep was played from, absent
	// for a rep recorded outside a training.
	TrainingItemID *string `json:"training_item_id,omitempty"`
	// TargetUnmeasured says the step prescribed a load nothing measured, which
	// is a sensor that dropped mid run. The rep then carries no target, the same
	// as a step nothing was meant to measure, and this is what tells them apart.
	TargetUnmeasured bool `json:"target_unmeasured"`
}

type AssessmentRequest struct {
	AssessmentID string   `json:"assessment_id"`
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

// SessionCoachReplyRequest is the answer a coach writes to the notes an athlete
// left on a session. An empty reply takes a previous answer back.
type SessionCoachReplyRequest struct {
	Reply string `json:"reply"`
}

// maxCoachReplyLength caps an answer at a few paragraphs, which is what the
// exchange is for. Anything longer belongs in the program the coach edits.
const maxCoachReplyLength = 4000

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
	// was created. Absent on a session run from nothing. The list endpoints
	// leave it out, since it is a whole training per row and only the detail
	// screen reads it.
	Prescription json.RawMessage `json:"prescription,omitempty" swaggertype:"object"`
	// Samples is the force curve the sensor recorded, carried on an assessment
	// session only. Absent everywhere else, and left out by the list endpoints
	// for the reason the prescription is.
	Samples  json.RawMessage `json:"samples,omitempty" swaggertype:"object"`
	Duration int32           `json:"duration"`
	// CoachReply is what the coach answered the athlete's notes with, absent
	// while they have not answered. CoachReplyAt dates that answer, and
	// CoachReplyRead says whether the athlete has opened it since it was last
	// written, which is what the app announces an unread answer from.
	CoachReply     *string `json:"coach_reply,omitempty"`
	CoachReplyAt   *string `json:"coach_reply_at,omitempty"`
	CoachReplyRead bool    `json:"coach_reply_read"`
	UpdatedAt      string  `json:"updated_at"`
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
	ID               pgtype.UUID
	UserID           pgtype.UUID
	Name             string
	Notes            string
	Date             pgtype.Timestamptz
	IsAssessment     bool
	Activity         int32
	Origin           string
	TrainingID       pgtype.UUID
	ProgramSessionID pgtype.UUID
	Prescription     []byte
	Samples          []byte
	Duration         int32
	CoachReply       pgtype.Text
	CoachReplyAt     pgtype.Timestamptz
	CoachReplyReadAt pgtype.Timestamptz
	UpdatedAt        pgtype.Timestamptz
}

func optionalUUIDString(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := id.String()
	return &s
}

// optionalTimestamp formats a nullable instant the way every other timestamp
// leaves this package, or returns nil when the column holds none.
func optionalTimestamp(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339)
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
		CoachReplyAt:     optionalTimestamp(f.CoachReplyAt),
		CoachReplyRead:   f.CoachReplyReadAt.Valid,
		UpdatedAt:        f.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if f.CoachReply.Valid {
		resp.CoachReply = &f.CoachReply.String
	}
	if len(f.Prescription) > 0 {
		resp.Prescription = json.RawMessage(f.Prescription)
	}
	if len(f.Samples) > 0 {
		resp.Samples = json.RawMessage(f.Samples)
	}
	return resp
}

func sessionToResponse(s db.Session) SessionResponse {
	return sessionFields(s).toResponse()
}

func sessionRowToListItem(r db.GetUserSessionsRow) SessionListItem {
	return SessionListItem{
		SessionResponse: sessionFields{
			ID:               r.ID,
			UserID:           r.UserID,
			Name:             r.Name,
			Notes:            r.Notes,
			Date:             r.Date,
			IsAssessment:     r.IsAssessment,
			Activity:         r.Activity,
			Origin:           r.Origin,
			TrainingID:       r.TrainingID,
			ProgramSessionID: r.ProgramSessionID,
			Duration:         r.Duration,
			CoachReply:       r.CoachReply,
			CoachReplyAt:     r.CoachReplyAt,
			CoachReplyReadAt: r.CoachReplyReadAt,
			UpdatedAt:        r.UpdatedAt,
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
	// Hand is which hand pulled the rep: "left", "right" or "both".
	Hand         string `json:"hand"`
	GripPosition int32  `json:"grip_position"`
	EdgeSizeMm   *int32 `json:"edge_size_mm,omitempty"`
	// TrainingItemID keys into the session prescription items, so the reps can
	// be read block by block. Absent on a rep played outside a training, and on
	// sessions recorded before the app sent it.
	TrainingItemID *string `json:"training_item_id,omitempty"`
	// TargetUnmeasured marks a rep the run was supposed to grade and could not,
	// because the sensor delivered nothing while the step was running. It has no
	// target for that reason, so a client counting on-target reps leaves it out
	// of the ratio rather than reading it as a miss.
	TargetUnmeasured bool   `json:"target_unmeasured"`
	UpdatedAt        string `json:"updated_at"`
}

func repDataToResponse(r db.RepData) RepDataResponse {
	return RepDataResponse{
		ID:               r.ID.String(),
		SessionID:        r.SessionID.String(),
		AverageWeight:    r.AverageWeight,
		TargetWeight:     r.TargetWeight,
		Duration:         r.Duration,
		Index:            r.Index,
		IsRest:           r.IsRest,
		Hand:             r.Hand,
		GripPosition:     r.GripPosition,
		EdgeSizeMm:       optionalInt32(r.EdgeSizeMm),
		TrainingItemID:   optionalUUIDString(r.TrainingItemID),
		TargetUnmeasured: r.TargetUnmeasured,
		UpdatedAt:        r.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func repDatasToResponses(rows []db.RepData) []RepDataResponse {
	items := make([]RepDataResponse, 0, len(rows))
	for _, r := range rows {
		items = append(items, repDataToResponse(r))
	}
	return items
}

// SessionItemResultResponse is a count the run recorded for an item the
// prescription left open, as the endpoints return it.
type SessionItemResultResponse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	// TrainingItemID keys into the session prescription items, the same way a
	// rep does, so the count can be shown against what was asked for.
	TrainingItemID string `json:"training_item_id"`
	Occurrence     int32  `json:"occurrence"`
	Field          string `json:"field" enums:"reps,cycles"`
	Value          int32  `json:"value"`
	UpdatedAt      string `json:"updated_at"`
}

func sessionItemResultToResponse(r db.SessionItemResult) SessionItemResultResponse {
	return SessionItemResultResponse{
		ID:             r.ID.String(),
		SessionID:      r.SessionID.String(),
		TrainingItemID: r.TrainingItemID.String(),
		Occurrence:     r.Occurrence,
		Field:          r.Field,
		Value:          r.Value,
		UpdatedAt:      r.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func sessionItemResultsToResponses(rows []db.SessionItemResult) []SessionItemResultResponse {
	items := make([]SessionItemResultResponse, 0, len(rows))
	for _, r := range rows {
		items = append(items, sessionItemResultToResponse(r))
	}
	return items
}

// SessionDetailResponse is the envelope both session-read endpoints return: the
// session with the reps and assessments recorded against it. Typed so the
// clients reading training_item_id off a rep have a generated contract for it.
type SessionDetailResponse struct {
	Session     SessionResponse      `json:"session"`
	RepDatas    []RepDataResponse    `json:"rep_datas"`
	Assessments []AssessmentResponse `json:"assessments"`
	// ItemResults are the counts the run recorded for the items the
	// prescription left open, empty for a session that had none.
	ItemResults []SessionItemResultResponse `json:"item_results"`
}

// AssessmentResponse is an assessment result as the endpoints return it, with
// the assessment that defines it, so a client can name and format the number
// without a second request. The definition reads as it stands now, not as it did
// when the result was measured: the profile section is the assessment, not the
// run that produced one of its rows.
type AssessmentResponse struct {
	ID           string `json:"id"`
	UserID       string `json:"user_id"`
	SessionID    string `json:"session_id"`
	AssessmentID string `json:"assessment_id"`
	Label        string `json:"label"`
	Unit         string `json:"unit" enums:"kilograms,seconds,repetitions"`
	PerHand      bool   `json:"per_hand"`
	// The training the assessment is run from, absent for the ones Crimpy ships.
	TrainingID   *string  `json:"training_id,omitempty"`
	RightValue   *float32 `json:"right_value,omitempty"`
	LeftValue    *float32 `json:"left_value,omitempty"`
	GripPosition *int32   `json:"grip_position,omitempty"`
	UpdatedAt    string   `json:"updated_at"`
}

// assessmentResult is the shape every assessment read path produces: the result
// row joined to its definition. The generated row types differ per query, so the
// callers fill this in and share one mapper.
type assessmentResult struct {
	Assessment db.Assessment
	Label      string
	Unit       string
	PerHand    bool
	TrainingID pgtype.UUID
}

func assessmentToResponse(r assessmentResult) AssessmentResponse {
	a := r.Assessment
	resp := AssessmentResponse{
		ID:           a.ID.String(),
		UserID:       a.UserID.String(),
		SessionID:    a.SessionID.String(),
		AssessmentID: a.AssessmentID.String(),
		Label:        r.Label,
		Unit:         r.Unit,
		PerHand:      r.PerHand,
		GripPosition: optionalInt32(a.GripPosition),
		UpdatedAt:    a.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if r.TrainingID.Valid {
		trainingID := r.TrainingID.String()
		resp.TrainingID = &trainingID
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
			AssessmentResponse: assessmentToResponse(assessmentResult{
				Assessment: db.Assessment{
					ID:           r.ID,
					UserID:       r.UserID,
					AssessmentID: r.AssessmentID,
					RightValue:   r.RightValue,
					LeftValue:    r.LeftValue,
					SessionID:    r.SessionID,
					GripPosition: r.GripPosition,
					UpdatedAt:    r.UpdatedAt,
				},
				Label:      r.Label,
				Unit:       r.Unit,
				PerHand:    r.PerHand,
				TrainingID: r.TrainingID,
			}),
			SessionDate: r.SessionDate.Time.UTC().Format(time.RFC3339),
		})
	}
	return items
}

func assessmentsToResponses(rows []db.GetSessionAssessmentsRow) []AssessmentResponse {
	items := make([]AssessmentResponse, 0, len(rows))
	for _, r := range rows {
		items = append(items, assessmentToResponse(assessmentResult{
			Assessment: db.Assessment{
				ID:           r.ID,
				UserID:       r.UserID,
				AssessmentID: r.AssessmentID,
				RightValue:   r.RightValue,
				LeftValue:    r.LeftValue,
				SessionID:    r.SessionID,
				GripPosition: r.GripPosition,
				UpdatedAt:    r.UpdatedAt,
			},
			Label:      r.Label,
			Unit:       r.Unit,
			PerHand:    r.PerHand,
			TrainingID: r.TrainingID,
		}))
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
// session was actually run from. The training keeps its own key names here, id
// included, so a client can read a snapshot with the training parser it has.
type PrescriptionSnapshot struct {
	ID           string  `json:"id"`
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
	ResolvedAgainst  PrescriptionInputs     `json:"resolved_against"`
}

// PrescriptionInputs are the athlete's own numbers the prescription is read
// against, frozen with it. A load or a target the coach set as a percentage of
// an assessment is stored as the percentage, so reading it against the results
// the athlete has now would restate the prescription every time they reassess.
//
// The bodyweight a percent_bw load needs is not here: the app keeps it on the
// device and never sends it, so the server has nothing to freeze.
type PrescriptionInputs struct {
	// Empty when the athlete had done no assessment, which is the case where
	// the clients fall back to the value the coach set.
	Assessments []AssessmentResultSnapshot `json:"assessments"`
	// The assessments the prescription references, as they read when the session
	// was created, including the ones the athlete has never done. A reference
	// with no result still has to be named on screen and unit checked before it
	// resolves, and the definition stays editable afterwards.
	Definitions []AssessmentDefinitionSnapshot `json:"definitions,omitempty"`
}

// AssessmentResultSnapshot is the last value the athlete had measured for one
// assessment, per hand. A hand that has never been measured is absent rather
// than zero, since the clients take the coach fallback for it.
type AssessmentResultSnapshot struct {
	AssessmentID string   `json:"assessment_id"`
	RightValue   *float32 `json:"right_value,omitempty"`
	LeftValue    *float32 `json:"left_value,omitempty"`
}

// AssessmentDefinitionSnapshot names an assessment a prescription references and
// says what its result means, which is what lets a client unit check the
// reference and label it without reading a definition it may not own.
type AssessmentDefinitionSnapshot struct {
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	Prompt  *string `json:"prompt,omitempty"`
	Unit    string  `json:"unit" enums:"kilograms,seconds,repetitions"`
	PerHand bool    `json:"per_hand"`
	// The training the assessment is run from, absent on the ones Crimpy ships.
	TrainingID *string `json:"training_id,omitempty"`
	// Set once the unit and the hands can no longer move, because results were
	// measured against them or a training reads a number against them. An editor
	// shows the two controls as fixed rather than letting a coach try and be
	// refused.
	UnitLocked bool `json:"unit_locked"`
}

func assessmentDefinitionToSnapshot(d db.AssessmentDefinition) AssessmentDefinitionSnapshot {
	snapshot := AssessmentDefinitionSnapshot{
		ID:      d.ID.String(),
		Label:   d.Label,
		Unit:    d.Unit,
		PerHand: d.PerHand,
	}
	if d.Prompt.Valid {
		snapshot.Prompt = &d.Prompt.String
	}
	if d.TrainingID.Valid {
		trainingID := d.TrainingID.String()
		snapshot.TrainingID = &trainingID
	}
	return snapshot
}

// lockedAssessmentUnits answers, for each named assessment, whether its unit and
// hands are still free to change. Read once for a whole tree rather than per
// definition.
func lockedAssessmentUnits(ctx context.Context, q *db.Queries, ids []pgtype.UUID) (map[string]bool, error) {
	locked := make(map[string]bool, len(ids))
	for _, id := range ids {
		measured, err := q.CountAssessmentsForDefinition(ctx, id)
		if err != nil {
			return nil, err
		}
		if measured > 0 {
			locked[id.String()] = true
			continue
		}
		referenced, err := q.CountReferencesToAssessment(ctx, id.String())
		if err != nil {
			return nil, err
		}
		locked[id.String()] = referenced > 0
	}
	return locked, nil
}

func assessmentResultsToSnapshot(rows []db.GetUserLatestAssessmentValuesRow) []AssessmentResultSnapshot {
	results := make([]AssessmentResultSnapshot, 0, len(rows))
	for _, r := range rows {
		result := AssessmentResultSnapshot{AssessmentID: r.AssessmentID.String()}
		if r.RightValue.Valid {
			result.RightValue = &r.RightValue.Float32
		}
		if r.LeftValue.Valid {
			result.LeftValue = &r.LeftValue.Float32
		}
		results = append(results, result)
	}
	return results
}

// buildPrescriptionSnapshot resolves the training the session was run from, with
// the program session's per-item overrides already merged in, so a later edit of
// either cannot rewrite what was prescribed. programSession is nil for a session
// played straight from a training, outside any program.
func buildPrescriptionSnapshot(ctx context.Context, qtx *db.Queries, userID, trainingID pgtype.UUID, programSession *db.CoachProgramWeekSession) (PrescriptionSnapshot, error) {
	training, err := qtx.GetTraining(ctx, trainingID)
	if err != nil {
		return PrescriptionSnapshot{}, err
	}

	rows, err := qtx.GetTrainingItems(ctx, trainingID)
	if err != nil {
		return PrescriptionSnapshot{}, err
	}

	var zeroParent pgtype.UUID
	snapshot := PrescriptionSnapshot{
		ID:           training.ID.String(),
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

	// Read before the session is inserted, so an assessment this very run
	// records does not become what its own prescription was read against.
	assessments, err := qtx.GetUserLatestAssessmentValues(ctx, userID)
	if err != nil {
		return PrescriptionSnapshot{}, err
	}
	snapshot.ResolvedAgainst.Assessments = assessmentResultsToSnapshot(assessments)

	if programSession != nil {
		id := programSession.ID.String()
		snapshot.ProgramSessionID = &id
		if programSession.Notes.Valid {
			snapshot.CoachNotes = &programSession.Notes.String
		}

		overrides, err := qtx.GetCoachProgramSessionOverrides(ctx, programSession.ID)
		if err != nil {
			return PrescriptionSnapshot{}, err
		}
		byItem := make(map[string]json.RawMessage, len(overrides))
		for _, o := range overrides {
			byItem[o.ItemID.String()] = json.RawMessage(o.Overrides)
		}
		if err := dropStaleItemOverrides(ctx, qtx, userID, training.UserID, snapshot.Items, byItem, *programSession); err != nil {
			return PrescriptionSnapshot{}, err
		}
		if err := mergeItemOverrides(snapshot.Items, byItem); err != nil {
			return PrescriptionSnapshot{}, err
		}
	}

	// After the overrides are merged: one of them may reference an assessment the
	// training itself does not, and the snapshot has to name every reference it
	// actually carries.
	definitions, err := freezeAssessmentDefinitions(ctx, qtx, snapshot.Items)
	if err != nil {
		return PrescriptionSnapshot{}, err
	}
	snapshot.ResolvedAgainst.Definitions = definitions

	return snapshot, nil
}

// freezeAssessmentDefinitions names every assessment the items reference, so the
// prescription can be labelled and unit checked later without reading a
// definition the reader may not own, and without a rename restating what a past
// session was asked for.
func freezeAssessmentDefinitions(ctx context.Context, qtx *db.Queries, items []TrainingItemResponse) ([]AssessmentDefinitionSnapshot, error) {
	refs := collectAssessmentRefs(responseRefSources(items))
	ids := make([]pgtype.UUID, 0, len(refs))
	for _, ref := range refs {
		var id pgtype.UUID
		if err := id.Scan(ref); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := qtx.GetAssessmentDefinitionsForPrescription(ctx, ids)
	if err != nil {
		return nil, err
	}
	definitions := make([]AssessmentDefinitionSnapshot, 0, len(rows))
	for _, row := range rows {
		definitions = append(definitions, assessmentDefinitionToSnapshot(row))
	}
	return definitions, nil
}

// resolveRepItemLinks turns each rep's training item link into the value to
// store. A link naming an item the frozen prescription does not hold is dropped
// to NULL rather than refused: the coach can delete an item while the athlete is
// mid run, and the link is only a grouping hint the reader already falls back
// from, so refusing would trade a lost grouping for a lost session. A malformed
// id is a client bug, not a race, and still fails the request - the returned
// index names the offending rep, or -1 when every link resolved.
func resolveRepItemLinks(reps []RepDataRequest, prescribedItemIDs map[string]struct{}, userID string) ([]pgtype.UUID, int) {
	resolved := make([]pgtype.UUID, len(reps))
	for i, rd := range reps {
		itemID, err := optionalUUID(rd.TrainingItemID)
		if err != nil {
			return nil, i
		}
		if itemID.Valid {
			if _, ok := prescribedItemIDs[itemID.String()]; !ok {
				slog.Warn("dropping rep link to an item the prescription does not hold",
					"user_id", userID, "rep_index", i, "training_item_id", itemID.String())
				resolved[i] = pgtype.UUID{}
				continue
			}
		}
		resolved[i] = itemID
	}
	return resolved, -1
}

// resolveItemResultLinks turns each open-count result into the item id to store
// against it. It drops a result naming an item the frozen prescription does not
// hold, for the reason resolveRepItemLinks drops a rep link: the coach can
// delete an item while the athlete is mid run, and a count keyed to an item
// nothing can resolve is unreadable anyway, so refusing would trade an
// unreadable number for a lost session. A malformed id is a client bug rather
// than a race and still fails the request, as does a second result claiming a
// field of an item pass that is already answered, which the unique index would
// otherwise refuse mid transaction. The returned index names the offending
// returned error names the offending result; kept says which ones to write.
func resolveItemResultLinks(results []SessionItemResultRequest, prescribedItemIDs map[string]struct{}, userID string) (ids []pgtype.UUID, kept []bool, err error) {
	ids = make([]pgtype.UUID, len(results))
	kept = make([]bool, len(results))
	seen := map[string]struct{}{}
	for i, r := range results {
		switch r.Field {
		case itemResultFieldReps, itemResultFieldCycles:
		default:
			return nil, nil, fmt.Errorf("item result %d: field must be %s or %s", i, itemResultFieldReps, itemResultFieldCycles)
		}
		if r.Value < 0 {
			return nil, nil, fmt.Errorf("item result %d: value must be zero or more", i)
		}
		if r.Occurrence < 0 {
			return nil, nil, fmt.Errorf("item result %d: occurrence must be zero or more", i)
		}
		var itemID pgtype.UUID
		if scanErr := itemID.Scan(r.TrainingItemID); scanErr != nil {
			return nil, nil, fmt.Errorf("item result %d: invalid training item ID", i)
		}
		key := fmt.Sprintf("%s/%d/%s", itemID.String(), r.Occurrence, r.Field)
		if _, dup := seen[key]; dup {
			return nil, nil, fmt.Errorf("item result %d: %s is already answered for pass %d of this item", i, r.Field, r.Occurrence)
		}
		seen[key] = struct{}{}
		if _, ok := prescribedItemIDs[itemID.String()]; !ok {
			slog.Warn("dropping item result naming an item the prescription does not hold",
				"user_id", userID, "result_index", i, "training_item_id", itemID.String())
			continue
		}
		ids[i] = itemID
		kept[i] = true
	}
	return ids, kept, nil
}

// firstInvalidRepHand names the first rep carrying a hand the schema will not
// take, or -1 when every rep is valid. Checked before the insert so a bad value
// costs a 400 rather than the check constraint failing mid transaction.
func firstInvalidRepHand(reps []RepDataRequest) int {
	for i, rd := range reps {
		switch rd.Hand {
		case handLeft, handRight, handBoth:
		default:
			return i
		}
	}
	return -1
}

// collectItemIDs gathers the id of every item of a prescription, nested ones
// included, so a rep claiming to come from one can be checked against it.
func collectItemIDs(items []TrainingItemResponse, ids map[string]struct{}) {
	for _, item := range items {
		ids[item.ID] = struct{}{}
		collectItemIDs(item.Items, ids)
	}
}

// collectOverriddenItems reads every item the overrides target back into the
// shape the validators work on, keyed by item id.
func collectOverriddenItems(items []TrainingItemResponse, byItem map[string]json.RawMessage, bases map[string]TrainingItemRequest) {
	for i := range items {
		if _, overridden := byItem[items[i].ID]; overridden {
			bases[items[i].ID] = itemResponseToRequest(items[i])
		}
		collectOverriddenItems(items[i].Items, byItem, bases)
	}
}

// dropStaleItemOverrides removes from byItem every override the item it targets
// no longer takes. A week is validated against the training as it stood when it
// was saved, and the coach may edit that training afterwards without the week
// being consulted, so an override valid then can describe, once merged, a shape
// both write paths refuse by the time the athlete plays the session. The whole
// override for that item goes rather than the offending key, so what the athlete
// gets is either the coach's prescription entire or the training's own item,
// which is a valid shape by construction. Dropping is never an error: a stale
// override must not stop an athlete starting a session.
func dropStaleItemOverrides(ctx context.Context, qtx *db.Queries, userID, ownerID pgtype.UUID, items []TrainingItemResponse, byItem map[string]json.RawMessage, programSession db.CoachProgramWeekSession) error {
	bases := make(map[string]TrainingItemRequest, len(byItem))
	collectOverriddenItems(items, byItem, bases)

	stale, err := staleOverrides(ctx, qtx, ownerID, bases, byItem)
	if err != nil {
		return err
	}

	for id, reason := range stale {
		slog.Warn("dropping a program override the training item no longer takes",
			"user_id", userID.String(),
			"program_session_id", programSession.ID.String(),
			"week_id", programSession.WeekID.String(),
			"training_id", programSession.TrainingID.String(),
			"training_item_id", id,
			"reason", reason)
		delete(byItem, id)
	}
	return nil
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
// @Description Create a new training session for the authenticated user with optional rep data and assessments. A session run from a prescription freezes it onto the session, with the program session overrides merged in, so later edits of the training cannot rewrite it. An override the training item no longer takes, because the training was edited after the week was prescribed, is dropped rather than merged, so what is frozen is never a shape the write paths refuse. When a program_session_id is sent, that row decides the training, and a training_id disagreeing with it is refused. A logged session may carry the link as well, so a coach slot with nothing to step through can be completed by hand; only a played one locks the coach's week. A rep may name the prescription item it was played from through training_item_id, which must be one of the items the session was prescribed. A rep whose step prescribed a load the run failed to measure sends target_unmeasured, so a client can tell it from a rep no target was ever expected for. That flag is what the clients grade on: such a rep is recorded with no target, and one sent with both is stored as it arrives and still read as unmeasured. A run may also send item_results, the counts it resolved for items the prescription left open: the reps an AMRAP turned out to be, and the rounds an emom was carried through. Each names one of the prescribed items, an occurrence telling repeated passes apart, and the field it answers.
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
	if handIndex := firstInvalidRepHand(req.RepDatas); handIndex >= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("Hand must be left, right or both on rep %d", handIndex),
		})
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

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)

	// The program week row is what prescribed this session, so it decides the
	// training rather than the request. That closes two holes the independent
	// link checks leave open: a request pairing a program session with some
	// other training the caller may also reach, and one sending no training at
	// all, which would lock the coach's week against a session carrying no
	// record of what it asked for.
	var programSession *db.CoachProgramWeekSession
	if programSessionID.Valid {
		ps, err := qtx.GetCoachProgramWeekSession(c.Context(), programSessionID)
		if err != nil {
			slog.Error("failed to read session program session", "user_id", userID, "program_session_id", programSessionID.String(), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
		}
		if trainingID.Valid && trainingID != ps.TrainingID {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Training does not match the one the program session prescribes",
			})
		}
		trainingID = ps.TrainingID
		programSession = &ps
	}

	var prescription []byte
	prescribedItemIDs := map[string]struct{}{}
	if trainingID.Valid {
		snapshot, err := buildPrescriptionSnapshot(c.Context(), qtx, userUUID, trainingID, programSession)
		if err != nil {
			slog.Error("failed to snapshot session prescription", "user_id", userID, "training_id", trainingID.String(), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
		}
		prescription, err = json.Marshal(snapshot)
		if err != nil {
			slog.Error("failed to encode session prescription", "user_id", userID, "training_id", trainingID.String(), "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
		}
		collectItemIDs(snapshot.Items, prescribedItemIDs)
	}

	// Resolved before anything is written, so a rejected id costs a round trip
	// rather than a session insert and every rep before the bad one.
	repItemIDs, errIndex := resolveRepItemLinks(req.RepDatas, prescribedItemIDs, userID)
	if errIndex >= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("Invalid training item ID on rep %d", errIndex),
		})
	}

	itemResultIDs, keepItemResult, itemResultErr := resolveItemResultLinks(req.ItemResults, prescribedItemIDs, userID)
	if itemResultErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": itemResultErr.Error()})
	}

	// Same again: a malformed curve costs a round trip rather than a rolled back
	// insert of the session and every rep behind it.
	samples, err := encodeSessionSamples(&req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Checked here for the same reason, so an assessment the athlete may not
	// record against is refused before the session and its reps are inserted.
	assessmentUUIDs := make([]pgtype.UUID, len(req.Assessments))
	for i, a := range req.Assessments {
		id, ok := requireRecordableAssessment(c, h.queries, a.AssessmentID, userUUID)
		if !ok {
			return nil
		}
		assessmentUUIDs[i] = id
	}

	session, err := qtx.CreateSession(c.Context(), db.CreateSessionParams{
		UserID:           userUUID,
		Name:             req.Name,
		Notes:            req.Notes,
		Date:             sessionDate,
		IsAssessment:     req.IsAssessment,
		Activity:         req.Activity,
		Origin:           origin,
		TrainingID:       trainingID,
		ProgramSessionID: programSessionID,
		Prescription:     prescription,
		Samples:          samples,
		Duration:         req.Duration,
	})
	if err != nil {
		slog.Error("failed to create session", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
	}

	for i, rd := range req.RepDatas {
		var edgeSizeMm pgtype.Int4
		if rd.EdgeSizeMm != nil {
			edgeSizeMm.Int32 = *rd.EdgeSizeMm
			edgeSizeMm.Valid = true
		}

		_, err = qtx.CreateRepData(c.Context(), db.CreateRepDataParams{
			UserID:           userUUID,
			AverageWeight:    rd.AverageWeight,
			SessionID:        session.ID,
			IsRest:           rd.IsRest,
			Hand:             rd.Hand,
			Duration:         rd.Duration,
			TargetWeight:     rd.TargetWeight,
			Index:            rd.Index,
			GripPosition:     rd.GripPosition,
			EdgeSizeMm:       edgeSizeMm,
			TrainingItemID:   repItemIDs[i],
			TargetUnmeasured: rd.TargetUnmeasured,
		})
		if err != nil {
			slog.Error("failed to create rep data", "user_id", userID, "session_id", session.ID, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create rep data"})
		}
	}

	for i, r := range req.ItemResults {
		if !keepItemResult[i] {
			continue
		}
		_, err = qtx.CreateSessionItemResult(c.Context(), db.CreateSessionItemResultParams{
			SessionID:      session.ID,
			UserID:         userUUID,
			TrainingItemID: itemResultIDs[i],
			Occurrence:     r.Occurrence,
			Field:          r.Field,
			Value:          r.Value,
		})
		if err != nil {
			slog.Error("failed to create session item result", "user_id", userID, "session_id", session.ID, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create item result"})
		}
	}

	for i, a := range req.Assessments {
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
			AssessmentID: assessmentUUIDs[i],
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
// @Description Retrieve a specific session by ID with all related rep data, assessments and the counts the run recorded for the items the prescription left open. User must own the session unless they are an admin.
// @Tags Session
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} SessionDetailResponse "Session details with rep_datas, assessments and item_results"
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
	itemResults, _ := h.queries.GetSessionItemResults(c.Context(), session.ID)

	return c.JSON(SessionDetailResponse{
		Session:     sessionToResponse(session),
		RepDatas:    repDatasToResponses(repDatas),
		Assessments: assessmentsToResponses(assessments),
		ItemResults: sessionItemResultsToResponses(itemResults),
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

	// An update touches the name, the notes and the duration, and never the
	// curve. Echoing it back would ship the bulkiest thing the row holds down
	// the wire to answer a rename, for a client that reads none of it.
	updated.Samples = nil
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

// MarkCoachReplyRead godoc
// @Summary Mark a coach reply as read
// @Description Stamp the coach's answer to this session as seen by the athlete. Idempotent: the first read is the one kept. User must own the session unless they are an admin.
// @Tags Session
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session ID (UUID)"
// @Success 200 {object} SessionResponse "Session with the reply marked read"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Session not found or carries no coach reply"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sessions/{id}/coach-reply/read [put]
func (h *SessionHandler) MarkCoachReplyRead(c fiber.Ctx) error {
	_, sessionUUID, ok := h.ownedSession().require(c)
	if !ok {
		return nil
	}

	updated, err := h.queries.MarkSessionCoachReplyRead(c.Context(), sessionUUID)
	if err != nil {
		// The query only matches a session that carries an answer, so no row is
		// an athlete marking one that was never written or was taken back.
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session carries no coach reply"})
		}
		slog.Error("failed to mark coach reply read", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to mark the reply as read"})
	}

	updated.Samples = nil
	return c.JSON(sessionToResponse(updated))
}
