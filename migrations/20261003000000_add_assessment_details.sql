-- Modify "assessments" table
ALTER TABLE "assessments" ADD CONSTRAINT "assessments_details_object_check" CHECK ((details IS NULL) OR (jsonb_typeof(details) = 'object'::text)), ADD COLUMN "details" jsonb NULL;
