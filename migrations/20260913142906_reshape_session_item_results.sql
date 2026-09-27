-- The rows already stored answer a single field each, under a "value" column
-- that is about to be dropped. Folding them into the new one-row-per-pass shape
-- would be a migration written for three weeks of pre-production rows, and two
-- of them answering the same pass would collide on the new unique constraint
-- while doing it. They are cleared instead, which is also what makes the
-- "reported something" check below apply to an empty table.
DELETE FROM "session_item_results";
-- Modify "session_item_results" table
ALTER TABLE "session_item_results" DROP CONSTRAINT "session_item_results_field_check", DROP CONSTRAINT "session_item_results_value_check", DROP CONSTRAINT "session_item_results_unique", ADD CONSTRAINT "session_item_results_cycles_check" CHECK ((cycles IS NULL) OR (cycles >= 0)), ADD CONSTRAINT "session_item_results_duration_check" CHECK ((duration_seconds IS NULL) OR (duration_seconds >= 0)), ADD CONSTRAINT "session_item_results_load_check" CHECK ((load_kg IS NULL) OR (load_kg >= (0)::double precision)), ADD CONSTRAINT "session_item_results_note_check" CHECK ((note IS NULL) OR ((note <> ''::text) AND (char_length(note) <= 2000))), ADD CONSTRAINT "session_item_results_reported_check" CHECK ((reps IS NOT NULL) OR (cycles IS NOT NULL) OR (load_kg IS NOT NULL) OR (duration_seconds IS NOT NULL) OR (note IS NOT NULL)), ADD CONSTRAINT "session_item_results_reps_check" CHECK ((reps IS NULL) OR (reps >= 0)), DROP COLUMN "field", DROP COLUMN "value", ADD COLUMN "reps" integer NULL, ADD COLUMN "cycles" integer NULL, ADD COLUMN "load_kg" real NULL, ADD COLUMN "duration_seconds" integer NULL, ADD COLUMN "note" text NULL, ADD CONSTRAINT "session_item_results_unique" UNIQUE ("session_id", "training_item_id", "occurrence");
