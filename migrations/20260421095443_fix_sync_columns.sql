-- Modify "assessments" table
ALTER TABLE "assessments" DROP COLUMN "created_at";
-- Modify "builtin_training_weights" table
ALTER TABLE "builtin_training_weights" ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now();
-- Modify "pinned_builtin_trainings" table
ALTER TABLE "pinned_builtin_trainings" ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now();
-- Modify "rep_datas" table
ALTER TABLE "rep_datas" DROP COLUMN "created_at";
-- Modify "rep_templates" table
ALTER TABLE "rep_templates" DROP COLUMN "created_at";
-- Modify "repeaters" table
ALTER TABLE "repeaters" DROP COLUMN "created_at";
-- Modify "sensor_configs" table
ALTER TABLE "sensor_configs" ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now();
-- Modify "sessions" table
ALTER TABLE "sessions" DROP COLUMN "created_at";
-- Modify "trainings" table
ALTER TABLE "trainings" DROP COLUMN "created_at";
