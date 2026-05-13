-- Modify "coach_trainings" table
ALTER TABLE "coach_trainings" ALTER COLUMN "training_type" SET DEFAULT 'workout';
-- Rename existing 'climbing' rows to 'workout' (the old default meant workout, not on-the-wall climbing)
UPDATE "coach_trainings" SET "training_type" = 'workout' WHERE "training_type" = 'climbing';
