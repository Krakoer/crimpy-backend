-- Create "coach_sessions" table
CREATE TABLE "coach_sessions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "coach_id" uuid NOT NULL,
  "title" text NOT NULL,
  "description" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_sessions_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "coach_sessions_coach_id_idx" to table: "coach_sessions"
CREATE INDEX "coach_sessions_coach_id_idx" ON "coach_sessions" ("coach_id");
-- Create "coach_session_items" table
CREATE TABLE "coach_session_items" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "session_id" uuid NOT NULL,
  "parent_id" uuid NULL,
  "type" text NOT NULL,
  "position" integer NOT NULL DEFAULT 0,
  "cycles" integer NULL,
  "cycle_rest_seconds" integer NULL,
  "reps" integer NULL,
  "reps_unit" text NULL,
  "rest_seconds" integer NULL,
  "exercise_id" uuid NULL,
  "hb_worktime_seconds" integer NULL,
  "both_hands" boolean NULL,
  "loads" jsonb NULL,
  "hand_positions" jsonb NULL,
  "edge_sizes_mm" jsonb NULL,
  "section_title" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_session_items_exercise_id_fkey" FOREIGN KEY ("exercise_id") REFERENCES "exercises" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "coach_session_items_parent_id_fkey" FOREIGN KEY ("parent_id") REFERENCES "coach_session_items" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "coach_session_items_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "coach_sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "coach_session_items_parent_id_idx" to table: "coach_session_items"
CREATE INDEX "coach_session_items_parent_id_idx" ON "coach_session_items" ("parent_id");
-- Create index "coach_session_items_session_id_idx" to table: "coach_session_items"
CREATE INDEX "coach_session_items_session_id_idx" ON "coach_session_items" ("session_id");
