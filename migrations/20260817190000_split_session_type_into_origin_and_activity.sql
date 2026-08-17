-- Split the overloaded "session_type" into an activity label and an origin, and
-- link a played session back to what it was played from.

-- Rename rather than drop and re-add: the integer values carry over unchanged
-- (0 hangboard, 1 climbing, 2 stretching, 3 workout), only the meaning narrows
-- to a pure label.
ALTER TABLE "sessions" RENAME COLUMN "session_type" TO "activity";

ALTER TABLE "sessions" ADD COLUMN "origin" text NOT NULL DEFAULT 'logged';
ALTER TABLE "sessions" ADD COLUMN "training_id" uuid NULL;
ALTER TABLE "sessions" ADD COLUMN "program_session_id" uuid NULL;

-- Reps only ever come from a run played step by step in the app, so their
-- presence is what tells the two origins apart in the existing data.
UPDATE "sessions"
SET "origin" = 'played'
WHERE EXISTS (
  SELECT 1 FROM "rep_datas" WHERE "rep_datas"."session_id" = "sessions"."id"
);

-- Assessments are played too, and a protocol that recorded no usable rep still
-- produced its result in the app.
UPDATE "sessions" SET "origin" = 'played' WHERE "is_assessment";

ALTER TABLE "sessions" ADD CONSTRAINT "sessions_origin_check"
  CHECK ("origin" IN ('played', 'logged'));

ALTER TABLE "sessions" ADD CONSTRAINT "sessions_training_id_fkey"
  FOREIGN KEY ("training_id") REFERENCES "trainings" ("id") ON DELETE SET NULL;

ALTER TABLE "sessions" ADD CONSTRAINT "sessions_program_session_id_fkey"
  FOREIGN KEY ("program_session_id") REFERENCES "coach_program_week_sessions" ("id") ON DELETE SET NULL;

CREATE INDEX "sessions_training_id_idx" ON "sessions" ("training_id");
CREATE INDEX "sessions_program_session_id_idx" ON "sessions" ("program_session_id");
