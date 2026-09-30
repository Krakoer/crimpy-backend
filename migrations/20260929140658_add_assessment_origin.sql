-- Modify "assessments" table
ALTER TABLE "assessments" ADD CONSTRAINT "assessments_origin_check" CHECK (origin = ANY (ARRAY['test'::text, 'training'::text])), ADD COLUMN "origin" text NOT NULL DEFAULT 'test';
