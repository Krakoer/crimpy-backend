CREATE TABLE "users" (
  "id"           UUID        NOT NULL DEFAULT gen_random_uuid(),
  "email"        TEXT        NOT NULL,
  "password"     TEXT        NOT NULL,
  "firstname"    TEXT        NOT NULL,
  "lastname"     TEXT        NOT NULL,
  "is_admin"     BOOLEAN     NOT NULL DEFAULT false,
  "created_at"   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "users_email_key" ON "users" ("email");

-- Stores the training sessions the user has done.
CREATE TABLE "sessions" (
  "id"                  SERIAL      NOT NULL,
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"                TEXT        NOT NULL,
  "notes"               TEXT        NOT NULL,
  "date"                TIMESTAMPTZ NOT NULL DEFAULT now(),
  "is_assessment"       BOOLEAN     NOT NULL DEFAULT false,
  "session_type"        INTEGER     NOT NULL DEFAULT 0, -- 0 = crimpy (default)
  "duration"            INTEGER     NOT NULL DEFAULT 0,
  -- Repeater configuration (if session was a repeater workout)
  "repeater_sets"       INTEGER,
  "repeater_reps"       INTEGER,
  "repeater_work_time"  INTEGER,
  "repeater_rest_time"  INTEGER,
  "repeater_set_rest"   INTEGER,
  "repeater_split_hand" BOOLEAN,
  PRIMARY KEY ("id")
);

-- Stores the assessments the user has done, with the results.
CREATE TABLE "assessments" (
  "id"            SERIAL  NOT NULL,
  "type"          INTEGER NOT NULL,
  "right_value"   REAL,
  "left_value"    REAL,
  "session_id"    INTEGER NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "grip_position" INTEGER DEFAULT 0, -- 0 = halfCrimp (default)
  PRIMARY KEY ("id")
);

-- Stores the repeaters trainings, including builtins and assessments.
CREATE TABLE "repeaters" (
  "id"                   SERIAL  NOT NULL,
  "sets"                 INTEGER NOT NULL,
  "reps"                 INTEGER NOT NULL,
  "worktime"             INTEGER NOT NULL,
  "resttime"             INTEGER NOT NULL,
  "set_rest"             INTEGER NOT NULL,
  "target_weight_right"  REAL,
  "target_weight_left"   REAL,
  "split_hand"           BOOLEAN NOT NULL,
  "grip_position"        INTEGER NOT NULL DEFAULT 0, -- 0 = halfCrimp (default)
  PRIMARY KEY ("id")
);

-- Stores the available trainings, including builtins and assessments.
CREATE TABLE "trainings" (
  "id"            SERIAL  NOT NULL,
  "user_id"       UUID    NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"          TEXT    NOT NULL,
  "repeater_id"   INTEGER REFERENCES "repeaters"("id") ON DELETE CASCADE,
  "is_favorite"   BOOLEAN NOT NULL DEFAULT false,
  "is_assessment" BOOLEAN NOT NULL DEFAULT false,
  PRIMARY KEY ("id")
);

-- Stores the repetitions for the trainings.
CREATE TABLE "rep_templates" (
  "id"            SERIAL  NOT NULL,
  "is_rest"       BOOLEAN NOT NULL,
  "right_hand"    BOOLEAN NOT NULL,
  "duration"      INTEGER NOT NULL,
  "training_id"   INTEGER NOT NULL REFERENCES "trainings"("id") ON DELETE CASCADE,
  "target_weight" REAL    NOT NULL,
  "index"         INTEGER NOT NULL,
  "grip_position" INTEGER NOT NULL DEFAULT 0, -- 0 = halfCrimp (default)
  PRIMARY KEY ("id")
);

-- Stores the data for the repetitions done during a session.
CREATE TABLE "rep_datas" (
  "id"             SERIAL  NOT NULL,
  "average_weight" REAL    NOT NULL,
  "session_id"     INTEGER NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "is_rest"        BOOLEAN NOT NULL,
  "right_hand"     BOOLEAN NOT NULL,
  "duration"       INTEGER NOT NULL,
  "target_weight"  REAL    NOT NULL,
  "index"          INTEGER NOT NULL,
  "grip_position"  INTEGER NOT NULL DEFAULT 0, -- 0 = halfCrimp (default)
  PRIMARY KEY ("id")
);

-- Indexes for foreign keys to improve query performance
CREATE INDEX "sessions_user_id_idx" ON "sessions"("user_id");
CREATE INDEX "assessments_session_id_idx" ON "assessments"("session_id");
CREATE INDEX "trainings_user_id_idx" ON "trainings"("user_id");
CREATE INDEX "trainings_repeater_id_idx" ON "trainings"("repeater_id");
CREATE INDEX "rep_templates_training_id_idx" ON "rep_templates"("training_id");
CREATE INDEX "rep_datas_session_id_idx" ON "rep_datas"("session_id");
