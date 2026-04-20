package sync

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"strconv"

	"github.com/gofiber/fiber/v3"
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

// GetSummary godoc
// @Summary Get sync summary
// @Description Returns count of records per collection and last sync version for the authenticated user
// @Tags sync
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} SyncSummaryResponse "Sync summary"
// @Failure 400 {object} map[string]string "Invalid user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/summary [get]
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
			"sessions":                 summary.SessionsCount,
			"assessments":              summary.AssessmentsCount,
			"trainings":                summary.TrainingsCount,
			"repeaters":                summary.RepeatersCount,
			"rep_templates":            summary.RepTemplatesCount,
			"rep_datas":                summary.RepDatasCount,
			"pinned_builtin_trainings": summary.PinnedBuiltinTrainingsCount,
			"sensor_configs":           summary.SensorConfigsCount,
			"builtin_training_weights": summary.BuiltinTrainingWeightsCount,
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
// @Param since_version query integer false "Sync version to pull changes since" default(0)
// @Success 200 {object} PullResponse "Records and current server version"
// @Failure 400 {object} map[string]string "Invalid user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/pull [get]
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

	pinnedBuiltinTrainings, err := h.queries.GetPinnedBuiltinTrainingsSinceVersion(context.Background(), db.GetPinnedBuiltinTrainingsSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch pinned builtin trainings",
		})
	}

	sensorConfigs, err := h.queries.GetSensorConfigsSinceVersion(context.Background(), db.GetSensorConfigsSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch sensor configs",
		})
	}

	builtinTrainingWeights, err := h.queries.GetBuiltinTrainingWeightsSinceVersion(context.Background(), db.GetBuiltinTrainingWeightsSinceVersionParams{
		Column1: userUUID,
		Column2: int64(sinceVersion),
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch builtin training weights",
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

	pinnedBuiltinTrainingRecords := make([]PinnedBuiltinTrainingRecord, len(pinnedBuiltinTrainings))
	for i, pbt := range pinnedBuiltinTrainings {
		pinnedBuiltinTrainingRecords[i] = convertPinnedBuiltinTrainingToRecord(pbt)
	}

	sensorConfigRecords := make([]SensorConfigRecord, len(sensorConfigs))
	for i, sc := range sensorConfigs {
		sensorConfigRecords[i] = convertSensorConfigToRecord(sc)
	}

	builtinTrainingWeightRecords := make([]BuiltinTrainingWeightRecord, len(builtinTrainingWeights))
	for i, btw := range builtinTrainingWeights {
		builtinTrainingWeightRecords[i] = convertBuiltinTrainingWeightToRecord(btw)
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
			"sessions":                 sessionRecords,
			"assessments":              assessmentRecords,
			"trainings":                trainingRecords,
			"repeaters":                repeaterRecords,
			"rep_templates":            repTemplateRecords,
			"rep_datas":                repDataRecords,
			"pinned_builtin_trainings": pinnedBuiltinTrainingRecords,
			"sensor_configs":           sensorConfigRecords,
			"builtin_training_weights": builtinTrainingWeightRecords,
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
// @Param request body PushRequest true "Records to push"
// @Success 200 {object} PushResponse "Push results with accepted and rejected IDs"
// @Failure 400 {object} map[string]string "Invalid request body or user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/push [post]
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

	if pinnedBuiltinTrainingsData, ok := req.Records["pinned_builtin_trainings"]; ok {
		if pinnedBuiltinTrainings, ok := pinnedBuiltinTrainingsData.([]interface{}); ok {
			for _, pbt := range pinnedBuiltinTrainings {
				recordID, err := h.upsertPinnedBuiltinTraining(ctx, userUUID, pbt)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	if sensorConfigsData, ok := req.Records["sensor_configs"]; ok {
		if sensorConfigs, ok := sensorConfigsData.([]interface{}); ok {
			for _, sc := range sensorConfigs {
				recordID, err := h.upsertSensorConfig(ctx, userUUID, sc)
				if err != nil {
					rejected = append(rejected, recordID)
				} else {
					accepted = append(accepted, recordID)
				}
			}
		}
	}

	if builtinTrainingWeightsData, ok := req.Records["builtin_training_weights"]; ok {
		if builtinTrainingWeights, ok := builtinTrainingWeightsData.([]interface{}); ok {
			for _, btw := range builtinTrainingWeights {
				recordID, err := h.upsertBuiltinTrainingWeight(ctx, userUUID, btw)
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
	})
}

// Migrate godoc
// @Summary Migrate local data to cloud on first sync
// @Description Bulk uploads all local data when cloud account is empty (called once at first login)
// @Tags sync
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body PushRequest true "All local records to migrate"
// @Success 200 {object} PushResponse "Migration results with accepted and rejected IDs"
// @Failure 400 {object} map[string]string "Invalid request body or user ID"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/sync/migrate [post]
func (h *SyncHandler) Migrate(c fiber.Ctx) error {
	return h.Push(c)
}
