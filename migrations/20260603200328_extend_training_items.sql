-- Extend coach_training_items: replace hb_worktime_seconds/both_hands with
-- worktime_seconds/hand, add free_text and load_is_max, rename type 'hangboard'->'repeater'.

-- Add new columns
ALTER TABLE "coach_training_items"
  ADD COLUMN "worktime_seconds" integer NULL,
  ADD COLUMN "hand" text NULL,
  ADD COLUMN "free_text" text NULL,
  ADD COLUMN "load_is_max" boolean NOT NULL DEFAULT false;

-- Migrate hb_worktime_seconds -> worktime_seconds
UPDATE "coach_training_items" SET "worktime_seconds" = "hb_worktime_seconds";

-- Migrate both_hands (true->'both', false->'split') -> hand
UPDATE "coach_training_items"
SET "hand" = CASE WHEN "both_hands" = true THEN 'both' ELSE 'split' END
WHERE "both_hands" IS NOT NULL;

-- Rename item type 'hangboard' -> 'repeater'
UPDATE "coach_training_items" SET "type" = 'repeater' WHERE "type" = 'hangboard';

-- Drop old columns
ALTER TABLE "coach_training_items"
  DROP COLUMN "hb_worktime_seconds",
  DROP COLUMN "both_hands";
