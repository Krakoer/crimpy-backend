-- Rename tables; all data is preserved via RENAME (no CREATE/DROP).
ALTER TABLE "coach_training_items" RENAME TO "training_items";
ALTER TABLE "coach_trainings" RENAME TO "trainings";

-- Rename indexes to match new table names
ALTER INDEX "coach_trainings_user_id_idx" RENAME TO "trainings_user_id_idx";
ALTER INDEX "coach_training_items_training_id_idx" RENAME TO "training_items_training_id_idx";
ALTER INDEX "coach_training_items_parent_id_idx" RENAME TO "training_items_parent_id_idx";

-- Update FK in coach_program_week_sessions (now references renamed trainings table)
ALTER TABLE "coach_program_week_sessions"
  DROP CONSTRAINT "coach_program_week_sessions_training_id_fkey",
  ADD CONSTRAINT "coach_program_week_sessions_training_id_fkey"
    FOREIGN KEY ("training_id") REFERENCES "trainings" ("id") ON DELETE RESTRICT;

-- Update FK in coach_program_session_overrides (now references renamed training_items table)
ALTER TABLE "coach_program_session_overrides"
  DROP CONSTRAINT "coach_program_session_overrides_item_id_fkey",
  ADD CONSTRAINT "coach_program_session_overrides_item_id_fkey"
    FOREIGN KEY ("item_id") REFERENCES "training_items" ("id") ON DELETE CASCADE;
