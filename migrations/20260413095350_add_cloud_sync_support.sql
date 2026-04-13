-- Modify "sessions" table
ALTER TABLE "sessions" ALTER COLUMN "id" DROP DEFAULT, ALTER COLUMN "id" TYPE uuid, ALTER COLUMN "id" SET DEFAULT gen_random_uuid(), ADD COLUMN "created_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "deleted_at" timestamptz NULL, ADD COLUMN "device_id" uuid NULL, ADD COLUMN "sync_version" bigint NOT NULL DEFAULT 1, ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now();
-- Drop sequence used by serial column "id"
DROP SEQUENCE IF EXISTS "sessions_id_seq";
-- Create index "sessions_user_sync_idx" to table: "sessions"
CREATE INDEX "sessions_user_sync_idx" ON "sessions" ("user_id", "sync_version");
-- Modify "trainings" table
ALTER TABLE "trainings" ALTER COLUMN "id" DROP DEFAULT, ALTER COLUMN "id" TYPE uuid, ALTER COLUMN "id" SET DEFAULT gen_random_uuid(), ALTER COLUMN "repeater_id" TYPE uuid, ADD COLUMN "created_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "deleted_at" timestamptz NULL, ADD COLUMN "device_id" uuid NULL, ADD COLUMN "sync_version" bigint NOT NULL DEFAULT 1, ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now();
-- Drop sequence used by serial column "id"
DROP SEQUENCE IF EXISTS "trainings_id_seq";
-- Create index "trainings_user_sync_idx" to table: "trainings"
CREATE INDEX "trainings_user_sync_idx" ON "trainings" ("user_id", "sync_version");
-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "last_seen_at" timestamptz NULL;
-- Modify "assessments" table
ALTER TABLE "assessments" ALTER COLUMN "id" DROP DEFAULT, ALTER COLUMN "id" TYPE uuid, ALTER COLUMN "id" SET DEFAULT gen_random_uuid(), ALTER COLUMN "session_id" TYPE uuid, ADD COLUMN "created_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "deleted_at" timestamptz NULL, ADD COLUMN "device_id" uuid NULL, ADD COLUMN "sync_version" bigint NOT NULL DEFAULT 1, ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "user_id" uuid NOT NULL, ADD CONSTRAINT "assessments_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Drop sequence used by serial column "id"
DROP SEQUENCE IF EXISTS "assessments_id_seq";
-- Create index "assessments_user_id_idx" to table: "assessments"
CREATE INDEX "assessments_user_id_idx" ON "assessments" ("user_id");
-- Create index "assessments_user_sync_idx" to table: "assessments"
CREATE INDEX "assessments_user_sync_idx" ON "assessments" ("user_id", "sync_version");
-- Modify "rep_datas" table
ALTER TABLE "rep_datas" ALTER COLUMN "id" DROP DEFAULT, ALTER COLUMN "id" TYPE uuid, ALTER COLUMN "id" SET DEFAULT gen_random_uuid(), ALTER COLUMN "session_id" TYPE uuid, ADD COLUMN "created_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "deleted_at" timestamptz NULL, ADD COLUMN "device_id" uuid NULL, ADD COLUMN "sync_version" bigint NOT NULL DEFAULT 1, ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "user_id" uuid NOT NULL, ADD CONSTRAINT "rep_datas_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Drop sequence used by serial column "id"
DROP SEQUENCE IF EXISTS "rep_datas_id_seq";
-- Create index "rep_datas_user_id_idx" to table: "rep_datas"
CREATE INDEX "rep_datas_user_id_idx" ON "rep_datas" ("user_id");
-- Create index "rep_datas_user_sync_idx" to table: "rep_datas"
CREATE INDEX "rep_datas_user_sync_idx" ON "rep_datas" ("user_id", "sync_version");
-- Modify "rep_templates" table
ALTER TABLE "rep_templates" ALTER COLUMN "id" DROP DEFAULT, ALTER COLUMN "id" TYPE uuid, ALTER COLUMN "id" SET DEFAULT gen_random_uuid(), ALTER COLUMN "training_id" TYPE uuid, ADD COLUMN "created_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "deleted_at" timestamptz NULL, ADD COLUMN "device_id" uuid NULL, ADD COLUMN "sync_version" bigint NOT NULL DEFAULT 1, ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "user_id" uuid NOT NULL, ADD CONSTRAINT "rep_templates_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Drop sequence used by serial column "id"
DROP SEQUENCE IF EXISTS "rep_templates_id_seq";
-- Create index "rep_templates_user_id_idx" to table: "rep_templates"
CREATE INDEX "rep_templates_user_id_idx" ON "rep_templates" ("user_id");
-- Create index "rep_templates_user_sync_idx" to table: "rep_templates"
CREATE INDEX "rep_templates_user_sync_idx" ON "rep_templates" ("user_id", "sync_version");
-- Modify "repeaters" table
ALTER TABLE "repeaters" ALTER COLUMN "id" DROP DEFAULT, ALTER COLUMN "id" TYPE uuid, ALTER COLUMN "id" SET DEFAULT gen_random_uuid(), ADD COLUMN "created_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "deleted_at" timestamptz NULL, ADD COLUMN "device_id" uuid NULL, ADD COLUMN "sync_version" bigint NOT NULL DEFAULT 1, ADD COLUMN "server_updated_at" timestamptz NOT NULL DEFAULT now(), ADD COLUMN "user_id" uuid NOT NULL, ADD CONSTRAINT "repeaters_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Drop sequence used by serial column "id"
DROP SEQUENCE IF EXISTS "repeaters_id_seq";
-- Create index "repeaters_user_id_idx" to table: "repeaters"
CREATE INDEX "repeaters_user_id_idx" ON "repeaters" ("user_id");
-- Create index "repeaters_user_sync_idx" to table: "repeaters"
CREATE INDEX "repeaters_user_sync_idx" ON "repeaters" ("user_id", "sync_version");
