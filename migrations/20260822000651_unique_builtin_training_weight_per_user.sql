-- Drop the duplicate weight overrides the app created before it could find and
-- update an existing row, keeping the most recently written one per training.
DELETE FROM "builtin_training_weights" a
USING "builtin_training_weights" b
WHERE a."user_id" = b."user_id"
  AND a."builtin_training_id" = b."builtin_training_id"
  AND (a."updated_at", a."id") < (b."updated_at", b."id");

-- Modify "builtin_training_weights" table
ALTER TABLE "builtin_training_weights" ADD CONSTRAINT "builtin_training_weights_user_training_unique" UNIQUE ("user_id", "builtin_training_id");
