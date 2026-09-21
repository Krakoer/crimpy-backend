package handler

import (
	"bytes"
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// How long a note reported against one pass through a prescribed item may be,
// mirroring the session_item_results_note_check constraint. Long enough for the
// paragraph an athlete writes about a hard set, short enough that the column is
// not a document store.
const maxItemResultNoteLength = 2000

// activityCount bounds the activity label shared with the app: 0 hangboard,
// 1 climbing, 2 stretching, 3 workout, 4 other. It mirrors the
// sessions_activity_check constraint in schema/schema.sql.
const activityCount = int32(5)

func isValidActivity(activity int32) bool {
	return activity >= 0 && activity < activityCount
}

// The session RPE scale, how much recovery a session cost, as the coaching
// sheet prints it: 5 is active recovery, 6 easy but productive, 7 needs less
// than a day of rest before repeating it, 8 one full rest day, 9 two, 10 three
// or more. It starts at 5 because that is where the written anchors start, and
// it mirrors the sessions_rpe_check constraint in schema/schema.sql.
//
// Not the set RPE scale, which measures reps left in reserve and belongs to the
// item a set was played from.
const (
	minSessionRPE = int32(5)
	maxSessionRPE = int32(10)
)

// sessionRPE is the RPE answer a request carries, resolved into the pair of
// columns that store it. Kept together because they are one answer: a session
// is unrated, rated, or ECHEC, and never two of those at once.
type sessionRPE struct {
	value  pgtype.Int4
	failed bool
}

// resolveSessionRPE reads the RPE answer out of a request, rejecting the two
// bodies the store would refuse anyway: a number off the scale, and ECHEC
// claimed alongside one.
func resolveSessionRPE(rpe *int32, failed *bool) (sessionRPE, error) {
	resolved := sessionRPE{failed: failed != nil && *failed}
	if rpe != nil {
		if *rpe < minSessionRPE || *rpe > maxSessionRPE {
			return sessionRPE{}, fmt.Errorf("rpe must be between %d and %d", minSessionRPE, maxSessionRPE)
		}
		if resolved.failed {
			return sessionRPE{}, errors.New("rpe_failed is a value of the scale, so it cannot be sent with an rpe")
		}
		resolved.value = pgtype.Int4{Int32: *rpe, Valid: true}
	}
	return resolved, nil
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
	// ItemResults is what the athlete reported about the items they were
	// prescribed: the count an AMRAP turned out to be, the rounds an emom was
	// carried through, and for any step at all the load, the duration and the
	// note that nothing else records.
	ItemResults []SessionItemResultRequest `json:"item_results,omitempty"`
	// Prescription is what the run was asked to do, sent by the client for a run
	// the server cannot describe: one played from a training Crimpy generates on
	// the device rather than from a stored one. The server freezes its own copy
	// whenever a training or a program slot names one, so sending this alongside
	// either is refused rather than ignored, and without one it is the only thing
	// the reps and the item reports have to name their steps against.
	Prescription json.RawMessage `json:"prescription,omitempty" swaggertype:"object"`
	// Samples is the force curve the sensor recorded. Accepted on an assessment
	// only: it is what a critical force or an MVC result means, and on any other
	// session it would be bulk nothing reads.
	Samples *SessionSamplesRequest `json:"samples,omitempty"`
	// BodyweightKg is the weight the device resolved this run's percent_bw loads
	// against. Sent rather than looked up, because the device may hold a newer
	// measurement than the server has: a run does not need the network, so an
	// athlete can weigh themselves and train before either reaches us. Absent
	// falls back to the latest measurement on file, and absent from both is a
	// session whose percent_bw loads nothing can restate.
	BodyweightKg *float32 `json:"bodyweight_kg,omitempty"`
	// RPE is how much recovery the session cost, on the session RPE scale, as
	// the athlete reported it. Absent on a session they were not asked or
	// skipped the prompt on, which the update path lets them fill in later.
	RPE *int32 `json:"rpe,omitempty"`
	// RPEFailed is the scale's ECHEC: a session the athlete could not carry
	// through. It replaces the number rather than grading it, so sending both is
	// refused.
	RPEFailed *bool `json:"rpe_failed,omitempty"`
}

// maxClientPrescriptionBytes caps a prescription the client sends at a few
// hundred steps of JSON. The server writes its own from a training it can read,
// so this only bounds the one case it cannot check against anything: a run of a
// training that exists nowhere but on the device.
const maxClientPrescriptionBytes = 256 * 1024

// clientPrescription validates the prescription a client sent for a run the
// server has no training to freeze one from, and returns it as it will be
// stored together with the item ids a rep or a report may name.
//
// Stored as it arrived rather than re-encoded through the response shape: it
// describes a training the server has never seen, so anything it carries that
// the server does not model is still the only record of what the athlete was
// asked to do. Every id in it is a name the client chose, which is why the two
// link columns are text.
//
// Not byte for byte, mind: withFrozenBodyweight re-encodes the top level to add
// the frozen inputs, which compacts the JSON and reorders its outermost keys.
// Every value survives, since they travel as raw messages, but nothing should
// hash these bytes or diff them against the device's own copy.
func clientPrescription(raw json.RawMessage) ([]byte, map[string]struct{}, error) {
	if len(raw) > maxClientPrescriptionBytes {
		return nil, nil, errors.New("Prescription is too large")
	}
	var snapshot PrescriptionSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, nil, errors.New("Invalid prescription")
	}
	if len(snapshot.Items) == 0 {
		return nil, nil, errors.New("Prescription prescribes no items")
	}
	ids := map[string]struct{}{}
	if err := collectPrescribedItemIDs(snapshot.Items, ids); err != nil {
		return nil, nil, err
	}
	return raw, ids, nil
}

// prescriptionFreezesBodyweight answers whether a client's own prescription
// already names the weight it was resolved against, which makes it the record
// of what happened and not something to be replaced from the series.
func frozenPrescriptionBodyweight(prescription []byte) *float32 {
	var root struct {
		ResolvedAgainst struct {
			BodyweightKg *float32 `json:"bodyweight_kg"`
		} `json:"resolved_against"`
	}
	if err := json.Unmarshal(prescription, &root); err != nil {
		return nil
	}
	return root.ResolvedAgainst.BodyweightKg
}

// withFrozenBodyweight writes the weight a run resolved its percent_bw loads
// against into a client's own prescription, under resolved_against beside the
// assessments the other path freezes there.
//
// Done by editing the JSON rather than by decoding into PrescriptionSnapshot
// and re-encoding, for the reason clientPrescription keeps the raw bytes: this
// describes a training the server has never seen, so a field it does not model
// is still the only record of what the athlete was asked to do, and a round
// trip through the typed struct would drop it. Decoding one level into raw
// messages keeps every key the client sent.
//
// A nil weight writes nothing, so a session played by an athlete who has never
// recorded one carries no bodyweight rather than a zero.
func withFrozenBodyweight(prescription []byte, weightKg *float32) ([]byte, error) {
	if weightKg == nil {
		return prescription, nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(prescription, &root); err != nil {
		return nil, err
	}

	inputs := map[string]json.RawMessage{}
	if existing, ok := root["resolved_against"]; ok {
		if err := json.Unmarshal(existing, &inputs); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(*weightKg)
	if err != nil {
		return nil, err
	}
	inputs["bodyweight_kg"] = encoded

	resolved, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	root["resolved_against"] = resolved
	return json.Marshal(root)
}

// collectPrescribedItemIDs gathers the id of every item of a client's
// prescription, nested ones included, refusing the ones that would cost the
// session later rather than the report they name.
//
// A blank id would let every unnamed step collide on one row of
// session_item_results. A repeated one is the same collision spelled out: the
// second report of it reads as a second answer to a pass already answered,
// which fails the whole request, so a prescription whose only outcome is that
// 400 is turned away while nothing has been written.
func collectPrescribedItemIDs(items []TrainingItemResponse, ids map[string]struct{}) error {
	for _, item := range items {
		if item.ID == "" {
			return errors.New("Prescription holds an item with no id")
		}
		// Counted in characters rather than bytes, the way the char_length
		// constraint counts it, so a key written in accented text is not cut
		// short of one written in ASCII.
		if utf8.RuneCountInString(item.ID) > maxItemIDLength {
			return errors.New("Prescription holds an item id that is too long")
		}
		if _, repeated := ids[item.ID]; repeated {
			return errors.New("Prescription names the same item twice")
		}
		ids[item.ID] = struct{}{}
		if err := collectPrescribedItemIDs(item.Items, ids); err != nil {
			return err
		}
	}
	return nil
}

// maxItemIDLength matches the check on both link columns, counted the same way
// the database counts it, so an id it would refuse is turned away with a
// message rather than a failed insert.
const maxItemIDLength = 200

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

// SessionItemResultRequest is what the athlete reported about one pass through
// a prescribed item: whichever of the counts, the load, the duration and the
// note they had something to say about. The item is named by its id in the
// frozen prescription, and Occurrence tells the passes apart when the item sits
// inside a block that repeats.
//
// Every reported field is a pointer, so a pass that says nothing about one is
// told from a pass that reports zero: no reps done is a result, and an absent
// rep count is not.
type SessionItemResultRequest struct {
	TrainingItemID string `json:"training_item_id"`
	Occurrence     int32  `json:"occurrence"`
	// Reps is how many repetitions the pass did, which an AMRAP has no other
	// record of.
	Reps *int32 `json:"reps,omitempty"`
	// Cycles is how many rounds of a block the pass was carried through before
	// the athlete dropped out, which an emom has no other record of.
	Cycles *int32 `json:"cycles,omitempty"`
	// LoadKg is the load the pass was actually worked at, in kilograms.
	LoadKg *float32 `json:"load_kg,omitempty"`
	// DurationSeconds is how long the pass actually held.
	DurationSeconds *int32 `json:"duration_seconds,omitempty"`
	// Note is what the athlete wrote about the pass.
	Note *string `json:"note,omitempty"`
}

// reported says whether the request carries anything at all, which is what the
// session_item_results_reported_check constraint refuses a row without.
func (r SessionItemResultRequest) reported() bool {
	return r.Reps != nil || r.Cycles != nil || r.LoadKg != nil ||
		r.DurationSeconds != nil || r.Note != nil
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

// UpdateSessionRequest is what an edit changes about a session. Every field is
// optional and every one left out is kept, so a client that sends only what it
// means to change cannot blank the rest: an athlete filling in an RPE weeks
// later should not have to restate the notes to keep them.
type UpdateSessionRequest struct {
	// Sent, the name replaces the stored one, and it may not be empty, for the
	// reason the create path refuses an empty one.
	Name  *string `json:"name,omitempty"`
	Notes *string `json:"notes,omitempty"`
	// Sent, the duration replaces the stored one, in seconds, and may not be
	// negative. Only a logged session sends it: a played one is timed by its run.
	Duration *int32 `json:"duration,omitempty"`
	// Only logged sessions send a date. Omitted, the stored one is kept, which is
	// what played sessions rely on since their date is fixed by the run. A
	// pointer like the fields above, so this struct spells "not sent" one way
	// rather than two.
	Date *string `json:"date,omitempty"`
	// The athlete's RPE answer, which this path exists to let them give after
	// the fact: forgetting it at the end of a run is the normal case, and a
	// played session keeps it editable even though nothing else on it is.
	//
	// The pair is one answer, so it is one field as far as keeping goes: sending
	// neither leaves the stored answer alone, and sending either replaces the
	// whole of it. That is how a rated session is taken back to unrated, with
	// "rpe_failed": false and no "rpe" beside it.
	RPE       *int32 `json:"rpe,omitempty"`
	RPEFailed *bool  `json:"rpe_failed,omitempty"`
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
	// was created: from the training or the program slot the session names, or
	// from the copy the client sent for a run of a training the server cannot
	// read. Absent only on a session that answers no prescription at all. The
	// list endpoints leave it out, since it is a whole training per row and only
	// the detail screen reads it.
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
	// RPE is how much recovery the session cost on the session RPE scale, absent
	// while the athlete has not reported one. RPEFailed is the scale's ECHEC,
	// and never true beside a number.
	RPE       *int32 `json:"rpe,omitempty"`
	RPEFailed bool   `json:"rpe_failed"`
	UpdatedAt string `json:"updated_at"`
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
	Rpe              pgtype.Int4
	RpeFailed        bool
	UpdatedAt        pgtype.Timestamptz
}

func optionalUUIDString(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := id.String()
	return &s
}

// optionalString hands back what a nullable text column holds, or nil when it
// holds nothing.
func optionalString(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
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

// toPgText carries a field a request may leave out into the null the statement
// reads as "keep what is stored". Named for the direction it runs, since
// optionalString above is the same conversion the other way.
func toPgText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func toPgInt4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
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
		RPE:              optionalInt32(f.Rpe),
		RPEFailed:        f.RpeFailed,
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
			Rpe:              r.Rpe,
			RpeFailed:        r.RpeFailed,
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
		TrainingItemID:   optionalString(r.TrainingItemID),
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

// SessionItemResultResponse is what the athlete reported about one pass through
// a prescribed item, as the endpoints return it. A field the pass said nothing
// about is absent rather than zero, so a coach reading it is never shown a
// number the athlete did not give.
type SessionItemResultResponse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	// TrainingItemID keys into the session prescription items, the same way a
	// rep does, so what was achieved can be shown against what was asked for.
	TrainingItemID  string   `json:"training_item_id"`
	Occurrence      int32    `json:"occurrence"`
	Reps            *int32   `json:"reps,omitempty"`
	Cycles          *int32   `json:"cycles,omitempty"`
	LoadKg          *float32 `json:"load_kg,omitempty"`
	DurationSeconds *int32   `json:"duration_seconds,omitempty"`
	Note            *string  `json:"note,omitempty"`
	UpdatedAt       string   `json:"updated_at"`
}

func sessionItemResultToResponse(r db.SessionItemResult) SessionItemResultResponse {
	resp := SessionItemResultResponse{
		ID:             r.ID.String(),
		SessionID:      r.SessionID.String(),
		TrainingItemID: r.TrainingItemID,
		Occurrence:     r.Occurrence,
		UpdatedAt:      r.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if r.Reps.Valid {
		resp.Reps = &r.Reps.Int32
	}
	if r.Cycles.Valid {
		resp.Cycles = &r.Cycles.Int32
	}
	if r.LoadKg.Valid {
		resp.LoadKg = &r.LoadKg.Float32
	}
	if r.DurationSeconds.Valid {
		resp.DurationSeconds = &r.DurationSeconds.Int32
	}
	if r.Note.Valid {
		resp.Note = &r.Note.String
	}
	return resp
}

func sessionItemResultsToResponses(rows []db.SessionItemResult) []SessionItemResultResponse {
	items := make([]SessionItemResultResponse, 0, len(rows))
	for _, r := range rows {
		items = append(items, sessionItemResultToResponse(r))
	}
	return items
}

// sessionDetailReads loads the three collections a session detail is drawn from,
// for the athlete's endpoint and the coach's alike, so the two cannot answer with
// different parts of a session or disagree about what a failed read means.
//
// A read that fails leaves its own part empty rather than failing the whole
// detail, which is deliberate: the caller still gets the session. It is logged
// rather than swallowed, so that an outage does not read as an athlete who
// recorded nothing, which is what an empty assessments array says.
// GetSessionAssessments gained a new way to fail with bodyweight_for_result: on a
// database the migration has not reached yet, which is the window of a deploy
// whose API image rolls before its migrate container finishes.
//
// Each comes back as an empty slice rather than nil on failure, which is what the
// response mappers already turn any of them into.
func sessionDetailReads(
	c fiber.Ctx,
	queries *db.Queries,
	sessionID pgtype.UUID,
) ([]db.RepData, []db.GetSessionAssessmentsRow, []db.SessionItemResult) {
	repDatas, err := queries.GetSessionRepDatas(c.Context(), sessionID)
	if err != nil {
		slog.Error("failed to retrieve the session rep datas", "session_id", sessionID.String(), "error", err)
	}
	assessments, err := queries.GetSessionAssessments(c.Context(), sessionID)
	if err != nil {
		slog.Error("failed to retrieve the session assessments", "session_id", sessionID.String(), "error", err)
	}
	itemResults, err := queries.GetSessionItemResults(c.Context(), sessionID)
	if err != nil {
		slog.Error("failed to retrieve the session item results", "session_id", sessionID.String(), "error", err)
	}
	return repDatas, assessments, itemResults
}

// SessionDetailResponse is the envelope both session-read endpoints return: the
// session with the reps and assessments recorded against it. Typed so the
// clients reading training_item_id off a rep have a generated contract for it.
type SessionDetailResponse struct {
	Session     SessionResponse      `json:"session"`
	RepDatas    []RepDataResponse    `json:"rep_datas"`
	Assessments []AssessmentResponse `json:"assessments"`
	// ItemResults is what the athlete reported about the items they were
	// prescribed, empty for a session they reported nothing on.
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
	// Whether the result reads as a ratio to the bodyweight it was pulled at
	// rather than as an absolute load. Display only: the value beside it is the
	// raw measurement, and the denominator is the two fields below.
	BodyweightRelative bool `json:"bodyweight_relative"`
	// The weigh-in a bodyweight relative score is divided by: the last one taken
	// at or before the session that measured the result. It travels on the result
	// rather than being left to a client to pick out of a bodyweight series, so
	// every screen reads one result against one weight, the session detail
	// included.
	//
	// Absent when no weigh-in qualifies, which is a ratio a reader declines rather
	// than invents. That is also what POST /api/assessments answers with when the
	// athlete has never weighed in, which is a state the clients already draw:
	// recording a result does not require a weigh-in to exist first.
	BodyweightKg *float32 `json:"bodyweight_kg,omitempty"`
	// When that weigh-in was taken, which is how near the denominator is to the
	// result it divides, and what decides whether the ratio means anything at all.
	// Absent exactly when the weight is.
	BodyweightMeasuredAt *string `json:"bodyweight_measured_at,omitempty"`
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
	Assessment         db.Assessment
	Label              string
	Unit               string
	PerHand            bool
	BodyweightRelative bool
	TrainingID         pgtype.UUID
	// The weigh-in the result divides by, as the queries send it: zero standing
	// for "no weigh-in qualifies", since a real measurement is strictly above
	// zero. Every caller looks it up, the recording path included, so the field
	// means the same thing on every endpoint that answers with a result.
	BodyweightKg         float32
	BodyweightMeasuredAt pgtype.Timestamptz
}

func assessmentToResponse(r assessmentResult) AssessmentResponse {
	a := r.Assessment
	resp := AssessmentResponse{
		ID:                 a.ID.String(),
		UserID:             a.UserID.String(),
		SessionID:          a.SessionID.String(),
		AssessmentID:       a.AssessmentID.String(),
		Label:              r.Label,
		Unit:               r.Unit,
		PerHand:            r.PerHand,
		BodyweightRelative: r.BodyweightRelative,
		GripPosition:       optionalInt32(a.GripPosition),
		UpdatedAt:          a.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	resp.BodyweightKg = measuredBodyweight(r.BodyweightKg)
	resp.BodyweightMeasuredAt = bodyweightMeasuredAt(resp.BodyweightKg, r.BodyweightMeasuredAt)
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

// AssessmentListItem is an assessment as the list endpoints return it: the
// result, its denominator, and the date of the session it was measured in, which
// only the listing carries because only the listing draws results from sessions
// the caller did not ask for by id.
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
				Label:                r.Label,
				Unit:                 r.Unit,
				PerHand:              r.PerHand,
				BodyweightRelative:   r.BodyweightRelative,
				TrainingID:           r.TrainingID,
				BodyweightKg:         r.BodyweightKg,
				BodyweightMeasuredAt: r.BodyweightMeasuredAt,
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
			Label:                r.Label,
			Unit:                 r.Unit,
			PerHand:              r.PerHand,
			BodyweightRelative:   r.BodyweightRelative,
			TrainingID:           r.TrainingID,
			BodyweightKg:         r.BodyweightKg,
			BodyweightMeasuredAt: r.BodyweightMeasuredAt,
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
type PrescriptionInputs struct {
	// Empty when the athlete had done no assessment, which is the case where
	// the clients fall back to the value the coach set.
	Assessments []AssessmentResultSnapshot `json:"assessments"`
	// The assessments the prescription references, as they read when the session
	// was created, including the ones the athlete has never done. A reference
	// with no result still has to be named on screen and unit checked before it
	// resolves, and the definition stays editable afterwards.
	Definitions []AssessmentDefinitionSnapshot `json:"definitions,omitempty"`
	// BodyweightKg is what a percent_bw load was read against, in kilograms.
	// Absent when the athlete has never recorded a weight and the device sent
	// none, which is the case where a percent_bw load cannot be restated later
	// and a reader has to say so rather than guess.
	BodyweightKg *float32 `json:"bodyweight_kg,omitempty"`
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
	// Whether the result is drawn as a ratio to the bodyweight it was pulled at.
	// Display only, and read by nothing that resolves a percentage: a
	// prescription is resolved against the raw kilograms whatever this says.
	BodyweightRelative bool `json:"bodyweight_relative"`
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
		ID:                 d.ID.String(),
		Label:              d.Label,
		Unit:               d.Unit,
		PerHand:            d.PerHand,
		BodyweightRelative: d.BodyweightRelative,
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
// its hands are frozen: a result was measured under them, or a training item or
// a program week override reads a number against them.
//
// One query for the whole set, because the reference half of that question
// reads a table through and a listing asks it of every row it serves.
func lockedAssessmentUnits(ctx context.Context, q *db.Queries, ids []pgtype.UUID) (map[string]bool, error) {
	locked := make(map[string]bool, len(ids))
	for _, id := range ids {
		locked[id.String()] = false
	}
	if len(ids) == 0 {
		return locked, nil
	}

	frozen, err := q.GetLockedAssessmentDefinitions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range frozen {
		locked[id.String()] = true
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
func buildPrescriptionSnapshot(ctx context.Context, qtx *db.Queries, userID, trainingID pgtype.UUID, programSession *db.CoachProgramWeekSession, usedBodyweight *float32) (PrescriptionSnapshot, error) {
	training, err := qtx.GetTraining(ctx, trainingID)
	if err != nil {
		return PrescriptionSnapshot{}, err
	}

	rows, err := qtx.GetTrainingItems(ctx, []pgtype.UUID{trainingID})
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

	// What the device says it used wins over what is on file. The device can
	// hold a measurement the server has not seen, because a run needs no
	// network, and freezing our own number would record a weight the loads were
	// not actually resolved against.
	snapshot.ResolvedAgainst.BodyweightKg = usedBodyweight
	if snapshot.ResolvedAgainst.BodyweightKg == nil {
		onFile, err := latestBodyweight(ctx, qtx, userID)
		if err != nil {
			return PrescriptionSnapshot{}, err
		}
		snapshot.ResolvedAgainst.BodyweightKg = onFile
	}

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
	ids := assessmentRefIDs(responseRefSources(items))
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := qtx.GetAssessmentDefinitionsForPrescription(ctx, ids)
	if err != nil {
		return nil, err
	}
	return assessmentDefinitionSnapshots(rows), nil
}

// freezeOwnedAssessmentDefinitions is freezeAssessmentDefinitions for references
// that reach the reader from outside the training tree, where no item they were
// handed already carries the reference. Those are resolved against the owner
// that wrote the reference, so naming them cannot hand the reader a definition
// belonging to anybody else.
func freezeOwnedAssessmentDefinitions(ctx context.Context, q *db.Queries, ownerID pgtype.UUID, sources []assessmentRefSource) ([]AssessmentDefinitionSnapshot, error) {
	ids := assessmentRefIDs(sources)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.GetAssessmentDefinitionsByIDs(ctx, db.GetAssessmentDefinitionsByIDsParams{
		Ids:    ids,
		UserID: ownerID,
	})
	if err != nil {
		return nil, err
	}
	return assessmentDefinitionSnapshots(rows), nil
}

func assessmentDefinitionSnapshots(rows []db.AssessmentDefinition) []AssessmentDefinitionSnapshot {
	definitions := make([]AssessmentDefinitionSnapshot, 0, len(rows))
	for _, row := range rows {
		definitions = append(definitions, assessmentDefinitionToSnapshot(row))
	}
	return definitions
}

// appendMissingAssessments adds every definition the list does not already name,
// so a reader handed two sources of references does not see an assessment twice.
func appendMissingAssessments(named, extra []AssessmentDefinitionSnapshot) []AssessmentDefinitionSnapshot {
	if len(extra) == 0 {
		return named
	}
	seen := make(map[string]struct{}, len(named)+len(extra))
	for _, d := range named {
		seen[d.ID] = struct{}{}
	}
	for _, d := range extra {
		if _, already := seen[d.ID]; already {
			continue
		}
		seen[d.ID] = struct{}{}
		named = append(named, d)
	}
	return named
}

// resolveRepItemLinks turns each rep's training item link into the value to
// store. A link naming an item the frozen prescription does not hold is dropped
// to NULL rather than refused: the coach can delete an item while the athlete is
// mid run, and the link is only a grouping hint the reader already falls back
// from, so refusing would trade a lost grouping for a lost session. A link the
// column could not hold at all is a client bug, not a race, and still fails the
// request - the returned index names the offending rep, or -1 when every link
// resolved.
//
// The link is a name in the snapshot rather than a uuid, because a snapshot the
// client sent names its own items, so it is checked for length and emptiness
// instead of being parsed.
func resolveRepItemLinks(reps []RepDataRequest, prescribedItemIDs map[string]struct{}, userID string) ([]pgtype.Text, int) {
	resolved := make([]pgtype.Text, len(reps))
	for i, rd := range reps {
		if rd.TrainingItemID == nil {
			continue
		}
		itemID := *rd.TrainingItemID
		if itemID == "" || utf8.RuneCountInString(itemID) > maxItemIDLength {
			return nil, i
		}
		if _, ok := prescribedItemIDs[itemID]; !ok {
			slog.Warn("dropping rep link to an item the prescription does not hold",
				"user_id", userID, "rep_index", i, "training_item_id", itemID)
			continue
		}
		resolved[i] = pgtype.Text{String: itemID, Valid: true}
	}
	return resolved, -1
}

// storeRejectedInput says whether a write failed because of what the client
// sent rather than because of anything the server did: a NUL, an unpaired
// surrogate, invalid UTF-8, a number no numeric type holds. Go accepts all of
// them as JSON and jsonb accepts none, and the only body on a session the server
// does not encode itself is the prescription a client hands over.
//
// Enumerating them before the insert cannot work, since Go's JSON is strictly
// wider than jsonb's; asking the database what it refused is the one check that
// covers the whole difference. Class 22 is "data exception", which is that
// difference and nothing the server is responsible for.
func storeRejectedInput(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return strings.HasPrefix(pgErr.Code, "22")
}

// itemResultInsert is one validated result and the prescription item it is
// stored against, ready for the insert. A result the prescription does not hold
// never reaches this slice, which is what replaces a "keep this one" flag
// running alongside the request.
type itemResultInsert struct {
	request SessionItemResultRequest
	itemID  string
}

// resolveItemResults validates what the athlete reported against the items of
// the frozen prescription and returns the rows to write. It drops a result
// naming an item the prescription does not hold, for the reason
// resolveRepItemLinks drops a rep link: the coach can delete an item while the
// athlete is mid run, and a report keyed to an item nothing can resolve is
// unreadable anyway, so refusing would trade an unreadable line for a lost
// session. It also drops a result that reports nothing once its note is
// trimmed, which is an athlete who opened a field and typed nothing in it
// rather than an error worth losing the session over.
//
// An id the column could not hold, a negative number, an overlong note and a
// second result claiming a pass another one already answered all fail the
// request: each is a client bug rather than a race, and the last would otherwise
// be the unique index failing mid transaction. The returned error names the
// offending result.
//
// The id is a name in the snapshot rather than a uuid, because a snapshot the
// client sent names its own items.
func resolveItemResults(results []SessionItemResultRequest, prescribedItemIDs map[string]struct{}, userID string) ([]itemResultInsert, error) {
	inserts := make([]itemResultInsert, 0, len(results))
	seen := map[string]struct{}{}
	for i, r := range results {
		if r.Occurrence < 0 {
			return nil, fmt.Errorf("item result %d: occurrence must be zero or more", i)
		}
		if r.Reps != nil && *r.Reps < 0 {
			return nil, fmt.Errorf("item result %d: reps must be zero or more", i)
		}
		if r.Cycles != nil && *r.Cycles < 0 {
			return nil, fmt.Errorf("item result %d: cycles must be zero or more", i)
		}
		if r.LoadKg != nil && *r.LoadKg < 0 {
			return nil, fmt.Errorf("item result %d: load must be zero or more", i)
		}
		if r.DurationSeconds != nil && *r.DurationSeconds < 0 {
			return nil, fmt.Errorf("item result %d: duration must be zero or more", i)
		}
		if r.Note != nil {
			trimmed := strings.TrimSpace(*r.Note)
			// Counted in characters rather than bytes, the way the
			// char_length constraint counts it, so an accented note is not cut
			// short of one written in ASCII.
			if utf8.RuneCountInString(trimmed) > maxItemResultNoteLength {
				return nil, fmt.Errorf("item result %d: note must be at most %d characters", i, maxItemResultNoteLength)
			}
			if trimmed == "" {
				r.Note = nil
			} else {
				r.Note = &trimmed
			}
		}
		itemID := r.TrainingItemID
		if itemID == "" || utf8.RuneCountInString(itemID) > maxItemIDLength {
			return nil, fmt.Errorf("item result %d: invalid training item ID", i)
		}
		if !r.reported() {
			continue
		}
		// Membership is settled before the pass is booked as answered, so a
		// result the prescription cannot place costs nothing at all: booking it
		// first would let two dropped results collide with each other and fail
		// the request over a pair of rows neither of which is ever written.
		if _, ok := prescribedItemIDs[itemID]; !ok {
			slog.Warn("dropping item result naming an item the prescription does not hold",
				"user_id", userID, "result_index", i, "training_item_id", itemID)
			continue
		}
		key := fmt.Sprintf("%s/%d", itemID, r.Occurrence)
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("item result %d: pass %d of this item is already answered", i, r.Occurrence)
		}
		seen[key] = struct{}{}
		inserts = append(inserts, itemResultInsert{request: r, itemID: itemID})
	}
	return inserts, nil
}

// itemResultParams turns a validated result into the row it is stored as, with
// every field the athlete said nothing about left invalid, which is the NULL
// the column takes.
func itemResultParams(insert itemResultInsert, sessionID, userID pgtype.UUID) db.CreateSessionItemResultParams {
	r := insert.request
	params := db.CreateSessionItemResultParams{
		SessionID:      sessionID,
		UserID:         userID,
		TrainingItemID: insert.itemID,
		Occurrence:     r.Occurrence,
	}
	if r.Reps != nil {
		params.Reps = pgtype.Int4{Int32: *r.Reps, Valid: true}
	}
	if r.Cycles != nil {
		params.Cycles = pgtype.Int4{Int32: *r.Cycles, Valid: true}
	}
	if r.LoadKg != nil {
		params.LoadKg = pgtype.Float4{Float32: *r.LoadKg, Valid: true}
	}
	if r.DurationSeconds != nil {
		params.DurationSeconds = pgtype.Int4{Int32: *r.DurationSeconds, Valid: true}
	}
	if r.Note != nil {
		params.Note = pgtype.Text{String: *r.Note, Valid: true}
	}
	return params
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
func dropStaleItemOverrides(ctx context.Context, qtx *db.Queries, athleteID, assessmentOwnerID pgtype.UUID, items []TrainingItemResponse, byItem map[string]json.RawMessage, programSession db.CoachProgramWeekSession) error {
	bases := make(map[string]TrainingItemRequest, len(byItem))
	collectOverriddenItems(items, byItem, bases)

	stale, err := staleOverrides(ctx, qtx, assessmentOwnerID, bases, byItem)
	if err != nil {
		return err
	}

	for id, reason := range stale {
		slog.Warn("dropping a program override the training item no longer takes",
			"user_id", athleteID.String(),
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
// @Description Create a new training session for the authenticated user with optional rep data and assessments. A session run from a prescription freezes it onto the session, with the program session overrides merged in, so later edits of the training cannot rewrite it. An override the training item no longer takes, because the training was edited after the week was prescribed, is dropped rather than merged, so what is frozen is never a shape the write paths refuse. When a program_session_id is sent, that row decides the training, and a training_id disagreeing with it is refused. A logged session may carry the link as well, so a coach slot with nothing to step through can be completed by hand; only a played one locks the coach's week. A run of a training the server cannot read, one Crimpy generates on the device, sends its own prescription instead, and the reps and the item reports name its items the same way; sending one alongside a training_id or a program_session_id is refused, since the server freezes its own copy from those. Such a prescription is held to 256 KB, must prescribe at least one item, and must name every item it holds with an id of at most 200 characters that no other item of it repeats. A rep may name the prescription item it was played from through training_item_id, which must be one of the items the session was prescribed. A rep whose step prescribed a load the run failed to measure sends target_unmeasured, so a client can tell it from a rep no target was ever expected for. That flag is what the clients grade on: such a rep is recorded with no target, and one sent with both is stored as it arrives and still read as unmeasured. A run may also send item_results, what the athlete reported about the items they were prescribed: the reps an AMRAP turned out to be, the rounds an emom was carried through, and for any step at all the load, the duration and the note nothing else records. Each names one of the prescribed items and an occurrence telling repeated passes apart, then whichever of reps, cycles, load_kg, duration_seconds and note it has something to say about; a field left out is stored as absent rather than as a zero. One result answers one pass, so two naming the same pass are refused, and one reporting nothing at all is dropped. A session may also carry the athlete's session RPE, how much recovery it cost: rpe is a value of the scale, 5 to 10, and rpe_failed is that scale's ECHEC, a session that could not be carried through. They are exclusive, and both are optional, since the value stays editable through the update endpoint long after the session.
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
	// Refused here as well as on the update path, so the server cannot store a
	// duration it would later refuse to be handed back.
	if req.Duration < 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Duration cannot be negative"})
	}
	// Checked here as well as on the bodyweight endpoint, because this value is
	// frozen into the prescription rather than stored in the series, so nothing
	// else would refuse a weight that is not one.
	if req.BodyweightKg != nil && !plausibleBodyweight(*req.BodyweightKg) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("bodyweight_kg must be above %d and at most %d", minBodyweightKg, maxBodyweightKg),
		})
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
	rpe, err := resolveSessionRPE(req.RPE, req.RPEFailed)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
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

	// A client that prescribed nothing may still spell the field, and every
	// other optional link in this handler reads a null as absent rather than
	// failing to parse over it.
	sentPrescription := req.Prescription
	if bytes.Equal(bytes.TrimSpace(sentPrescription), []byte("null")) {
		sentPrescription = nil
	}

	var prescription []byte
	prescribedItemIDs := map[string]struct{}{}
	switch {
	case trainingID.Valid:
		// The server can read the training, so it writes the snapshot itself and
		// a client copy would only be a second opinion about what was prescribed.
		if len(sentPrescription) > 0 {
			named := "a training"
			if programSession != nil {
				named = "a program session"
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("Prescription is not accepted on a session that names %s", named),
			})
		}
		snapshot, err := buildPrescriptionSnapshot(c.Context(), qtx, userUUID, trainingID, programSession, req.BodyweightKg)
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
	case len(sentPrescription) > 0:
		var err error
		prescription, prescribedItemIDs, err = clientPrescription(sentPrescription)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		// A run of a training the server cannot read still resolves percent_bw
		// loads against a weight, and that weight is as much a part of what was
		// prescribed here as on the path above. Without this the field is taken
		// from the request, checked, and then dropped, and the weight is gone
		// for good: it only ever lived on the device.
		//
		// The series is the last resort, not the second: a prescription that
		// already names the weight it was resolved against is the device saying
		// so, and overwriting it with what we happen to hold would record a
		// weight the loads were not read against, which is the whole thing this
		// is here to avoid.
		usedBodyweight := req.BodyweightKg
		if frozen := frozenPrescriptionBodyweight(prescription); frozen != nil {
			// Checked for the same reason the request field is: a weight the
			// prescription froze for itself is stored as it arrived, so this is
			// the only place that can refuse one no athlete could weigh.
			if !plausibleBodyweight(*frozen) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": fmt.Sprintf("resolved_against.bodyweight_kg must be above %d and at most %d", minBodyweightKg, maxBodyweightKg),
				})
			}
		} else if usedBodyweight == nil {
			usedBodyweight, err = latestBodyweight(c.Context(), qtx, userUUID)
			if err != nil {
				slog.Error("failed to read the bodyweight to freeze", "user_id", userID, "error", err)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
			}
		}
		prescription, err = withFrozenBodyweight(prescription, usedBodyweight)
		if err != nil {
			slog.Error("failed to freeze the bodyweight into the prescription", "user_id", userID, "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create session"})
		}
	}

	// Resolved before anything is written, so a rejected id costs a round trip
	// rather than a session insert and every rep before the bad one.
	repItemIDs, errIndex := resolveRepItemLinks(req.RepDatas, prescribedItemIDs, userID)
	if errIndex >= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("Invalid training item ID on rep %d", errIndex),
		})
	}

	itemResultInserts, itemResultErr := resolveItemResults(req.ItemResults, prescribedItemIDs, userID)
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
		Rpe:              rpe.value,
		RpeFailed:        rpe.failed,
	})
	if err != nil {
		// A retry of the same body would fail the same way, so the client is
		// told what to change rather than being handed a 500 it can only repeat.
		if storeRejectedInput(err) {
			slog.Warn("refusing a session the store cannot keep", "user_id", userID, "error", err)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Prescription holds something the store cannot keep",
			})
		}
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

	for _, insert := range itemResultInserts {
		_, err = qtx.CreateSessionItemResult(c.Context(), itemResultParams(insert, session.ID, userUUID))
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
// @Description Retrieve a specific session by ID with its rep data, assessments and what the athlete reported about the items they were prescribed: the count an AMRAP turned out to be, the rounds an emom was carried through, and for any step at all the load, the duration and the note nothing else records. Each assessment carries the weigh-in a bodyweight relative score is divided by, the last one taken at or before the session, with the date it was taken so a reader can tell a fresh denominator from a stale one. Both are absent when no weigh-in qualifies. User must own the session unless they are an admin.
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

	repDatas, assessments, itemResults := sessionDetailReads(c, h.queries, session.ID)

	return c.JSON(SessionDetailResponse{
		Session:     sessionToResponse(session),
		RepDatas:    repDatasToResponses(repDatas),
		Assessments: assessmentsToResponses(assessments),
		ItemResults: sessionItemResultsToResponses(itemResults),
	})
}

// UpdateSession godoc
// @Summary Update a session
// @Description Update a session's name, notes, duration, date and RPE. Every field is optional and every field left out keeps the value already stored, so a request may carry only what it means to change. A name sent as an empty string and a negative duration are refused rather than stored, the way the create path refuses them; not sending them at all is a different statement and keeps what is stored. The RPE pair counts as one field: sending neither rpe nor rpe_failed keeps the stored answer, and sending either replaces the whole answer, so rpe_failed false with no rpe beside it is how a rated session is taken back to unrated. User must own the session unless they are an admin.
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

	// A name and a duration are refused rather than stored when they say nothing
	// a session may hold, for the reason the create path refuses them. Not sent
	// at all is a different statement, and means the stored value is kept.
	if req.Name != nil && *req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name is required"})
	}
	if req.Duration != nil && *req.Duration < 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Duration cannot be negative"})
	}

	// The keeping is done by the statement rather than here, for every field,
	// so no read sits between what a request leaves out and the write.
	rpeGiven := req.RPE != nil || req.RPEFailed != nil
	rpe, err := resolveSessionRPE(req.RPE, req.RPEFailed)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var date pgtype.Timestamptz
	if req.Date != nil {
		t, err := parseSessionDate(*req.Date)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid date format"})
		}
		date = pgtype.Timestamptz{Time: t, Valid: true}
	}

	updated, err := h.queries.UpdateSession(c.Context(), db.UpdateSessionParams{
		ID:        sessionUUID,
		Name:      toPgText(req.Name),
		Notes:     toPgText(req.Notes),
		Duration:  toPgInt4(req.Duration),
		Date:      date,
		RpeGiven:  rpeGiven,
		Rpe:       rpe.value,
		RpeFailed: rpe.failed,
	})
	if err != nil {
		slog.Error("failed to update session", "session_id", sessionUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update session"})
	}

	// An update touches the name, the notes, the duration and the RPE, and never
	// the curve. Echoing it back would ship the bulkiest thing the row holds
	// down the wire to answer a rename, for a client that reads none of it.
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
