-- Modify "rep_datas" table
ALTER TABLE "rep_datas" ADD CONSTRAINT "rep_datas_item_check" CHECK ((training_item_id IS NULL) OR ((training_item_id <> ''::text) AND (char_length(training_item_id) <= 200))), ALTER COLUMN "training_item_id" TYPE text;
-- Modify "session_item_results" table
ALTER TABLE "session_item_results" ADD CONSTRAINT "session_item_results_item_check" CHECK ((training_item_id <> ''::text) AND (char_length(training_item_id) <= 200)), ALTER COLUMN "training_item_id" TYPE text;
