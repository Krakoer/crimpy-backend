-- Modify "sessions" table
ALTER TABLE "sessions" DROP COLUMN "data_path";
-- Modify "trainings" table
ALTER TABLE "trainings" DROP COLUMN "is_builtin";
