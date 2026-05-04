-- Modify "coach_session_items" table
ALTER TABLE "coach_session_items" DROP CONSTRAINT "coach_session_items_exercise_id_fkey", DROP COLUMN "reps_unit", ADD COLUMN "duration" integer NULL, ADD CONSTRAINT "coach_session_items_exercise_id_fkey" FOREIGN KEY ("exercise_id") REFERENCES "exercises" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
