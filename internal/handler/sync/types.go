package sync

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
	SyncVersion     int64   `json:"sync_version"`
	ServerUpdatedAt string  `json:"server_updated_at"`
}
