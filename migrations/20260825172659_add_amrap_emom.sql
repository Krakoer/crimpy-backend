-- Modify "training_items" table
ALTER TABLE "training_items" ADD CONSTRAINT "training_items_emom_interval_check" CHECK ((type = 'emom'::text) = (interval_seconds IS NOT NULL)), ADD CONSTRAINT "training_items_interval_positive_check" CHECK ((interval_seconds IS NULL) OR (interval_seconds > 0)), ADD CONSTRAINT "training_items_reps_is_max_check" CHECK ((reps_is_max = false) OR (type = 'exercise'::text)), ADD COLUMN "interval_seconds" integer NULL, ADD COLUMN "reps_is_max" boolean NOT NULL DEFAULT false;
-- Create "session_item_results" table
CREATE TABLE "session_item_results" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "session_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "training_item_id" uuid NOT NULL,
  "occurrence" integer NOT NULL DEFAULT 0,
  "field" text NOT NULL,
  "value" integer NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "session_item_results_unique" UNIQUE ("session_id", "training_item_id", "occurrence", "field"),
  CONSTRAINT "session_item_results_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "session_item_results_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "session_item_results_field_check" CHECK (field = ANY (ARRAY['reps'::text, 'cycles'::text])),
  CONSTRAINT "session_item_results_occurrence_check" CHECK (occurrence >= 0),
  CONSTRAINT "session_item_results_value_check" CHECK (value >= 0)
);
-- Create index "session_item_results_session_id_idx" to table: "session_item_results"
CREATE INDEX "session_item_results_session_id_idx" ON "session_item_results" ("session_id");
-- Create index "session_item_results_user_id_idx" to table: "session_item_results"
CREATE INDEX "session_item_results_user_id_idx" ON "session_item_results" ("user_id");
