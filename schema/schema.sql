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
--
-- Two independent columns describe a session. "activity" is what was done and is
-- a label only: it drives colors, filters and the week histogram, never what the
-- app permits. "origin" is how the session came to exist and is set by the code
-- path that wrote it, never chosen by a coach or an athlete. Any activity can
-- have either origin, so a hangboard session played in the app and a run logged
-- by hand are both first class.
CREATE TABLE "sessions" (
  "id"                  UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"                TEXT        NOT NULL,
  "notes"               TEXT        NOT NULL,
  "date"                TIMESTAMPTZ NOT NULL DEFAULT now(),
  "is_assessment"       BOOLEAN     NOT NULL DEFAULT false,
  -- 0 hangboard, 1 climbing, 2 stretching, 3 workout, 4 other.
  "activity"            INTEGER     NOT NULL DEFAULT 0,
  -- 'played': run step by step in the app, so it owns its reps and timings and
  -- only its notes may be edited. 'logged': entered by hand afterwards, so every
  -- field stays editable.
  "origin"              TEXT        NOT NULL DEFAULT 'logged',
  -- What the session was played from, so it can be shown against what was
  -- prescribed. Both null for a session that answers no prescription, and for
  -- templates deleted since. A logged session may carry program_session_id too:
  -- a coach slot with nothing to step through is completed by hand, and that is
  -- still an answer to the prescription. Only a played one freezes the week,
  -- see is_locked in queries/coach_program_weeks.sql.
  "training_id"         UUID,
  "program_session_id"  UUID,
  -- The prescription resolved at create time: the training as it read then,
  -- with the program session overrides already merged into its items. The
  -- template it was resolved from stays editable, so only this snapshot still
  -- describes what the athlete was actually asked to do. Null exactly when the
  -- session was not run from a training, see the check below.
  "prescription"        JSONB,
  "duration"            INTEGER     NOT NULL DEFAULT 0,
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "sessions_activity_check" CHECK (activity BETWEEN 0 AND 4),
  CONSTRAINT "sessions_origin_check" CHECK (origin IN ('played', 'logged')),
  -- A session run from a training carries the snapshot of it. Holding the link
  -- without the snapshot would say the session was prescribed something while
  -- keeping no record of what, which no write path produces. Not the converse:
  -- deleting the training nulls the link and the snapshot rightly outlives it.
  CONSTRAINT "sessions_training_has_prescription_check"
    CHECK (training_id IS NULL OR prescription IS NOT NULL)
);

-- Stores the assessments the user has done, with the results.
CREATE TABLE "assessments" (
  "id"            UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"       UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  -- Which assessment was measured. The definition is declared further down this
  -- file, so the foreign key is added at the bottom.
  "assessment_id" UUID        NOT NULL,
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
  -- Which hand pulled the rep: 'left', 'right', or 'both' for a hang the athlete
  -- took two handed. A boolean cannot carry the third state, so a two handed rep
  -- used to be stored, and read back, as the left hand.
  "hand"           TEXT        NOT NULL,
  "duration"       INTEGER     NOT NULL,
  "target_weight"  REAL        NOT NULL,
  "index"          INTEGER     NOT NULL,
  "grip_position"  INTEGER     NOT NULL DEFAULT 0,
  -- Depth in millimeters of the edge the rep was pulled on, null when the step
  -- prescribes no edge (rests, exercises done off the hangboard).
  "edge_size_mm"   INTEGER,
  -- Which item of the prescription the rep was played from, so the reps of a
  -- training can be read block by block instead of as one pooled list. Null for
  -- a rep recorded outside a training, and for sessions played before the app
  -- started sending it.
  --
  -- Deliberately not a foreign key: it points into the session's frozen
  -- prescription snapshot, which keeps the item ids it was resolved with, not
  -- into the still editable "training_items" row. A reference would have to null
  -- itself when the coach deletes the item, losing the grouping the snapshot can
  -- still describe.
  "training_item_id" UUID,
  -- Whether the step prescribed a load nothing measured, which is a sensor that
  -- dropped while a hang it was meant to read was running. Such a rep stores no
  -- target, exactly as a step nothing was ever going to measure does (a both
  -- hands hang, a run started without a sensor), so this flag is what tells the
  -- two apart: a rep flagged here was performed but could not be graded, so an
  -- on-target ratio leaves it out of both sides, while a step that never had a
  -- target still counts as part of the run it was played in.
  "target_unmeasured" BOOLEAN NOT NULL DEFAULT FALSE,
  "updated_at"     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "rep_datas_hand_check" CHECK (hand IN ('left', 'right', 'both'))
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
  "builtin_training_id" UUID        NOT NULL,
  "custom_weight_left"  REAL        NOT NULL,
  "custom_weight_right" REAL        NOT NULL,
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "builtin_training_weights_user_training_unique" UNIQUE ("user_id", "builtin_training_id")
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
  PRIMARY KEY ("id"),
  CONSTRAINT "trainings_training_type_check"
    CHECK (training_type IN ('hangboard', 'climbing', 'stretching', 'workout', 'other'))
);

CREATE INDEX "trainings_user_id_idx" ON "trainings"("user_id");

-- Stores every assessment that can be measured, the ones Crimpy ships and the
-- ones a coach writes. There is no builtin/custom split anywhere above this
-- table: a result and a percentage reference both name a row here by id.
--
-- A builtin has no owner, no training and asks no question: the app runs it from
-- a sensor protocol keyed by the row id. A custom one carries all three, and is
-- run like any other training, ending on the question its prompt asks.
CREATE TABLE "assessment_definitions" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"     UUID        REFERENCES "users"("id") ON DELETE CASCADE,
  "training_id" UUID        REFERENCES "trainings"("id") ON DELETE RESTRICT,
  "label"       TEXT        NOT NULL,
  "prompt"      TEXT,
  -- What the result is measured in, which decides the item fields a percentage
  -- of it may drive: kilograms a load, seconds a duration, repetitions reps.
  "unit"        TEXT        NOT NULL,
  -- Whether the two hands are measured apart, as a one arm test is.
  "per_hand"    BOOLEAN     NOT NULL DEFAULT FALSE,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "assessment_definitions_unit_check"
    CHECK (unit IN ('kilograms', 'seconds', 'repetitions')),
  CONSTRAINT "assessment_definitions_builtin_check"
    CHECK (num_nonnulls(user_id, training_id, prompt) IN (0, 3))
);

CREATE INDEX "assessment_definitions_user_id_idx" ON "assessment_definitions"("user_id");
CREATE UNIQUE INDEX "assessment_definitions_training_id_idx"
  ON "assessment_definitions"("training_id") WHERE training_id IS NOT NULL;

-- Stores individual items within a training (repeaters, hangboard reps, free notes,
-- exercises, circuits, groups). Items are stored flat; nesting is via parent_id.
--
-- Hangboard configuration is stored as JSONB arrays whose layout is declared by
-- the granularity column rather than inferred from any array length. Every
-- client writes the same shape: one entry per configuration row, with the row
-- count fixed by the granularity (1 uniform, reps per rep, cycles * reps per
-- set indexed set * reps + rep). Per-hand fields carry one array per hand so a
-- row index always means the same thing in every array.
--
-- Types: 'repeater', 'hangboard_rep', 'free', 'exercise', 'circuit', 'group', 'emom'
CREATE TABLE "training_items" (
  "id"                   UUID        NOT NULL DEFAULT gen_random_uuid(),
  "training_id"          UUID        NOT NULL REFERENCES "trainings"("id") ON DELETE CASCADE,
  "parent_id"            UUID        REFERENCES "training_items"("id") ON DELETE CASCADE,
  "type"                 TEXT        NOT NULL,
  "position"             INTEGER     NOT NULL DEFAULT 0,
  -- Circuit, repeater and emom cycles (scalar). A hangboard_rep carries
  -- neither, for the same reason it carries no reps.
  "cycles"               INTEGER,
  "cycle_rest_seconds"   INTEGER,
  -- How often a round starts, for an 'emom' and nothing else. It is what makes
  -- the block every minute on the minute rather than a circuit: the work of a
  -- round is self paced and whatever is left of the interval is the rest, so
  -- the round after it starts on the clock however fast the round before it
  -- was. A circuit names the rest instead and lets the block drift.
  "interval_seconds"     INTEGER,
  -- Exercise and repeater reps (scalar). A hangboard_rep is a single hang and
  -- carries none: the repeater is the block that repeats a hang.
  "reps"                 INTEGER,
  -- Whether the rep count is left open, which is an AMRAP: the coach prescribes
  -- no number and the athlete does as many as they can, then records how many
  -- that was. The stored "reps" is then read by nothing.
  --
  -- Exercises only. A repeater lays its loads, grips and edges out one row per
  -- rep, so a rep count nothing knows until the block has been run leaves those
  -- arrays with no length to be written against.
  "reps_is_max"          BOOLEAN     NOT NULL DEFAULT FALSE,
  "duration"             INTEGER,
  "rest_seconds"         INTEGER,
  -- Exercise-specific (scalar)
  "exercise_id"          UUID        REFERENCES "exercises"("id") ON DELETE CASCADE,
  -- Repeater and hangboard_rep worktime (replaces hb_worktime_seconds)
  "worktime_seconds"     INTEGER,
  -- How the two hands are worked, for repeater and hangboard_rep items:
  --   'both'      both hands on the board at once, one hang per rep
  --   'alternate' one hand at a time, right then left within each rep
  --   'split'     one hand at a time, every rep of a set on one hand before the other
  --   'left'      left hand only
  --   'right'     right hand only
  -- Only 'both' puts two hands on the board simultaneously; the other modes hang
  -- a single hand at a time and can therefore be measured by the force sensor.
  "hand"                 TEXT,
  -- Layout of the configuration arrays below: 'uniform' (1 row), 'rep' (reps
  -- rows) or 'set' (cycles * reps rows, indexed set * reps + rep).
  "granularity"          TEXT,
  -- Free item text content
  "free_text"            TEXT,
  -- Optional coach comment shown to the athlete (e.g. "first rep in pronation")
  "comment"              TEXT,
  -- Configurable fields (JSONB arrays, see the layout note above the table).
  -- Each holds exactly one entry per configuration row.
  -- loads: [{value: float, unit: string}]. Right hand of a two-handed mode,
  -- and the only load for 'both', 'left' and 'right'.
  "loads"                JSONB,
  -- left_loads: [{value: float, unit: string}]. Left hand of 'alternate' and
  -- 'split' items; null otherwise.
  "left_loads"           JSONB,
  -- hand_positions: [[string]] indexed [hand][row], left hand first. Carries
  -- one array per hand, or a single array when both hands share the grip.
  "hand_positions"       JSONB,
  -- edge_sizes_mm: [int]
  "edge_sizes_mm"        JSONB,
  -- Whether load is maximum effort (as hard as possible) rather than a fixed value
  "load_is_max"          BOOLEAN     NOT NULL DEFAULT FALSE,
  -- Scalar fields expressed as a percentage of the athlete last assessment
  -- instead of a fixed number, keyed by field name ('duration', 'reps'):
  -- {"duration": {"assessment_id": "<uuid>", "percent": 75, "fallback": 60}}
  -- The id names a row in "assessment_definitions", whose unit must match the
  -- field: seconds a duration, repetitions reps, kilograms a load. Loads carry
  -- the same reference inline, as a 'percent_assessment' unit.
  "variable_targets"     JSONB,
  -- Group-specific
  "group_title"          TEXT,
  "created_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "training_items_hand_check"
    CHECK (hand IS NULL OR hand IN ('both', 'alternate', 'split', 'left', 'right')),
  CONSTRAINT "training_items_granularity_check"
    CHECK (granularity IS NULL OR granularity IN ('uniform', 'rep', 'set')),
  -- An interval is what an emom is, so the two stand or fall together: a block
  -- typed emom without one has no clock to start its rounds on, and any other
  -- type carrying one names a field nothing reading it would run.
  CONSTRAINT "training_items_emom_interval_check"
    CHECK ((type = 'emom') = (interval_seconds IS NOT NULL)),
  CONSTRAINT "training_items_interval_positive_check"
    CHECK (interval_seconds IS NULL OR interval_seconds > 0),
  CONSTRAINT "training_items_reps_is_max_check"
    CHECK (reps_is_max = FALSE OR type = 'exercise')
);

CREATE INDEX "training_items_training_id_idx" ON "training_items"("training_id");
CREATE INDEX "training_items_parent_id_idx" ON "training_items"("parent_id");

-- Stores what the athlete achieved on a step the prescription left open. An
-- AMRAP exercise has no rep count until it has been run, and an emom the
-- athlete dropped out of ran fewer rounds than it asked for. Neither number can
-- be read back off "rep_datas": a set of pull ups passes through no sensor, so
-- without this table nothing records that twenty three of them were done.
--
-- One row per open field of one pass through an item. "occurrence" tells the
-- passes apart when the item sits inside a block that repeats, counting from 0
-- in the order the run played them.
CREATE TABLE "session_item_results" (
  "id"               UUID        NOT NULL DEFAULT gen_random_uuid(),
  "session_id"       UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "user_id"          UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  -- Which item of the prescription the count answers. Deliberately not a
  -- foreign key, for the reason "rep_datas"."training_item_id" is not one
  -- either: it points into the session frozen prescription snapshot, which
  -- keeps the item ids it was resolved with, not into the still editable
  -- "training_items" row.
  "training_item_id" UUID        NOT NULL,
  "occurrence"       INTEGER     NOT NULL DEFAULT 0,
  -- Which open field the count answers: 'reps' for an AMRAP, 'cycles' for the
  -- rounds an emom was carried through before the athlete dropped out.
  "field"            TEXT        NOT NULL,
  "value"            INTEGER     NOT NULL,
  "updated_at"       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "session_item_results_field_check" CHECK (field IN ('reps', 'cycles')),
  CONSTRAINT "session_item_results_value_check" CHECK (value >= 0),
  CONSTRAINT "session_item_results_occurrence_check" CHECK (occurrence >= 0),
  CONSTRAINT "session_item_results_unique" UNIQUE ("session_id", "training_item_id", "occurrence", "field")
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

-- Declared here rather than inline on "sessions" because both targets are
-- created further down this file.
ALTER TABLE "sessions"
  ADD CONSTRAINT "sessions_training_id_fkey"
  FOREIGN KEY ("training_id") REFERENCES "trainings"("id") ON DELETE SET NULL;

ALTER TABLE "sessions"
  ADD CONSTRAINT "sessions_program_session_id_fkey"
  FOREIGN KEY ("program_session_id") REFERENCES "coach_program_week_sessions"("id") ON DELETE SET NULL;

-- RESTRICT: deleting a definition must never silently erase the history measured
-- against it. The handler refuses the delete with a count instead.
ALTER TABLE "assessments"
  ADD CONSTRAINT "assessments_assessment_id_fkey"
  FOREIGN KEY ("assessment_id") REFERENCES "assessment_definitions"("id") ON DELETE RESTRICT;

-- Indexes for foreign keys to improve query performance
CREATE INDEX "sessions_user_id_idx" ON "sessions"("user_id");
CREATE INDEX "sessions_training_id_idx" ON "sessions"("training_id");
CREATE INDEX "sessions_program_session_id_idx" ON "sessions"("program_session_id");
CREATE INDEX "assessments_session_id_idx" ON "assessments"("session_id");
CREATE INDEX "assessments_user_id_idx" ON "assessments"("user_id");
CREATE INDEX "assessments_assessment_id_idx" ON "assessments"("assessment_id");
CREATE INDEX "rep_datas_session_id_idx" ON "rep_datas"("session_id");
CREATE INDEX "session_item_results_session_id_idx" ON "session_item_results"("session_id");
CREATE INDEX "session_item_results_user_id_idx" ON "session_item_results"("user_id");
CREATE INDEX "rep_datas_user_id_idx" ON "rep_datas"("user_id");
CREATE INDEX "pinned_builtin_trainings_user_id_idx" ON "pinned_builtin_trainings"("user_id");
CREATE INDEX "sensor_configs_user_id_idx" ON "sensor_configs"("user_id");
CREATE INDEX "builtin_training_weights_user_id_idx" ON "builtin_training_weights"("user_id");

