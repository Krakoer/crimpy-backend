package sync

import (
	"context"
	"crimpy/backend/internal/db"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgtype"
)

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

	slog.Debug("upserting session", "id", id)
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
		UpdatedAt:         updatedAt,
		DeletedAt:         deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert session", "id", id, "error", err)
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

	var gripPos pgtype.Int4
	gripPos.Int32 = gripPosition
	gripPos.Valid = true

	slog.Debug("upserting assessment", "id", id)
	_, err = h.queries.UpsertAssessment(ctx, db.UpsertAssessmentParams{
		ID:           assessmentUUID,
		UserID:       userUUID,
		Type:         typeVal,
		RightValue:   rightValue,
		LeftValue:    leftValue,
		SessionID:    sessionID,
		GripPosition: gripPos,
		UpdatedAt:    updatedAt,
		DeletedAt:    deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert assessment", "id", id, "error", err)
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

	slog.Debug("upserting training", "id", id)
	_, err = h.queries.UpsertTraining(ctx, db.UpsertTrainingParams{
		ID:           trainingUUID,
		UserID:       userUUID,
		Name:         name,
		RepeaterID:   repeaterID,
		IsFavorite:   isFavorite,
		IsAssessment: isAssessment,
		UpdatedAt:    updatedAt,
		DeletedAt:    deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert training", "id", id, "error", err)
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

	slog.Debug("upserting repeater", "id", id)
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
		UpdatedAt:         updatedAt,
		DeletedAt:         deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert repeater", "id", id, "error", err)
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

	slog.Debug("upserting rep template", "id", id)
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
		UpdatedAt:    updatedAt,
		DeletedAt:    deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert rep template", "id", id, "error", err)
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

	slog.Debug("upserting rep data", "id", id)
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
		UpdatedAt:     updatedAt,
		DeletedAt:     deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert rep data", "id", id, "error", err)
		return id, err
	}

	return id, nil
}

func (h *SyncHandler) upsertPinnedBuiltinTraining(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid pinned builtin training record")
	}

	builtinTrainingID, _ := m["builtin_training_id"].(string)
	if builtinTrainingID == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing builtin training ID")
	}

	builtinTrainingUUID, err := parseUUID(builtinTrainingID)
	if err != nil {
		return builtinTrainingID, err
	}

	updatedAtStr, _ := m["updated_at"].(string)
	updatedAt, err := parseTimestamp(updatedAtStr)
	if err != nil {
		return builtinTrainingID, err
	}

	var deletedAt pgtype.Timestamptz
	if deletedAtStr, ok := m["deleted_at"].(string); ok && deletedAtStr != "" {
		deletedAt, err = parseTimestamp(deletedAtStr)
		if err != nil {
			return builtinTrainingID, err
		}
	}

	slog.Debug("upserting pinned builtin training", "id", builtinTrainingID)
	_, err = h.queries.UpsertPinnedBuiltinTraining(ctx, db.UpsertPinnedBuiltinTrainingParams{
		BuiltinTrainingID: builtinTrainingUUID,
		UserID:            userUUID,
		UpdatedAt:         updatedAt,
		DeletedAt:         deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert pinned builtin training", "id", builtinTrainingID, "error", err)
		return builtinTrainingID, err
	}

	return builtinTrainingID, nil
}

func (h *SyncHandler) upsertSensorConfig(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid sensor config record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing sensor config ID")
	}

	sensorConfigUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	name, _ := m["name"].(string)
	index := int64(m["index"].(float64))
	tare := float32(m["tare"].(float64))
	coef := float32(m["coef"].(float64))

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

	slog.Debug("upserting sensor config", "id", id)
	_, err = h.queries.UpsertSensorConfig(ctx, db.UpsertSensorConfigParams{
		ID:        sensorConfigUUID,
		UserID:    userUUID,
		Name:      name,
		Index:     index,
		Tare:      tare,
		Coef:      coef,
		UpdatedAt: updatedAt,
		DeletedAt: deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert sensor config", "id", id, "error", err)
		return id, err
	}

	return id, nil
}

func (h *SyncHandler) upsertBuiltinTrainingWeight(ctx context.Context, userUUID pgtype.UUID, data interface{}) (string, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fiber.NewError(fiber.StatusBadRequest, "Invalid builtin training weight record")
	}

	id, _ := m["id"].(string)
	if id == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "Missing builtin training weight ID")
	}

	builtinTrainingWeightUUID, err := parseUUID(id)
	if err != nil {
		return id, err
	}

	builtinTrainingIDStr, _ := m["builtin_training_id"].(string)
	builtinTrainingID, err := parseUUID(builtinTrainingIDStr)
	if err != nil {
		return id, err
	}

	customWeightRight := float32(m["custom_weight_right"].(float64))
	customWeightLeft := float32(m["custom_weight_left"].(float64))

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

	slog.Debug("upserting builtin training weight", "id", id)
	_, err = h.queries.UpsertBuiltinTrainingWeight(ctx, db.UpsertBuiltinTrainingWeightParams{
		ID:                builtinTrainingWeightUUID,
		UserID:            userUUID,
		BuiltinTraningID:  builtinTrainingID,
		CustomWeightRight: customWeightRight,
		CustomWeightLeft:  customWeightLeft,
		UpdatedAt:         updatedAt,
		DeletedAt:         deletedAt,
	})
	if err != nil {
		slog.Error("failed to upsert builtin training weight", "id", id, "error", err)
		return id, err
	}

	return id, nil
}
