-- Create "assessment_definitions" table
CREATE TABLE "assessment_definitions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NULL,
  "training_id" uuid NULL,
  "label" text NOT NULL,
  "prompt" text NULL,
  "unit" text NOT NULL,
  "per_hand" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "assessment_definitions_training_id_fkey" FOREIGN KEY ("training_id") REFERENCES "trainings" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "assessment_definitions_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assessment_definitions_builtin_check" CHECK (num_nonnulls(user_id, training_id, prompt) = ANY (ARRAY[0, 3])),
  CONSTRAINT "assessment_definitions_unit_check" CHECK (unit = ANY (ARRAY['kilograms'::text, 'seconds'::text, 'repetitions'::text]))
);
-- Create index "assessment_definitions_training_id_idx" to table: "assessment_definitions"
CREATE UNIQUE INDEX "assessment_definitions_training_id_idx" ON "assessment_definitions" ("training_id") WHERE (training_id IS NOT NULL);
-- Create index "assessment_definitions_user_id_idx" to table: "assessment_definitions"
CREATE INDEX "assessment_definitions_user_id_idx" ON "assessment_definitions" ("user_id");

-- Seed the assessments Crimpy ships. The ids are the ones the app already
-- hardcodes in lib/database/builtins.dart, so a client that knows a protocol by
-- id keeps working without a lookup table.
INSERT INTO "assessment_definitions" ("id", "label", "unit", "per_hand") VALUES
  ('55970ac0-4544-4945-80cd-4841f7c58fe5', 'Critical Force', 'kilograms', true),
  ('f7954158-63ba-4f0b-a125-6ef195fa6442', 'Max Force',      'kilograms', true),
  ('493acbdd-6fe7-4f25-987c-575ccf433293', '60% Endurance',  'seconds',   true);

-- Modify "assessments" table: replace the type discriminator by the definition
-- it names. Added nullable, mapped, then tightened, so existing results keep the
-- assessment they were measured against.
ALTER TABLE "assessments" ADD COLUMN "assessment_id" uuid NULL;
UPDATE "assessments" SET "assessment_id" = '55970ac0-4544-4945-80cd-4841f7c58fe5' WHERE "type" = 0;
UPDATE "assessments" SET "assessment_id" = 'f7954158-63ba-4f0b-a125-6ef195fa6442' WHERE "type" = 1;
UPDATE "assessments" SET "assessment_id" = '493acbdd-6fe7-4f25-987c-575ccf433293' WHERE "type" = 2;
DELETE FROM "assessments" WHERE "assessment_id" IS NULL;
ALTER TABLE "assessments"
  ALTER COLUMN "assessment_id" SET NOT NULL,
  DROP COLUMN "type",
  ADD CONSTRAINT "assessments_assessment_id_fkey" FOREIGN KEY ("assessment_id") REFERENCES "assessment_definitions" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Create index "assessments_assessment_id_idx" to table: "assessments"
CREATE INDEX "assessments_assessment_id_idx" ON "assessments" ("assessment_id");
