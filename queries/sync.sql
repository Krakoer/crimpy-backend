-- name: GetSyncSummary :one
SELECT
  $1::uuid as user_id,
  (SELECT COUNT(*) FROM sessions WHERE user_id = $1::uuid AND deleted_at IS NULL) as sessions_count,
  (SELECT COUNT(*) FROM assessments WHERE user_id = $1::uuid AND deleted_at IS NULL) as assessments_count,
  (SELECT COUNT(*) FROM trainings WHERE user_id = $1::uuid AND deleted_at IS NULL) as trainings_count,
  (SELECT COUNT(*) FROM repeaters WHERE user_id = $1::uuid AND deleted_at IS NULL) as repeaters_count,
  (SELECT COUNT(*) FROM rep_templates WHERE user_id = $1::uuid AND deleted_at IS NULL) as rep_templates_count,
  (SELECT COUNT(*) FROM rep_datas WHERE user_id = $1::uuid AND deleted_at IS NULL) as rep_datas_count,
  (SELECT COUNT(*) FROM pinned_builtin_trainings WHERE user_id = $1::uuid AND deleted_at IS NULL) as pinned_builtin_trainings_count,
  (SELECT COUNT(*) FROM sensor_configs WHERE user_id = $1::uuid AND deleted_at IS NULL) as sensor_configs_count,
  (SELECT COUNT(*) FROM builtin_training_weights WHERE user_id = $1::uuid AND deleted_at IS NULL) as builtin_training_weights_count,
  COALESCE((
    SELECT MAX(sync_version) FROM (
      SELECT MAX(sync_version) as sync_version FROM sessions WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM assessments WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM trainings WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM repeaters WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $1::uuid
      UNION ALL
      SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $1::uuid
    ) as all_versions
  ), 0) as last_sync_version;

-- name: GetSessionsSinceVersion :many
SELECT * FROM sessions
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetAssessmentsSinceVersion :many
SELECT * FROM assessments
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetTrainingsSinceVersion :many
SELECT * FROM trainings
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetRepeatersSinceVersion :many
SELECT * FROM repeaters
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetRepTemplatesSinceVersion :many
SELECT * FROM rep_templates
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetRepDatasSinceVersion :many
SELECT * FROM rep_datas
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetPinnedBuiltinTrainingsSinceVersion :many
SELECT * FROM pinned_builtin_trainings
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetSensorConfigsSinceVersion :many
SELECT * FROM sensor_configs
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetBuiltinTrainingWeightsSinceVersion :many
SELECT * FROM builtin_training_weights
WHERE user_id = $1::uuid
AND sync_version > $2::bigint
ORDER BY sync_version ASC;

-- name: GetSessionByID :one
SELECT * FROM sessions WHERE id = $1::uuid AND user_id = $2::uuid;

-- name: GetAssessmentByID :one
SELECT * FROM assessments WHERE id = $1::uuid AND user_id = $2::uuid;

-- name: GetTrainingByID :one
SELECT * FROM trainings WHERE id = $1::uuid AND user_id = $2::uuid;

-- name: GetRepeaterByID :one
SELECT * FROM repeaters WHERE id = $1::uuid AND user_id = $2::uuid;

-- name: GetRepTemplateByID :one
SELECT * FROM rep_templates WHERE id = $1::uuid AND user_id = $2::uuid;

-- name: GetRepDataByID :one
SELECT * FROM rep_datas WHERE id = $1::uuid AND user_id = $2::uuid;

-- name: UpsertSession :one
INSERT INTO sessions (
  id, user_id, name, notes, date, is_assessment, session_type, duration,
  repeater_sets, repeater_reps, repeater_work_time, repeater_rest_time,
  repeater_set_rest, repeater_split_hand, updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  notes = EXCLUDED.notes,
  date = EXCLUDED.date,
  is_assessment = EXCLUDED.is_assessment,
  session_type = EXCLUDED.session_type,
  duration = EXCLUDED.duration,
  repeater_sets = EXCLUDED.repeater_sets,
  repeater_reps = EXCLUDED.repeater_reps,
  repeater_work_time = EXCLUDED.repeater_work_time,
  repeater_rest_time = EXCLUDED.repeater_rest_time,
  repeater_set_rest = EXCLUDED.repeater_set_rest,
  repeater_split_hand = EXCLUDED.repeater_split_hand,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE sessions.user_id = $2::uuid AND EXCLUDED.updated_at > sessions.server_updated_at
RETURNING *;

-- name: UpsertAssessment :one
INSERT INTO assessments (
  id, user_id, type, right_value, left_value, session_id, grip_position,
  updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (id) DO UPDATE SET
  type = EXCLUDED.type,
  right_value = EXCLUDED.right_value,
  left_value = EXCLUDED.left_value,
  session_id = EXCLUDED.session_id,
  grip_position = EXCLUDED.grip_position,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE assessments.user_id = $2::uuid AND EXCLUDED.updated_at > assessments.server_updated_at
RETURNING *;

-- name: UpsertTraining :one
INSERT INTO trainings (
  id, user_id, name, repeater_id, is_favorite, is_assessment,
  updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  repeater_id = EXCLUDED.repeater_id,
  is_favorite = EXCLUDED.is_favorite,
  is_assessment = EXCLUDED.is_assessment,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE trainings.user_id = $2::uuid AND EXCLUDED.updated_at > trainings.server_updated_at
RETURNING *;

-- name: UpsertRepeater :one
INSERT INTO repeaters (
  id, user_id, sets, reps, worktime, resttime, set_rest,
  target_weight_right, target_weight_left, split_hand, grip_position,
  updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (id) DO UPDATE SET
  sets = EXCLUDED.sets,
  reps = EXCLUDED.reps,
  worktime = EXCLUDED.worktime,
  resttime = EXCLUDED.resttime,
  set_rest = EXCLUDED.set_rest,
  target_weight_right = EXCLUDED.target_weight_right,
  target_weight_left = EXCLUDED.target_weight_left,
  split_hand = EXCLUDED.split_hand,
  grip_position = EXCLUDED.grip_position,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE repeaters.user_id = $2::uuid AND EXCLUDED.updated_at > repeaters.server_updated_at
RETURNING *;

-- name: UpsertRepTemplate :one
INSERT INTO rep_templates (
  id, user_id, training_id, is_rest, right_hand, duration, target_weight, index, grip_position,
  updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
ON CONFLICT (id) DO UPDATE SET
  training_id = EXCLUDED.training_id,
  is_rest = EXCLUDED.is_rest,
  right_hand = EXCLUDED.right_hand,
  duration = EXCLUDED.duration,
  target_weight = EXCLUDED.target_weight,
  index = EXCLUDED.index,
  grip_position = EXCLUDED.grip_position,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE rep_templates.user_id = $2::uuid AND EXCLUDED.updated_at > rep_templates.server_updated_at
RETURNING *;

-- name: UpsertRepData :one
INSERT INTO rep_datas (
  id, user_id, session_id, average_weight, is_rest, right_hand, duration, target_weight, index, grip_position,
  updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
ON CONFLICT (id) DO UPDATE SET
  session_id = EXCLUDED.session_id,
  average_weight = EXCLUDED.average_weight,
  is_rest = EXCLUDED.is_rest,
  right_hand = EXCLUDED.right_hand,
  duration = EXCLUDED.duration,
  target_weight = EXCLUDED.target_weight,
  index = EXCLUDED.index,
  grip_position = EXCLUDED.grip_position,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE rep_datas.user_id = $2::uuid AND EXCLUDED.updated_at > rep_datas.server_updated_at
RETURNING *;

-- name: UpsertPinnedBuiltinTraining :one
INSERT INTO pinned_builtin_trainings (
  builtin_training_id, user_id, updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4
)
ON CONFLICT (builtin_training_id) DO UPDATE SET
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE pinned_builtin_trainings.user_id = $2::uuid AND EXCLUDED.updated_at > pinned_builtin_trainings.server_updated_at
RETURNING *;

-- name: UpsertSensorConfig :one
INSERT INTO sensor_configs (
  id, user_id, name, index, tare, coef, updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (id) DO UPDATE SET
  name = EXCLUDED.name,
  index = EXCLUDED.index,
  tare = EXCLUDED.tare,
  coef = EXCLUDED.coef,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE sensor_configs.user_id = $2::uuid AND EXCLUDED.updated_at > sensor_configs.server_updated_at
RETURNING *;

-- name: UpsertBuiltinTrainingWeight :one
INSERT INTO builtin_training_weights (
  id, user_id, builtin_traning_id, custom_weight_right, custom_weight_left, updated_at, deleted_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (id) DO UPDATE SET
  builtin_traning_id = EXCLUDED.builtin_traning_id,
  custom_weight_right = EXCLUDED.custom_weight_right,
  custom_weight_left = EXCLUDED.custom_weight_left,
  updated_at = EXCLUDED.updated_at,
  deleted_at = EXCLUDED.deleted_at,
  server_updated_at = now(),
  sync_version = (
    SELECT COALESCE(MAX(v), 0) + 1 FROM (
      SELECT MAX(sync_version) as v FROM sessions WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM assessments WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM repeaters WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_templates WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM rep_datas WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM pinned_builtin_trainings WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM sensor_configs WHERE user_id = $2::uuid
      UNION ALL SELECT MAX(sync_version) FROM builtin_training_weights WHERE user_id = $2::uuid
    ) as all_versions
  )
WHERE builtin_training_weights.user_id = $2::uuid AND EXCLUDED.updated_at > builtin_training_weights.server_updated_at
RETURNING *;

-- name: UpdateUserLastSeen :exec
UPDATE users SET last_seen_at = now() WHERE id = $1::uuid;
