-- Modify "assessments" table
ALTER TABLE "assessments" DROP COLUMN "deleted_at", DROP COLUMN "sync_version", DROP COLUMN "server_updated_at";
-- Modify "builtin_training_weights" table
ALTER TABLE "builtin_training_weights" DROP COLUMN "sync_version", DROP COLUMN "deleted_at", DROP COLUMN "server_updated_at";
-- Modify "pinned_builtin_trainings" table
ALTER TABLE "pinned_builtin_trainings" DROP CONSTRAINT "pinned_builtin_trainings_pkey", DROP COLUMN "sync_version", DROP COLUMN "deleted_at", DROP COLUMN "server_updated_at", ADD PRIMARY KEY ("builtin_training_id", "user_id");
-- Modify "rep_datas" table
ALTER TABLE "rep_datas" DROP COLUMN "deleted_at", DROP COLUMN "sync_version", DROP COLUMN "server_updated_at";
-- Modify "rep_templates" table
ALTER TABLE "rep_templates" DROP COLUMN "deleted_at", DROP COLUMN "sync_version", DROP COLUMN "server_updated_at";
-- Modify "repeaters" table
ALTER TABLE "repeaters" DROP COLUMN "deleted_at", DROP COLUMN "sync_version", DROP COLUMN "server_updated_at";
-- Modify "sensor_configs" table
ALTER TABLE "sensor_configs" DROP COLUMN "sync_version", DROP COLUMN "deleted_at", DROP COLUMN "server_updated_at";
-- Modify "sessions" table
ALTER TABLE "sessions" DROP COLUMN "deleted_at", DROP COLUMN "sync_version", DROP COLUMN "server_updated_at";
-- Modify "trainings" table
ALTER TABLE "trainings" DROP COLUMN "deleted_at", DROP COLUMN "sync_version", DROP COLUMN "server_updated_at";
