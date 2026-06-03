-- Unify training ownership: rename coach_id->user_id, add is_favorite, make goal/comment nullable.

-- Add user_id as nullable first so we can backfill from coach_id
ALTER TABLE "coach_trainings" ADD COLUMN "user_id" uuid NULL;

-- Backfill user_id from coach_id
UPDATE "coach_trainings" SET "user_id" = "coach_id";

-- Apply NOT NULL constraint and FK after backfill
ALTER TABLE "coach_trainings"
  ALTER COLUMN "user_id" SET NOT NULL,
  ADD CONSTRAINT "coach_trainings_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE;

-- Add is_favorite and relax goal/comment constraints
ALTER TABLE "coach_trainings"
  ADD COLUMN "is_favorite" boolean NOT NULL DEFAULT false,
  ALTER COLUMN "goal" DROP NOT NULL,
  ALTER COLUMN "goal" DROP DEFAULT,
  ALTER COLUMN "comment" DROP NOT NULL,
  ALTER COLUMN "comment" DROP DEFAULT;

-- Drop old coach_id column and its index (if any)
ALTER TABLE "coach_trainings" DROP COLUMN "coach_id";

-- Create index on new user_id column
CREATE INDEX "coach_trainings_user_id_idx" ON "coach_trainings" ("user_id");
