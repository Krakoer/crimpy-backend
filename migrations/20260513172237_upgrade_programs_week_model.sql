-- Create "coach_program_weeks" table
CREATE TABLE "coach_program_weeks" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "program_id" uuid NOT NULL,
  "week_number" integer NOT NULL,
  "notes" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_program_weeks_program_id_week_number_key" UNIQUE ("program_id", "week_number"),
  CONSTRAINT "coach_program_weeks_program_id_fkey" FOREIGN KEY ("program_id") REFERENCES "coach_programs" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "coach_program_weeks_number_check" CHECK (week_number > 0)
);
-- Create index "coach_program_weeks_program_id_idx" to table: "coach_program_weeks"
CREATE INDEX "coach_program_weeks_program_id_idx" ON "coach_program_weeks" ("program_id");
-- Create "coach_program_week_sessions" table
CREATE TABLE "coach_program_week_sessions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "week_id" uuid NOT NULL,
  "training_id" uuid NOT NULL,
  "day_of_week" integer NULL,
  "times_per_week" integer NULL,
  "position" integer NOT NULL DEFAULT 0,
  "notes" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_program_week_sessions_training_id_fkey" FOREIGN KEY ("training_id") REFERENCES "coach_trainings" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "coach_program_week_sessions_week_id_fkey" FOREIGN KEY ("week_id") REFERENCES "coach_program_weeks" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "cpws_day_range_check" CHECK ((day_of_week IS NULL) OR ((day_of_week >= 0) AND (day_of_week <= 6))),
  CONSTRAINT "cpws_mode_check" CHECK (((day_of_week IS NOT NULL) AND (times_per_week IS NULL)) OR ((day_of_week IS NULL) AND (times_per_week IS NOT NULL))),
  CONSTRAINT "cpws_times_check" CHECK ((times_per_week IS NULL) OR (times_per_week > 0))
);
-- Create index "coach_program_week_sessions_week_id_idx" to table: "coach_program_week_sessions"
CREATE INDEX "coach_program_week_sessions_week_id_idx" ON "coach_program_week_sessions" ("week_id");
-- Create "coach_program_session_overrides" table
CREATE TABLE "coach_program_session_overrides" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "session_id" uuid NOT NULL,
  "item_id" uuid NOT NULL,
  "overrides" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_program_session_overrides_session_id_item_id_key" UNIQUE ("session_id", "item_id"),
  CONSTRAINT "coach_program_session_overrides_item_id_fkey" FOREIGN KEY ("item_id") REFERENCES "coach_training_items" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "coach_program_session_overrides_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "coach_program_week_sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "coach_program_session_overrides_session_id_idx" to table: "coach_program_session_overrides"
CREATE INDEX "coach_program_session_overrides_session_id_idx" ON "coach_program_session_overrides" ("session_id");
-- Drop "coach_program_slots" table
DROP TABLE "coach_program_slots";
