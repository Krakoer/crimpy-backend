-- Modify "sessions" table
ALTER TABLE "sessions" ADD CONSTRAINT "sessions_samples_assessment_only_check" CHECK ((samples IS NULL) OR is_assessment), ADD COLUMN "samples" jsonb NULL;
