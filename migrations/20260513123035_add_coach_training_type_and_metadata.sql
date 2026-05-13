-- Modify "coach_trainings" table
ALTER TABLE "coach_trainings" ADD COLUMN "training_type" text NOT NULL DEFAULT 'climbing', ADD COLUMN "goal" text NOT NULL DEFAULT '', ADD COLUMN "comment" text NOT NULL DEFAULT '';
