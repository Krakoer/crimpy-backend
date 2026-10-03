-- Add the training day nullable first, so the backfill below decides every
-- existing row rather than the default stamping them all with today.
ALTER TABLE "sessions" ADD COLUMN "training_day" date;
-- Rows older than the column carry no device zone, so they take the instant
-- minus four hours read in UTC. Explicit about UTC so the server TimeZone
-- setting cannot move the answer.
UPDATE "sessions" SET "training_day" = (("date" AT TIME ZONE 'UTC'::text) - '04:00:00'::interval)::date;
-- Modify "sessions" table
ALTER TABLE "sessions" ALTER COLUMN "training_day" SET DEFAULT (((now() AT TIME ZONE 'UTC'::text) - '04:00:00'::interval))::date, ALTER COLUMN "training_day" SET NOT NULL;
