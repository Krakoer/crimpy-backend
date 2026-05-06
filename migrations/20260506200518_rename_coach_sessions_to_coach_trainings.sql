-- Modify "coach_session_items" table
ALTER TABLE "coach_session_items" DROP CONSTRAINT "coach_session_items_exercise_id_fkey", DROP CONSTRAINT "coach_session_items_session_id_fkey";
-- Modify "coach_sessions" table
ALTER TABLE "coach_sessions" DROP CONSTRAINT "coach_sessions_coach_id_fkey";
-- Create "coach_training_items" table
CREATE TABLE "coach_training_items" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "training_id" uuid NOT NULL,
  "parent_id" uuid NULL,
  "type" text NOT NULL,
  "position" integer NOT NULL DEFAULT 0,
  "cycles" integer NULL,
  "cycle_rest_seconds" integer NULL,
  "reps" integer NULL,
  "duration" integer NULL,
  "rest_seconds" integer NULL,
  "exercise_id" uuid NULL,
  "hb_worktime_seconds" integer NULL,
  "both_hands" boolean NULL,
  "loads" jsonb NULL,
  "left_loads" jsonb NULL,
  "hand_positions" jsonb NULL,
  "edge_sizes_mm" jsonb NULL,
  "section_title" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_training_items_parent_id_fkey" FOREIGN KEY ("parent_id") REFERENCES "coach_training_items" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "coach_training_items_parent_id_idx" to table: "coach_training_items"
CREATE INDEX "coach_training_items_parent_id_idx" ON "coach_training_items" ("parent_id");
-- Create index "coach_training_items_training_id_idx" to table: "coach_training_items"
CREATE INDEX "coach_training_items_training_id_idx" ON "coach_training_items" ("training_id");
-- Create "coach_trainings" table
CREATE TABLE "coach_trainings" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "coach_id" uuid NOT NULL,
  "title" text NOT NULL,
  "description" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "coach_trainings_coach_id_idx" to table: "coach_trainings"
CREATE INDEX "coach_trainings_coach_id_idx" ON "coach_trainings" ("coach_id");
-- Modify "coach_training_items" table
ALTER TABLE "coach_training_items" ADD CONSTRAINT "coach_training_items_exercise_id_fkey" FOREIGN KEY ("exercise_id") REFERENCES "exercises" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "coach_training_items_training_id_fkey" FOREIGN KEY ("training_id") REFERENCES "coach_trainings" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "coach_trainings" table
ALTER TABLE "coach_trainings" ADD CONSTRAINT "coach_trainings_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Drop "coach_session_items" table
DROP TABLE "coach_session_items";
-- Drop "coach_sessions" table
DROP TABLE "coach_sessions";
