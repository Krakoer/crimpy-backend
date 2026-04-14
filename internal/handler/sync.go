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
	Records map[string]interface{} `json:"records"`
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

// GetSummary godoc
// @Summary Get sync summary
// @Description Returns count of records per collection and last sync version for the authenticated user
// @Tags sync
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Device-ID header string false "Device ID (UUID)"
// @Success 200 {object} SyncSummaryResponse "Sync summary"
// @Failure 400 {object} map[string]string "Invalid user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/summary [get]
func (h *SyncHandler) GetSummary(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	if err := validateDeviceID(c); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

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

// Pull godoc
// @Summary Pull sync changes
// @Description Returns all records modified after the specified sync version for incremental sync
// @Tags sync
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Device-ID header string false "Device ID (UUID)"
// @Param since_version query integer false "Sync version to pull changes since" default(0)
// @Success 200 {object} PullResponse "Records and current server version"
// @Failure 400 {object} map[string]string "Invalid user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/pull [get]
func (h *SyncHandler) Pull(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	if err := validateDeviceID(c); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

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

// Push godoc
// @Summary Push local changes to cloud
// @Description Accepts a batch of locally changed records and applies last-write-wins conflict resolution
// @Tags sync
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Device-ID header string false "Device ID (UUID)"
// @Param request body PushRequest true "Records to push"
// @Success 200 {object} PushResponse "Push results with accepted and rejected IDs"
// @Failure 400 {object} map[string]string "Invalid request body or user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/push [post]
func (h *SyncHandler) Push(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	if err := validateDeviceID(c); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

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

	ctx := context.Background()
	accepted := []string{}
	rejected := []string{}

	if sessionsData, ok := req.Records["sessions"]; ok {
		if sessions, ok := sessionsData.([]interface{}); ok {
			for _, s := range sessions {
				recordID, err := h.upsertSession(ctx, userUUID, s)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	if assessmentsData, ok := req.Records["assessments"]; ok {
		if assessments, ok := assessmentsData.([]interface{}); ok {
			for _, a := range assessments {
				recordID, err := h.upsertAssessment(ctx, userUUID, a)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	if trainingsData, ok := req.Records["trainings"]; ok {
		if trainings, ok := trainingsData.([]interface{}); ok {
			for _, t := range trainings {
				recordID, err := h.upsertTraining(ctx, userUUID, t)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	if repeatersData, ok := req.Records["repeaters"]; ok {
		if repeaters, ok := repeatersData.([]interface{}); ok {
			for _, r := range repeaters {
				recordID, err := h.upsertRepeater(ctx, userUUID, r)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	if repTemplatesData, ok := req.Records["rep_templates"]; ok {
		if repTemplates, ok := repTemplatesData.([]interface{}); ok {
			for _, rt := range repTemplates {
				recordID, err := h.upsertRepTemplate(ctx, userUUID, rt)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	if repDatasData, ok := req.Records["rep_datas"]; ok {
		if repDatas, ok := repDatasData.([]interface{}); ok {
			for _, rd := range repDatas {
				recordID, err := h.upsertRepData(ctx, userUUID, rd)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	summary, err := h.queries.GetSyncSummary(ctx, userUUID)
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

// Migrate godoc
// @Summary Migrate local data to cloud on first sync
// @Description Bulk uploads all local data when cloud account is empty (called once at first login)
// @Tags sync
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param X-Device-ID header string false "Device ID (UUID)"
// @Param request body PushRequest true "All local records to migrate"
// @Success 200 {object} PushResponse "Migration results with accepted and rejected IDs"
// @Failure 400 {object} map[string]string "Invalid request body or user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/migrate [post]
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

func parseTimestamp(s string) (pgtype.Timestamptz, error) {
	var ts pgtype.Timestamptz
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ts, err
	}
	ts.Time = t
	ts.Valid = true
	return ts, nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, err
	}
	return u, nil
}

func validateDeviceID(c fiber.Ctx) error {
	deviceID := c.Get("X-Device-ID")
	if deviceID == "" {
		return nil
	}

	var u pgtype.UUID
	if err := u.Scan(deviceID); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid device ID format")
	}
	return nil
}

func (h *SyncHandler) upsertSession(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid session record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing session ID")
	}

	sessionUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	name, _ := m["name"].(string)
	notes, _ := m["notes"].(string)
	dateStr, _ := m["date"].(string)
	isAssessment, _ := m["is_assessment"].(bool)
	sessionType := int32(m["session_type"].(float64))
	duration := int32(m["duration"].(float64))

	date, err := parseTimestamp(dateStr)
	if err != nil {
		return id, err
	}

	createdAtStr, _ := m["created_at"].(string)
	createdAt, err := parseTimestamp(createdAtStr)
	if err != nil {
		return id, err
	}

	updatedAtStr, _ := m["updated_at"].(string)
	updatedAt, err := parseTimestamp(updatedAtStr)
	if err != nil {
		return id, err
	}

	var deletedAt pgtype.Timestamptz
	if deletedAtStr, ok := m["deleted_at"].(string); ok && deletedAtStr != "" {
		deletedAt, err = parseTimestamp(deletedAtStr)
		if err != nil {
			return id, err
		}
	}

	var deviceID pgtype.UUID
	if deviceIDStr, ok := m["device_id"].(string); ok && deviceIDStr != "" {
		deviceID, _ = parseUUID(deviceIDStr)
	}

	var repeaterSets, repeaterReps, repeaterWorkTime, repeaterRestTime, repeaterSetRest pgtype.Int4
	if v, ok := m["repeater_sets"].(float64); ok {
		repeaterSets.Int32 = int32(v)
		repeaterSets.Valid = true
	}
	if v, ok := m["repeater_reps"].(float64); ok {
		repeaterReps.Int32 = int32(v)
		repeaterReps.Valid = true
	}
	if v, ok := m["repeater_work_time"].(float64); ok {
		repeaterWorkTime.Int32 = int32(v)
		repeaterWorkTime.Valid = true
	}
	if v, ok := m["repeater_rest_time"].(float64); ok {
		repeaterRestTime.Int32 = int32(v)
		repeaterRestTime.Valid = true
	}
	if v, ok := m["repeater_set_rest"].(float64); ok {
		repeaterSetRest.Int32 = int32(v)
		repeaterSetRest.Valid = true
	}

	var repeaterSplitHand pgtype.Bool
	if v, ok := m["repeater_split_hand"].(bool); ok {
		repeaterSplitHand.Bool = v
		repeaterSplitHand.Valid = true
	}

	_, err = h.queries.UpsertSession(ctx, db.UpsertSessionParams{
		ID:                sessionUUID,
		UserID:            userUUID,
		Name:              name,
		Notes:             notes,
		Date:              date,
		IsAssessment:      isAssessment,
		SessionType:       sessionType,
		Duration:          duration,
		RepeaterSets:      repeaterSets,
		RepeaterReps:      repeaterReps,
		RepeaterWorkTime:  repeaterWorkTime,
		RepeaterRestTime:  repeaterRestTime,
		RepeaterSetRest:   repeaterSetRest,
		RepeaterSplitHand: repeaterSplitHand,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		DeletedAt:         deletedAt,
		DeviceID:          deviceID,
	})
	if err != nil {
		return id, err
	}

	return id, nil
}

func (h *SyncHandler) upsertAssessment(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid assessment record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing assessment ID")
	}

	assessmentUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	typeVal := int32(m["type"].(float64))
	sessionIDStr, _ := m["session_id"].(string)
	sessionID, err := parseUUID(sessionIDStr)
	if err != nil {
		return id, err
	}

	gripPosition := int32(m["grip_position"].(float64))

	var rightValue, leftValue pgtype.Float4
	if v, ok := m["right_value"].(float64); ok {
		rightValue.Float32 = float32(v)
		rightValue.Valid = true
	}
	if v, ok := m["left_value"].(float64); ok {
		leftValue.Float32 = float32(v)
		leftValue.Valid = true
	}

	createdAtStr, _ := m["created_at"].(string)
	createdAt, err := parseTimestamp(createdAtStr)
	if err != nil {
		return id, err
	}

	updatedAtStr, _ := m["updated_at"].(string)
	updatedAt, err := parseTimestamp(updatedAtStr)
	if err != nil {
		return id, err
	}

	var deletedAt pgtype.Timestamptz
	if deletedAtStr, ok := m["deleted_at"].(string); ok && deletedAtStr != "" {
		deletedAt, err = parseTimestamp(deletedAtStr)
		if err != nil {
			return id, err
		}
	}

	var deviceID pgtype.UUID
	if deviceIDStr, ok := m["device_id"].(string); ok && deviceIDStr != "" {
		deviceID, _ = parseUUID(deviceIDStr)
	}

	var gripPos pgtype.Int4
	gripPos.Int32 = gripPosition
	gripPos.Valid = true

	_, err = h.queries.UpsertAssessment(ctx, db.UpsertAssessmentParams{
		ID:           assessmentUUID,
		UserID:       userUUID,
		Type:         typeVal,
		RightValue:   rightValue,
		LeftValue:    leftValue,
		SessionID:    sessionID,
		GripPosition: gripPos,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		DeletedAt:    deletedAt,
		DeviceID:     deviceID,
	})
	if err != nil {
		return id, err
	}

	return id, nil
}

func (h *SyncHandler) upsertTraining(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid training record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing training ID")
	}

	trainingUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	name, _ := m["name"].(string)
	isFavorite, _ := m["is_favorite"].(bool)
	isAssessment, _ := m["is_assessment"].(bool)

	var repeaterID pgtype.UUID
	if repeaterIDStr, ok := m["repeater_id"].(string); ok && repeaterIDStr != "" {
		repeaterID, _ = parseUUID(repeaterIDStr)
	}

	createdAtStr, _ := m["created_at"].(string)
	createdAt, err := parseTimestamp(createdAtStr)
	if err != nil {
		return id, err
	}

	updatedAtStr, _ := m["updated_at"].(string)
	updatedAt, err := parseTimestamp(updatedAtStr)
	if err != nil {
		return id, err
	}

	var deletedAt pgtype.Timestamptz
	if deletedAtStr, ok := m["deleted_at"].(string); ok && deletedAtStr != "" {
		deletedAt, err = parseTimestamp(deletedAtStr)
		if err != nil {
			return id, err
		}
	}

	var deviceID pgtype.UUID
	if deviceIDStr, ok := m["device_id"].(string); ok && deviceIDStr != "" {
		deviceID, _ = parseUUID(deviceIDStr)
	}

	_, err = h.queries.UpsertTraining(ctx, db.UpsertTrainingParams{
		ID:           trainingUUID,
		UserID:       userUUID,
		Name:         name,
		RepeaterID:   repeaterID,
		IsFavorite:   isFavorite,
		IsAssessment: isAssessment,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		DeletedAt:    deletedAt,
		DeviceID:     deviceID,
	})
	if err != nil {
		return id, err
	}

	return id, nil
}

func (h *SyncHandler) upsertRepeater(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid repeater record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing repeater ID")
	}

	repeaterUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	sets := int32(m["sets"].(float64))
	reps := int32(m["reps"].(float64))
	worktime := int32(m["worktime"].(float64))
	resttime := int32(m["resttime"].(float64))
	setRest := int32(m["set_rest"].(float64))
	splitHand, _ := m["split_hand"].(bool)
	gripPosition := int32(m["grip_position"].(float64))

	var targetWeightRight, targetWeightLeft pgtype.Float4
	if v, ok := m["target_weight_right"].(float64); ok {
		targetWeightRight.Float32 = float32(v)
		targetWeightRight.Valid = true
	}
	if v, ok := m["target_weight_left"].(float64); ok {
		targetWeightLeft.Float32 = float32(v)
		targetWeightLeft.Valid = true
	}

	createdAtStr, _ := m["created_at"].(string)
	createdAt, err := parseTimestamp(createdAtStr)
	if err != nil {
		return id, err
	}

	updatedAtStr, _ := m["updated_at"].(string)
	updatedAt, err := parseTimestamp(updatedAtStr)
	if err != nil {
		return id, err
	}

	var deletedAt pgtype.Timestamptz
	if deletedAtStr, ok := m["deleted_at"].(string); ok && deletedAtStr != "" {
		deletedAt, err = parseTimestamp(deletedAtStr)
		if err != nil {
			return id, err
		}
	}

	var deviceID pgtype.UUID
	if deviceIDStr, ok := m["device_id"].(string); ok && deviceIDStr != "" {
		deviceID, _ = parseUUID(deviceIDStr)
	}

	_, err = h.queries.UpsertRepeater(ctx, db.UpsertRepeaterParams{
		ID:                repeaterUUID,
		UserID:            userUUID,
		Sets:              sets,
		Reps:              reps,
		Worktime:          worktime,
		Resttime:          resttime,
		SetRest:           setRest,
		TargetWeightRight: targetWeightRight,
		TargetWeightLeft:  targetWeightLeft,
		SplitHand:         splitHand,
		GripPosition:      gripPosition,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		DeletedAt:         deletedAt,
		DeviceID:          deviceID,
	})
	if err != nil {
		return id, err
	}

	return id, nil
}

func (h *SyncHandler) upsertRepTemplate(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid rep template record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing rep template ID")
	}

	repTemplateUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	trainingIDStr, _ := m["training_id"].(string)
	trainingID, err := parseUUID(trainingIDStr)
	if err != nil {
		return id, err
	}

	isRest, _ := m["is_rest"].(bool)
	rightHand, _ := m["right_hand"].(bool)
	duration := int32(m["duration"].(float64))
	targetWeight := float32(m["target_weight"].(float64))
	index := int32(m["index"].(float64))
	gripPosition := int32(m["grip_position"].(float64))

	createdAtStr, _ := m["created_at"].(string)
	createdAt, err := parseTimestamp(createdAtStr)
	if err != nil {
		return id, err
	}

	updatedAtStr, _ := m["updated_at"].(string)
	updatedAt, err := parseTimestamp(updatedAtStr)
	if err != nil {
		return id, err
	}

	var deletedAt pgtype.Timestamptz
	if deletedAtStr, ok := m["deleted_at"].(string); ok && deletedAtStr != "" {
		deletedAt, err = parseTimestamp(deletedAtStr)
		if err != nil {
			return id, err
		}
	}

	var deviceID pgtype.UUID
	if deviceIDStr, ok := m["device_id"].(string); ok && deviceIDStr != "" {
		deviceID, _ = parseUUID(deviceIDStr)
	}

	_, err = h.queries.UpsertRepTemplate(ctx, db.UpsertRepTemplateParams{
		ID:           repTemplateUUID,
		UserID:       userUUID,
		TrainingID:   trainingID,
		IsRest:       isRest,
		RightHand:    rightHand,
		Duration:     duration,
		TargetWeight: targetWeight,
		Index:        index,
		GripPosition: gripPosition,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		DeletedAt:    deletedAt,
		DeviceID:     deviceID,
	})
	if err != nil {
		return id, err
	}

	return id, nil
}

func (h *SyncHandler) upsertRepData(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid rep data record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing rep data ID")
	}

	repDataUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	sessionIDStr, _ := m["session_id"].(string)
	sessionID, err := parseUUID(sessionIDStr)
	if err != nil {
		return id, err
	}

	averageWeight := float32(m["average_weight"].(float64))
	isRest, _ := m["is_rest"].(bool)
	rightHand, _ := m["right_hand"].(bool)
	duration := int32(m["duration"].(float64))
	targetWeight := float32(m["target_weight"].(float64))
	index := int32(m["index"].(float64))
	gripPosition := int32(m["grip_position"].(float64))

	createdAtStr, _ := m["created_at"].(string)
	createdAt, err := parseTimestamp(createdAtStr)
	if err != nil {
		return id, err
	}

	updatedAtStr, _ := m["updated_at"].(string)
	updatedAt, err := parseTimestamp(updatedAtStr)
	if err != nil {
		return id, err
	}

	var deletedAt pgtype.Timestamptz
	if deletedAtStr, ok := m["deleted_at"].(string); ok && deletedAtStr != "" {
		deletedAt, err = parseTimestamp(deletedAtStr)
		if err != nil {
			return id, err
		}
	}

	var deviceID pgtype.UUID
	if deviceIDStr, ok := m["device_id"].(string); ok && deviceIDStr != "" {
		deviceID, _ = parseUUID(deviceIDStr)
	}

	_, err = h.queries.UpsertRepData(ctx, db.UpsertRepDataParams{
		ID:            repDataUUID,
		UserID:        userUUID,
		SessionID:     sessionID,
		AverageWeight: averageWeight,
		IsRest:        isRest,
		RightHand:     rightHand,
		Duration:      duration,
		TargetWeight:  targetWeight,
		Index:         index,
		GripPosition:  gripPosition,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		DeletedAt:     deletedAt,
		DeviceID:      deviceID,
	})
	if err != nil {
		return id, err
	}

	return id, nil
}
