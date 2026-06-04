-- Modify "coach_program_week_sessions" table
ALTER TABLE "coach_program_week_sessions" DROP CONSTRAINT "cpws_mode_check", ADD CONSTRAINT "cpws_mode_check" CHECK (((day_of_week IS NOT NULL) AND (times_per_week IS NULL) AND (is_everyday = false)) OR ((day_of_week IS NULL) AND (times_per_week IS NOT NULL) AND (is_everyday = false)) OR ((day_of_week IS NULL) AND (times_per_week IS NULL) AND (is_everyday = true))), ADD COLUMN "is_everyday" boolean NOT NULL DEFAULT false;
-- Rename a constraint from "coach_trainings_pkey" to "trainings_pkey"
ALTER TABLE "trainings" RENAME CONSTRAINT "coach_trainings_pkey" TO "trainings_pkey";
-- Modify "trainings" table
ALTER TABLE "trainings" DROP CONSTRAINT "coach_trainings_user_id_fkey", ADD CONSTRAINT "trainings_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Rename a constraint from "coach_training_items_pkey" to "training_items_pkey"
ALTER TABLE "training_items" RENAME CONSTRAINT "coach_training_items_pkey" TO "training_items_pkey";
-- Modify "training_items" table
ALTER TABLE "training_items" DROP CONSTRAINT "coach_training_items_exercise_id_fkey", DROP CONSTRAINT "coach_training_items_parent_id_fkey", DROP CONSTRAINT "coach_training_items_training_id_fkey", ADD CONSTRAINT "training_items_exercise_id_fkey" FOREIGN KEY ("exercise_id") REFERENCES "exercises" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "training_items_parent_id_fkey" FOREIGN KEY ("parent_id") REFERENCES "training_items" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "training_items_training_id_fkey" FOREIGN KEY ("training_id") REFERENCES "trainings" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
