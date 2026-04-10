-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "is_coach" boolean NOT NULL DEFAULT false, ADD COLUMN "coach_validated" boolean NOT NULL DEFAULT false;
