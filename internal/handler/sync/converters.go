package sync

import (
	"crimpy/backend/internal/db"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

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
		UpdatedAt:         s.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:         deletedAt,
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
		UpdatedAt:       a.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
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
		UpdatedAt:       t.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
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
		UpdatedAt:         r.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:         deletedAt,
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
		UpdatedAt:       rt.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
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
		UpdatedAt:       rd.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
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

func convertPinnedBuiltinTrainingToRecord(pbt db.PinnedBuiltinTraining) PinnedBuiltinTrainingRecord {
	var deletedAt *string
	if pbt.DeletedAt.Valid {
		t := pbt.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return PinnedBuiltinTrainingRecord{
		BuiltinTrainingID: uuidToString(pbt.BuiltinTrainingID),
		UpdatedAt:         pbt.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:         deletedAt,
		SyncVersion:       pbt.SyncVersion,
		ServerUpdatedAt:   pbt.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertSensorConfigToRecord(sc db.SensorConfig) SensorConfigRecord {
	var deletedAt *string
	if sc.DeletedAt.Valid {
		t := sc.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return SensorConfigRecord{
		ID:              uuidToString(sc.ID),
		Name:            sc.Name,
		Index:           sc.Index,
		Tare:            sc.Tare,
		Coef:            sc.Coef,
		UpdatedAt:       sc.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:       deletedAt,
		SyncVersion:     sc.SyncVersion,
		ServerUpdatedAt: sc.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}

func convertBuiltinTrainingWeightToRecord(btw db.BuiltinTrainingWeight) BuiltinTrainingWeightRecord {
	var deletedAt *string
	if btw.DeletedAt.Valid {
		t := btw.DeletedAt.Time.Format(time.RFC3339)
		deletedAt = &t
	}

	return BuiltinTrainingWeightRecord{
		ID:                uuidToString(btw.ID),
		BuiltinTrainingID: uuidToString(btw.BuiltinTraningID),
		CustomWeightRight: btw.CustomWeightRight,
		CustomWeightLeft:  btw.CustomWeightLeft,
		UpdatedAt:         btw.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:         deletedAt,
		SyncVersion:       btw.SyncVersion,
		ServerUpdatedAt:   btw.ServerUpdatedAt.Time.Format(time.RFC3339),
	}
}
