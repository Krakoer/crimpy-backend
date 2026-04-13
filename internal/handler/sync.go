package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type SyncHandler struct {
	queries *db.Queries
}

func NewSyncHandler(queries *db.Queries) *SyncHandler {
	return &SyncHandler{
		queries: queries,
	}
}

type SyncSummaryResponse struct {
	UserID          string           `json:"user_id"`
	Collections     map[string]int64 `json:"collections"`
	LastSyncVersion int64            `json:"last_sync_version"`
}

type PullResponse struct {
	ServerVersion int64                  `json:"server_version"`
	Records       map[string]interface{} `json:"records"`
}

type PushRequest struct {
	Records map[string][]interface{} `json:"records"`
}

type PushResponse struct {
	ServerVersion int64    `json:"server_version"`
	Accepted      []string `json:"accepted"`
	Rejected      []string `json:"rejected"`
	Conflicts     []string `json:"conflicts"`
}

type SessionRecord struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Notes             string  `json:"notes"`
	Date              string  `json:"date"`
	IsAssessment      bool    `json:"is_assessment"`
	SessionType       int32   `json:"session_type"`
	Duration          int32   `json:"duration"`
	RepeaterSets      *int32  `json:"repeater_sets"`
	RepeaterReps      *int32  `json:"repeater_reps"`
	RepeaterWorkTime  *int32  `json:"repeater_work_time"`
	RepeaterRestTime  *int32  `json:"repeater_rest_time"`
	RepeaterSetRest   *int32  `json:"repeater_set_rest"`
	RepeaterSplitHand *bool   `json:"repeater_split_hand"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	DeletedAt         *string `json:"deleted_at"`
	DeviceID          *string `json:"device_id"`
	SyncVersion       int64   `json:"sync_version"`
	ServerUpdatedAt   string  `json:"server_updated_at"`
}

type AssessmentRecord struct {
	ID              string   `json:"id"`
	Type            int32    `json:"type"`
	RightValue      *float32 `json:"right_value"`
	LeftValue       *float32 `json:"left_value"`
	SessionID       string   `json:"session_id"`
	GripPosition    int32    `json:"grip_position"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	DeletedAt       *string  `json:"deleted_at"`
	DeviceID        *string  `json:"device_id"`
	SyncVersion     int64    `json:"sync_version"`
	ServerUpdatedAt string   `json:"server_updated_at"`
}

type TrainingRecord struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	RepeaterID      *string `json:"repeater_id"`
	IsFavorite      bool    `json:"is_favorite"`
	IsAssessment    bool    `json:"is_assessment"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	DeletedAt       *string `json:"deleted_at"`
	DeviceID        *string `json:"device_id"`
	SyncVersion     int64   `json:"sync_version"`
	ServerUpdatedAt string  `json:"server_updated_at"`
}

type RepeaterRecord struct {
	ID                string   `json:"id"`
	Sets              int32    `json:"sets"`
	Reps              int32    `json:"reps"`
	Worktime          int32    `json:"worktime"`
	Resttime          int32    `json:"resttime"`
	SetRest           int32    `json:"set_rest"`
	TargetWeightRight *float32 `json:"target_weight_right"`
	TargetWeightLeft  *float32 `json:"target_weight_left"`
	SplitHand         bool     `json:"split_hand"`
	GripPosition      int32    `json:"grip_position"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
	DeletedAt         *string  `json:"deleted_at"`
	DeviceID          *string  `json:"device_id"`
	SyncVersion       int64    `json:"sync_version"`
	ServerUpdatedAt   string   `json:"server_updated_at"`
}

type RepTemplateRecord struct {
	ID              string  `json:"id"`
	TrainingID      string  `json:"training_id"`
	IsRest          bool    `json:"is_rest"`
	RightHand       bool    `json:"right_hand"`
	Duration        int32   `json:"duration"`
	TargetWeight    float32 `json:"target_weight"`
	Index           int32   `json:"index"`
	GripPosition    int32   `json:"grip_position"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	DeletedAt       *string `json:"deleted_at"`
	DeviceID        *string `json:"device_id"`
	SyncVersion     int64   `json:"sync_version"`
	ServerUpdatedAt string  `json:"server_updated_at"`
}

type RepDataRecord struct {
	ID              string  `json:"id"`
	SessionID       string  `json:"session_id"`
	AverageWeight   float32 `json:"average_weight"`
	IsRest          bool    `json:"is_rest"`
	RightHand       bool    `json:"right_hand"`
	Duration        int32   `json:"duration"`
	TargetWeight    float32 `json:"target_weight"`
	Index           int32   `json:"index"`
	GripPosition    int32   `json:"grip_position"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	DeletedAt       *string `json:"deleted_at"`
	DeviceID        *string `json:"device_id"`
	SyncVersion     int64   `json:"sync_version"`
	ServerUpdatedAt string  `json:"server_updated_at"`
}

func (h *SyncHandler) GetSummary(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	summary, err := h.queries.GetSyncSummary(context.Background(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get sync summary",
		})
	}

	err = h.queries.UpdateUserLastSeen(context.Background(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update last seen",
		})
	}

	lastSyncVersion := int64(0)
	if summary.LastSyncVersion != nil {
		if v, ok := summary.LastSyncVersion.(int64); ok {
			lastSyncVersion = v
		}
	}

	return c.Status(fiber.StatusOK).JSON(SyncSummaryResponse{
		UserID: userID,
		Collections: map[string]int64{
			"sessions":      summary.SessionsCount,
			"assessments":   summary.AssessmentsCount,
			"trainings":     summary.TrainingsCount,
			"repeaters":     summary.RepeatersCount,
			"rep_templates": summary.RepTemplatesCount,
			"rep_datas":     summary.RepDatasCount,
		},
		LastSyncVersion: lastSyncVersion,
	})
}

func (h *SyncHandler) Pull(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	sinceVersionStr := c.Query("since_version", "0")
	sinceVersion := int64(0)
	if v, err := strconv.ParseInt(sinceVersionStr, 10, 64); err == nil {
		sinceVersion = v
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	sessions, err := h.queries.GetSessionsSinceVersion(context.Background(), db.GetSessionsSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch sessions",
		})
	}

	assessments, err := h.queries.GetAssessmentsSinceVersion(context.Background(), db.GetAssessmentsSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch assessments",
		})
	}

	trainings, err := h.queries.GetTrainingsSinceVersion(context.Background(), db.GetTrainingsSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch trainings",
		})
	}

	repeaters, err := h.queries.GetRepeatersSinceVersion(context.Background(), db.GetRepeatersSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch repeaters",
		})
	}

	repTemplates, err := h.queries.GetRepTemplatesSinceVersion(context.Background(), db.GetRepTemplatesSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch rep templates",
		})
	}

	repDatas, err := h.queries.GetRepDatasSinceVersion(context.Background(), db.GetRepDatasSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch rep datas",
		})
	}

	summary, err := h.queries.GetSyncSummary(context.Background(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get sync summary",
		})
	}

	sessionRecords := make([]SessionRecord, len(sessions))
	for i, s := range sessions {
		sessionRecords[i] = convertSessionToRecord(s)
	}

	assessmentRecords := make([]AssessmentRecord, len(assessments))
	for i, a := range assessments {
		assessmentRecords[i] = convertAssessmentToRecord(a)
	}

	trainingRecords := make([]TrainingRecord, len(trainings))
	for i, t := range trainings {
		trainingRecords[i] = convertTrainingToRecord(t)
	}

	repeaterRecords := make([]RepeaterRecord, len(repeaters))
	for i, r := range repeaters {
		repeaterRecords[i] = convertRepeaterToRecord(r)
	}

	repTemplateRecords := make([]RepTemplateRecord, len(repTemplates))
	for i, rt := range repTemplates {
		repTemplateRecords[i] = convertRepTemplateToRecord(rt)
	}

	repDataRecords := make([]RepDataRecord, len(repDatas))
	for i, rd := range repDatas {
		repDataRecords[i] = convertRepDataToRecord(rd)
	}

	lastSyncVersion := int64(0)
	if summary.LastSyncVersion != nil {
		if v, ok := summary.LastSyncVersion.(int64); ok {
			lastSyncVersion = v
		}
	}

	return c.Status(fiber.StatusOK).JSON(PullResponse{
		ServerVersion: lastSyncVersion,
		Records: map[string]interface{}{
			"sessions":      sessionRecords,
			"assessments":   assessmentRecords,
			"trainings":     trainingRecords,
			"repeaters":     repeaterRecords,
			"rep_templates": repTemplateRecords,
			"rep_datas":     repDataRecords,
		},
	})
}

func (h *SyncHandler) Push(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	var req PushRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	accepted := []string{}
	rejected := []string{}

	summary, err := h.queries.GetSyncSummary(context.Background(), userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get sync summary",
		})
	}

	lastSyncVersion := int64(0)
	if summary.LastSyncVersion != nil {
		if v, ok := summary.LastSyncVersion.(int64); ok {
			lastSyncVersion = v
		}
	}

	return c.Status(fiber.StatusOK).JSON(PushResponse{
		ServerVersion: lastSyncVersion,
		Accepted:      accepted,
		Rejected:      rejected,
		Conflicts:     []string{},
	})
}

func (h *SyncHandler) Migrate(c fiber.Ctx) error {
	return h.Push(c)
}

func convertSessionToRecord(s db.Session) SessionRecord {
	var deletedAt *string
	if s.DeletedAt.Valid {
		t := s.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return SessionRecord{
		ID:                uuidToString(s.ID),
		Name:              s.Name,
		Notes:             s.Notes,
		Date:              s.Date.Time.Format(time.RFC3339),
		IsAssessment:      s.IsAssessment,
		SessionType:       s.SessionType,
		Duration:          s.Duration,
		RepeaterSets:      convertInt32Ptr(s.RepeaterSets),
		RepeaterReps:      convertInt32Ptr(s.RepeaterReps),
		RepeaterWorkTime:  convertInt32Ptr(s.RepeaterWorkTime),
		RepeaterRestTime:  convertInt32Ptr(s.RepeaterRestTime),
		RepeaterSetRest:   convertInt32Ptr(s.RepeaterSetRest),
		RepeaterSplitHand: convertBoolPtr(s.RepeaterSplitHand),
		CreatedAt:         s.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         s.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:         deletedAt,
		DeviceID:          uuidPtrToString(s.DeviceID),
		SyncVersion:       s.SyncVersion,
		ServerUpdatedAt:   s.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertAssessmentToRecord(a db.Assessment) AssessmentRecord {
	var deletedAt *string
	if a.DeletedAt.Valid {
		t := a.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return AssessmentRecord{
		ID:              uuidToString(a.ID),
		Type:            a.Type,
		RightValue:      convertFloat32Ptr(a.RightValue),
		LeftValue:       convertFloat32Ptr(a.LeftValue),
		SessionID:       uuidToString(a.SessionID),
		GripPosition:    a.GripPosition.Int32,
		CreatedAt:       a.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       a.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
		DeviceID:        uuidPtrToString(a.DeviceID),
		SyncVersion:     a.SyncVersion,
		ServerUpdatedAt: a.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertTrainingToRecord(t db.Training) TrainingRecord {
	var deletedAt *string
	if t.DeletedAt.Valid {
		dt := t.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &dt
	}

	return TrainingRecord{
		ID:              uuidToString(t.ID),
		Name:            t.Name,
		RepeaterID:      uuidPtrToString(t.RepeaterID),
		IsFavorite:      t.IsFavorite,
		IsAssessment:    t.IsAssessment,
		CreatedAt:       t.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       t.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
		DeviceID:        uuidPtrToString(t.DeviceID),
		SyncVersion:     t.SyncVersion,
		ServerUpdatedAt: t.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertRepeaterToRecord(r db.Repeater) RepeaterRecord {
	var deletedAt *string
	if r.DeletedAt.Valid {
		t := r.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return RepeaterRecord{
		ID:                uuidToString(r.ID),
		Sets:              r.Sets,
		Reps:              r.Reps,
		Worktime:          r.Worktime,
		Resttime:          r.Resttime,
		SetRest:           r.SetRest,
		TargetWeightRight: convertFloat32Ptr(r.TargetWeightRight),
		TargetWeightLeft:  convertFloat32Ptr(r.TargetWeightLeft),
		SplitHand:         r.SplitHand,
		GripPosition:      r.GripPosition,
		CreatedAt:         r.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:         r.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:         deletedAt,
		DeviceID:          uuidPtrToString(r.DeviceID),
		SyncVersion:       r.SyncVersion,
		ServerUpdatedAt:   r.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertRepTemplateToRecord(rt db.RepTemplate) RepTemplateRecord {
	var deletedAt *string
	if rt.DeletedAt.Valid {
		t := rt.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return RepTemplateRecord{
		ID:              uuidToString(rt.ID),
		TrainingID:      uuidToString(rt.TrainingID),
		IsRest:          rt.IsRest,
		RightHand:       rt.RightHand,
		Duration:        rt.Duration,
		TargetWeight:    rt.TargetWeight,
		Index:           rt.Index,
		GripPosition:    rt.GripPosition,
		CreatedAt:       rt.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       rt.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
		DeviceID:        uuidPtrToString(rt.DeviceID),
		SyncVersion:     rt.SyncVersion,
		ServerUpdatedAt: rt.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertRepDataToRecord(rd db.RepData) RepDataRecord {
	var deletedAt *string
	if rd.DeletedAt.Valid {
		t := rd.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return RepDataRecord{
		ID:              uuidToString(rd.ID),
		SessionID:       uuidToString(rd.SessionID),
		AverageWeight:   rd.AverageWeight,
		IsRest:          rd.IsRest,
		RightHand:       rd.RightHand,
		Duration:        rd.Duration,
		TargetWeight:    rd.TargetWeight,
		Index:           rd.Index,
		GripPosition:    rd.GripPosition,
		CreatedAt:       rd.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       rd.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
		DeviceID:        uuidPtrToString(rd.DeviceID),
		SyncVersion:     rd.SyncVersion,
		ServerUpdatedAt: rd.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertInt32Ptr(v pgtype.Int4) *int32 {
	if v.Valid {
		return &v.Int32
	}
	return nil
}

func convertFloat32Ptr(v pgtype.Float4) *float32 {
	if v.Valid {
		return &v.Float32
	}
	return nil
}

func convertBoolPtr(v pgtype.Bool) *bool {
	if v.Valid {
		return &v.Bool
	}
	return nil
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

func uuidPtrToString(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := uuid.UUID(u.Bytes).String()
	return &s
}
