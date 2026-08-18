-- The training type is the coach's label for what a session is about. "crimpy"
-- named the product rather than the training, which no longer works now that
-- any training can carry hangboard work. "other" joins it for the odd session,
-- a run in particular.

ALTER TABLE "trainings" DROP CONSTRAINT "trainings_training_type_check";

UPDATE "trainings" SET "training_type" = 'hangboard' WHERE "training_type" = 'crimpy';

ALTER TABLE "trainings" ADD CONSTRAINT "trainings_training_type_check"
  CHECK ("training_type" = ANY (ARRAY['hangboard'::text, 'climbing'::text, 'stretching'::text, 'workout'::text, 'other'::text]));
