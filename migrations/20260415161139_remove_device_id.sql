-- Modify "assessments" table
ALTER TABLE "assessments" DROP COLUMN "device_id";
-- Modify "rep_datas" table
ALTER TABLE "rep_datas" DROP COLUMN "device_id";
-- Modify "rep_templates" table
ALTER TABLE "rep_templates" DROP COLUMN "device_id";
-- Modify "repeaters" table
ALTER TABLE "repeaters" DROP COLUMN "device_id";
-- Modify "sessions" table
ALTER TABLE "sessions" DROP COLUMN "device_id";
-- Modify "trainings" table
ALTER TABLE "trainings" DROP COLUMN "device_id";
