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
  "created_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"          TIMESTAMPTZ,
  "device_id"           UUID,
  "sync_version"        BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at"   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the assessments the user has done, with the results.
CREATE TABLE "assessments" (
  "id"                UUID        NOT NULL DEFAULT gen_random_uuid(),
  "type"              INTEGER     NOT NULL,
  "right_value"       REAL,
  "left_value"        REAL,
  "session_id"        UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "grip_position"     INTEGER     DEFAULT 0,
  "created_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "device_id"         UUID,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  PRIMARY KEY ("id")
);

-- Stores the repeaters trainings, including builtins and assessments.
CREATE TABLE "repeaters" (
  "id"                   UUID        NOT NULL DEFAULT gen_random_uuid(),
  "sets"                 INTEGER     NOT NULL,
  "reps"                 INTEGER     NOT NULL,
  "worktime"             INTEGER     NOT NULL,
  "resttime"             INTEGER     NOT NULL,
  "set_rest"             INTEGER     NOT NULL,
  "target_weight_right"  REAL,
  "target_weight_left"   REAL,
  "split_hand"           BOOLEAN     NOT NULL,
  "grip_position"        INTEGER     NOT NULL DEFAULT 0,
  "created_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"           TIMESTAMPTZ,
  "device_id"            UUID,
  "sync_version"         BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  "user_id"              UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
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
  "created_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "device_id"         UUID,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the repetitions for the trainings.
CREATE TABLE "rep_templates" (
  "id"                UUID        NOT NULL DEFAULT gen_random_uuid(),
  "is_rest"           BOOLEAN     NOT NULL,
  "right_hand"        BOOLEAN     NOT NULL,
  "duration"          INTEGER     NOT NULL,
  "training_id"       UUID        NOT NULL REFERENCES "trainings"("id") ON DELETE CASCADE,
  "target_weight"     REAL        NOT NULL,
  "index"             INTEGER     NOT NULL,
  "grip_position"     INTEGER     NOT NULL DEFAULT 0,
  "created_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "device_id"         UUID,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  PRIMARY KEY ("id")
);

-- Stores the data for the repetitions done during a session.
CREATE TABLE "rep_datas" (
  "id"                UUID        NOT NULL DEFAULT gen_random_uuid(),
  "average_weight"    REAL        NOT NULL,
  "session_id"        UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "is_rest"           BOOLEAN     NOT NULL,
  "right_hand"        BOOLEAN     NOT NULL,
  "duration"          INTEGER     NOT NULL,
  "target_weight"     REAL        NOT NULL,
  "index"             INTEGER     NOT NULL,
  "grip_position"     INTEGER     NOT NULL DEFAULT 0,
  "created_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"        TIMESTAMPTZ NOT NULL DEFAULT now(),
  "deleted_at"        TIMESTAMPTZ,
  "device_id"         UUID,
  "sync_version"      BIGINT      NOT NULL DEFAULT 1,
  "server_updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "user_id"           UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  PRIMARY KEY ("id")
);

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

-- Indexes for sync operations
CREATE INDEX "sessions_user_sync_idx" ON "sessions"("user_id", "sync_version");
CREATE INDEX "assessments_user_sync_idx" ON "assessments"("user_id", "sync_version");
CREATE INDEX "trainings_user_sync_idx" ON "trainings"("user_id", "sync_version");
CREATE INDEX "repeaters_user_sync_idx" ON "repeaters"("user_id", "sync_version");
CREATE INDEX "rep_templates_user_sync_idx" ON "rep_templates"("user_id", "sync_version");
CREATE INDEX "rep_datas_user_sync_idx" ON "rep_datas"("user_id", "sync_version");

-- Trigger function to auto-update server_updated_at and sync_version
CREATE OR REPLACE FUNCTION bump_sync_version()
RETURNS TRIGGER AS $$
BEGIN
  NEW.server_updated_at := now();
  NEW.updated_at := now();
  NEW.sync_version := COALESCE(
    (SELECT MAX(sync_version) FROM sessions WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM assessments WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM trainings WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM repeaters WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM rep_templates WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM rep_datas WHERE user_id = NEW.user_id), 0
  ) + 1;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply triggers to all synced tables
CREATE TRIGGER trg_bump_sync_version_sessions
BEFORE INSERT OR UPDATE ON sessions
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_assessments
BEFORE INSERT OR UPDATE ON assessments
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_trainings
BEFORE INSERT OR UPDATE ON trainings
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_repeaters
BEFORE INSERT OR UPDATE ON repeaters
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_rep_templates
BEFORE INSERT OR UPDATE ON rep_templates
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_rep_datas
BEFORE INSERT OR UPDATE ON rep_datas
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();
