-- Modify "assessment_definitions" table
ALTER TABLE "assessment_definitions" ADD CONSTRAINT "assessment_definitions_bodyweight_relative_check" CHECK ((NOT bodyweight_relative) OR (unit = 'kilograms'::text)), ADD COLUMN "bodyweight_relative" boolean NOT NULL DEFAULT false;
