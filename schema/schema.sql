CREATE TABLE "users" (
  "id"                            UUID        NOT NULL DEFAULT gen_random_uuid(),
  "email"                         TEXT        NOT NULL,
  "password"                      TEXT        NOT NULL,
  "firstname"                     TEXT        NOT NULL,
  "lastname"                      TEXT        NOT NULL,
  "is_admin"                      BOOLEAN     NOT NULL DEFAULT false,
  "is_coach"                      BOOLEAN     NOT NULL DEFAULT false,
  "coach_validated"               BOOLEAN     NOT NULL DEFAULT false,
  "email_verified"                BOOLEAN     NOT NULL DEFAULT false,
  "verification_token"            TEXT,
  "verification_token_expires_at" TIMESTAMPTZ,
  "verification_email_sent_at"    TIMESTAMPTZ,
  "created_at"                    TIMESTAMPTZ NOT NULL DEFAULT now(),
  "last_seen_at"                  TIMESTAMPTZ,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "users_email_key" ON "users" ("email");

-- Stores the training sessions the user has done.
CREATE TABLE "sessions" (
  "id"                  UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"                TEXT        NOT NULL,
  "notes"               TEXT        NOT NULL,
  "date"                TIMESTAMPTZ NOT NULL DEFAULT now(),
  "is_assessment"       BOOLEAN     NOT NULL DEFAULT false,
  "session_type"        INTEGER     NOT NULL DEFAULT 0,
  "duration"            INTEGER     NOT NULL DEFAULT 0,
  "repeater_sets"       INTEGER,
  "repeater_reps"       INTEGER,
  "repeater_work_time"  INTEGER,
  "repeater_rest_time"  INTEGER,
  "repeater_set_rest"   INTEGER,
  "repeater_split_hand" BOOLEAN,
  -- Sync columns
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"          TIMESTAMPTZ,
  "sync_version"        BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at"   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the assessments the user has done, with the results.
CREATE TABLE "assessments" (
  "id"                UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "type"              INTEGER     NOT NULL,
  "right_value"       REAL,
  "left_value"        REAL,
  "session_id"        UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "grip_position"     INTEGER     DEFAULT 0,
  -- Sync columns
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the repeaters trainings, including builtins and assessments.
CREATE TABLE "repeaters" (
  "id"                   UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"              UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "sets"                 INTEGER     NOT NULL,
  "reps"                 INTEGER     NOT NULL,
  "worktime"             INTEGER     NOT NULL,
  "resttime"             INTEGER     NOT NULL,
  "set_rest"             INTEGER     NOT NULL,
  "target_weight_right"  REAL,
  "target_weight_left"   REAL,
  "split_hand"           BOOLEAN     NOT NULL,
  "grip_position"        INTEGER     NOT NULL DEFAULT 0,
  -- Sync columns
  "updated_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"           TIMESTAMPTZ,
  "sync_version"         BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the available trainings, including builtins and assessments.
CREATE TABLE "trainings" (
  "id"                UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"              TEXT        NOT NULL,
  "repeater_id"       UUID        REFERENCES "repeaters"("id") ON DELETE CASCADE,
  "is_favorite"       BOOLEAN     NOT NULL DEFAULT false,
  "is_assessment"     BOOLEAN     NOT NULL DEFAULT false,
  -- Sync columns
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the repetitions for the trainings.
CREATE TABLE "rep_templates" (
  "id"                UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "is_rest"           BOOLEAN     NOT NULL,
  "right_hand"        BOOLEAN     NOT NULL,
  "duration"          INTEGER     NOT NULL,
  "training_id"       UUID        NOT NULL REFERENCES "trainings"("id") ON DELETE CASCADE,
  "target_weight"     REAL        NOT NULL,
  "index"             INTEGER     NOT NULL,
  "grip_position"     INTEGER     NOT NULL DEFAULT 0,
  -- Sync columns
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the data for the repetitions done during a session.
CREATE TABLE "rep_datas" (
  "id"                UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "average_weight"    REAL        NOT NULL,
  "session_id"        UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "is_rest"           BOOLEAN     NOT NULL,
  "right_hand"        BOOLEAN     NOT NULL,
  "duration"          INTEGER     NOT NULL,
  "target_weight"     REAL        NOT NULL,
  "index"             INTEGER     NOT NULL,
  "grip_position"     INTEGER     NOT NULL DEFAULT 0,
  -- Sync columns
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the IDs of pinned builtin trainings
CREATE TABLE "pinned_builtin_trainings" (
  "builtin_training_id"     UUID        NOT NULL,
  "user_id"                 UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  -- Sync columns
  "updated_at"              TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"              TIMESTAMPTZ,
  "sync_version"            BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("builtin_training_id")
);

-- Stores the saved sensor configs.
CREATE TABLE "sensor_configs" (
  "id"                UUID        NOT NULL,
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"              TEXT        NOT NULL,
  "index"             BIGINT      NOT NULL,
  "tare"              REAL        NOT NULL,
  "coef"              REAL        NOT NULL,
  -- Sync columns
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores custom weights for builtin trainings per user.
CREATE TABLE "builtin_training_weights" (
  "id"                    UUID        NOT NULL,
  "user_id"               UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "builtin_traning_id"    UUID        NOT NULL,
  "custom_weight_left"    REAL        NOT NULL,
  "custom_weight_right"   REAL        NOT NULL,
  -- Sync columns
  "updated_at"            TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"            TIMESTAMPTZ,
  "sync_version"          BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores one-time enrollment invitation tokens generated by coaches.
CREATE TABLE "enrollment_tokens" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"   UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "token"      TEXT        NOT NULL,
  "expires_at" TIMESTAMPTZ NOT NULL,
  "used_at"    TIMESTAMPTZ,
  "used_by"    UUID        REFERENCES "users"("id") ON DELETE SET NULL,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "enrollment_tokens_token_key" ON "enrollment_tokens" ("token");
CREATE INDEX "enrollment_tokens_coach_id_idx" ON "enrollment_tokens" ("coach_id");

-- Stores exercises created by coaches for use in programs.
CREATE TABLE "exercises" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"        TEXT        NOT NULL,
  "description" TEXT,
  "comment"     TEXT,
  "video_link"  TEXT,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "exercises_coach_id_idx" ON "exercises"("coach_id");

-- Stores active coach-client relationships. A user may only be enrolled with one coach at a time.
CREATE TABLE "coach_enrollments" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "user_id"     UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "enrolled_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "coach_enrollments_user_id_key" ON "coach_enrollments" ("user_id");
CREATE INDEX "coach_enrollments_coach_id_idx" ON "coach_enrollments" ("coach_id");

-- Indexes for foreign keys to improve query performance
CREATE INDEX "sessions_user_id_idx" ON "sessions"("user_id");
CREATE INDEX "assessments_session_id_idx" ON "assessments"("session_id");
CREATE INDEX "assessments_user_id_idx" ON "assessments"("user_id");
CREATE INDEX "trainings_user_id_idx" ON "trainings"("user_id");
CREATE INDEX "trainings_repeater_id_idx" ON "trainings"("repeater_id");
CREATE INDEX "repeaters_user_id_idx" ON "repeaters"("user_id");
CREATE INDEX "rep_templates_training_id_idx" ON "rep_templates"("training_id");
CREATE INDEX "rep_templates_user_id_idx" ON "rep_templates"("user_id");
CREATE INDEX "rep_datas_session_id_idx" ON "rep_datas"("session_id");
CREATE INDEX "rep_datas_user_id_idx" ON "rep_datas"("user_id");
CREATE INDEX "pinned_builtin_trainings_user_id_idx" ON "pinned_builtin_trainings"("user_id");
CREATE INDEX "sensor_configs_user_id_idx" ON "sensor_configs"("user_id");
CREATE INDEX "builtin_training_weights_user_id_idx" ON "builtin_training_weights"("user_id");

-- Indexes for sync operations
CREATE INDEX "sessions_user_sync_idx" ON "sessions"("user_id", "sync_version");
CREATE INDEX "assessments_user_sync_idx" ON "assessments"("user_id", "sync_version");
CREATE INDEX "trainings_user_sync_idx" ON "trainings"("user_id", "sync_version");
CREATE INDEX "repeaters_user_sync_idx" ON "repeaters"("user_id", "sync_version");
CREATE INDEX "rep_templates_user_sync_idx" ON "rep_templates"("user_id", "sync_version");
CREATE INDEX "rep_datas_user_sync_idx" ON "rep_datas"("user_id", "sync_version");
CREATE INDEX "pinned_builtin_trainings_user_sync_idx" ON "pinned_builtin_trainings"("user_id", "sync_version");
CREATE INDEX "sensor_configs_user_sync_idx" ON "sensor_configs"("user_id", "sync_version");
CREATE INDEX "builtin_training_weights_user_sync_idx" ON "builtin_training_weights"("user_id", "sync_version");
