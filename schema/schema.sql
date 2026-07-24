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

-- Emails are stored normalized to lowercase; the index enforces that two
-- addresses differing only by case cannot both be registered.
CREATE UNIQUE INDEX "users_email_key" ON "users" (lower("email"));

-- Long-lived refresh tokens used to mint new short-lived access tokens.
-- Only the SHA-256 hash of the token is stored.
CREATE TABLE "refresh_tokens" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "token_hash" TEXT        NOT NULL,
  "expires_at" TIMESTAMPTZ NOT NULL,
  "revoked"    BOOLEAN     NOT NULL DEFAULT false,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "refresh_tokens_token_hash_key" ON "refresh_tokens" ("token_hash");
CREATE INDEX "refresh_tokens_user_id_idx" ON "refresh_tokens"("user_id");

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
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the assessments the user has done, with the results.
CREATE TABLE "assessments" (
  "id"            UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"       UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "type"          INTEGER     NOT NULL,
  "right_value"   REAL,
  "left_value"    REAL,
  "session_id"    UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "grip_position" INTEGER     DEFAULT 0,
  "updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);


-- Stores the data for the repetitions done during a session.
CREATE TABLE "rep_datas" (
  "id"             UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"        UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "average_weight" REAL        NOT NULL,
  "session_id"     UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "is_rest"        BOOLEAN     NOT NULL,
  "right_hand"     BOOLEAN     NOT NULL,
  "duration"       INTEGER     NOT NULL,
  "target_weight"  REAL        NOT NULL,
  "index"          INTEGER     NOT NULL,
  "grip_position"  INTEGER     NOT NULL DEFAULT 0,
  "updated_at"     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores the IDs of pinned builtin trainings
CREATE TABLE "pinned_builtin_trainings" (
  "builtin_training_id" UUID        NOT NULL,
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("builtin_training_id", "user_id")
);

-- Stores the saved sensor configs.
CREATE TABLE "sensor_configs" (
  "id"         UUID        NOT NULL,
  "user_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"       TEXT        NOT NULL,
  "index"      BIGINT      NOT NULL,
  "tare"       REAL        NOT NULL,
  "coef"       REAL        NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores custom weights for builtin trainings per user.
CREATE TABLE "builtin_training_weights" (
  "id"                  UUID        NOT NULL,
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "builtin_traning_id"  UUID        NOT NULL,
  "custom_weight_left"  REAL        NOT NULL,
  "custom_weight_right" REAL        NOT NULL,
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores exercises created by coaches for use in session templates.
CREATE TABLE "exercises" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"        TEXT        NOT NULL,
  "description" TEXT,
  "comment"     TEXT,
  "video_link"  TEXT,
  "is_favorite" BOOLEAN     NOT NULL DEFAULT false,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "exercises_coach_id_idx" ON "exercises"("coach_id");

-- Stores tags created by coaches to categorize exercises.
-- coach_id is NULL for system (builtin) tags.
CREATE TABLE "tags" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"   UUID        REFERENCES "users"("id") ON DELETE CASCADE,
  "name"       TEXT        NOT NULL,
  "color"      TEXT        NOT NULL,
  "is_builtin" BOOLEAN     NOT NULL DEFAULT FALSE,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "tags_coach_id_idx" ON "tags"("coach_id");

-- Associates tags with exercises (many-to-many).
CREATE TABLE "exercise_tags" (
  "exercise_id" UUID NOT NULL REFERENCES "exercises"("id") ON DELETE CASCADE,
  "tag_id"      UUID NOT NULL REFERENCES "tags"("id") ON DELETE CASCADE,
  PRIMARY KEY ("exercise_id", "tag_id")
);

-- Stores training templates for all users (user-created and coach-created).
CREATE TABLE "trainings" (
  "id"            UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"       UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "title"         TEXT        NOT NULL,
  "description"   TEXT,
  "training_type" TEXT        NOT NULL DEFAULT 'workout',
  "goal"          TEXT,
  "comment"       TEXT,
  "is_favorite"   BOOLEAN     NOT NULL DEFAULT FALSE,
  "created_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "trainings_user_id_idx" ON "trainings"("user_id");

-- Stores individual items within a training (repeaters, hangboard reps, free notes,
-- exercises, circuits, groups). Items are stored flat; nesting is via parent_id.
-- Per-rep configurable fields are stored as JSONB arrays sized by the number of reps.
-- Types: 'repeater', 'hangboard_rep', 'free', 'exercise', 'circuit', 'group'
CREATE TABLE "training_items" (
  "id"                   UUID        NOT NULL DEFAULT gen_random_uuid(),
  "training_id"          UUID        NOT NULL REFERENCES "trainings"("id") ON DELETE CASCADE,
  "parent_id"            UUID        REFERENCES "training_items"("id") ON DELETE CASCADE,
  "type"                 TEXT        NOT NULL,
  "position"             INTEGER     NOT NULL DEFAULT 0,
  -- Circuit and repeater cycles (scalar)
  "cycles"               INTEGER,
  "cycle_rest_seconds"   INTEGER,
  -- Exercise, repeater, and hangboard_rep reps (scalar)
  "reps"                 INTEGER,
  "duration"             INTEGER,
  "rest_seconds"         INTEGER,
  -- Exercise-specific (scalar)
  "exercise_id"          UUID        REFERENCES "exercises"("id") ON DELETE CASCADE,
  -- Repeater and hangboard_rep worktime (replaces hb_worktime_seconds)
  "worktime_seconds"     INTEGER,
  -- Hand: 'both', 'split', 'left', 'right' (replaces both_hands bool)
  "hand"                 TEXT,
  -- Free item text content
  "free_text"            TEXT,
  -- Optional coach comment shown to the athlete (e.g. "first rep in pronation")
  "comment"              TEXT,
  -- Per-rep configurable fields (JSONB arrays sized by reps)
  -- loads: [{value: float, unit: string}] per rep; right-hand or both-hands loads
  "loads"                JSONB,
  -- left_loads: [{value: float, unit: string}] per rep; only set in split mode
  "left_loads"           JSONB,
  -- hand_positions: [string] per rep
  "hand_positions"       JSONB,
  -- edge_sizes_mm: [int] per rep
  "edge_sizes_mm"        JSONB,
  -- Whether load is maximum effort (as hard as possible) rather than a fixed value
  "load_is_max"          BOOLEAN     NOT NULL DEFAULT FALSE,
  -- Group-specific
  "group_title"          TEXT,
  "created_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "training_items_training_id_idx" ON "training_items"("training_id");
CREATE INDEX "training_items_parent_id_idx" ON "training_items"("parent_id");

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

-- Stores training programs created by coaches for specific enrolled users.
CREATE TABLE "coach_programs" (
  "id"              UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"        UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "user_id"         UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"            TEXT        NOT NULL,
  "objective"       TEXT,
  "start_date"      DATE        NOT NULL,
  "duration_weeks"  INTEGER,
  "created_at"      TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"      TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_programs_coach_id_idx" ON "coach_programs"("coach_id");
CREATE INDEX "coach_programs_user_id_idx"  ON "coach_programs"("user_id");

-- Represents a specific week in a training program.
CREATE TABLE "coach_program_weeks" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "program_id"  UUID        NOT NULL REFERENCES "coach_programs"("id") ON DELETE CASCADE,
  "week_number" INTEGER     NOT NULL,
  "notes"       TEXT,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "coach_program_weeks_number_check" CHECK (week_number > 0),
  UNIQUE ("program_id", "week_number"),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_program_weeks_program_id_idx" ON "coach_program_weeks"("program_id");

-- A training session assigned to a specific week.
-- day_of_week 0=Mon...6=Sun; NULL means unscheduled (times_per_week required).
CREATE TABLE "coach_program_week_sessions" (
  "id"             UUID        NOT NULL DEFAULT gen_random_uuid(),
  "week_id"        UUID        NOT NULL REFERENCES "coach_program_weeks"("id") ON DELETE CASCADE,
  "training_id"    UUID        NOT NULL REFERENCES "trainings"("id") ON DELETE RESTRICT,
  "day_of_week"    INTEGER,
  "times_per_week" INTEGER,
  "is_everyday"    BOOLEAN     NOT NULL DEFAULT FALSE,
  "position"       INTEGER     NOT NULL DEFAULT 0,
  "notes"          TEXT,
  "created_at"     TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"     TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "cpws_mode_check" CHECK (
    (day_of_week IS NOT NULL AND times_per_week IS NULL AND is_everyday = FALSE)
    OR (day_of_week IS NULL AND times_per_week IS NOT NULL AND is_everyday = FALSE)
    OR (day_of_week IS NULL AND times_per_week IS NULL AND is_everyday = TRUE)
  ),
  CONSTRAINT "cpws_day_range_check"
    CHECK (day_of_week IS NULL OR (day_of_week >= 0 AND day_of_week <= 6)),
  CONSTRAINT "cpws_times_check"
    CHECK (times_per_week IS NULL OR times_per_week > 0),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_program_week_sessions_week_id_idx" ON "coach_program_week_sessions"("week_id");

-- Sparse per-item overrides for a session (e.g. different loads for a specific week).
-- overrides JSONB: only the fields being changed from the training template.
CREATE TABLE "coach_program_session_overrides" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "session_id" UUID        NOT NULL REFERENCES "coach_program_week_sessions"("id") ON DELETE CASCADE,
  "item_id"    UUID        NOT NULL REFERENCES "training_items"("id") ON DELETE CASCADE,
  "overrides"  JSONB       NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE ("session_id", "item_id"),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_program_session_overrides_session_id_idx" ON "coach_program_session_overrides"("session_id");

-- Indexes for foreign keys to improve query performance
CREATE INDEX "sessions_user_id_idx" ON "sessions"("user_id");
CREATE INDEX "assessments_session_id_idx" ON "assessments"("session_id");
CREATE INDEX "assessments_user_id_idx" ON "assessments"("user_id");
CREATE INDEX "rep_datas_session_id_idx" ON "rep_datas"("session_id");
CREATE INDEX "rep_datas_user_id_idx" ON "rep_datas"("user_id");
CREATE INDEX "pinned_builtin_trainings_user_id_idx" ON "pinned_builtin_trainings"("user_id");
CREATE INDEX "sensor_configs_user_id_idx" ON "sensor_configs"("user_id");
CREATE INDEX "builtin_training_weights_user_id_idx" ON "builtin_training_weights"("user_id");

