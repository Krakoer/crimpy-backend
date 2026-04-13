-- Create "users" table
CREATE TABLE "users" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "email" text NOT NULL,
  "password" text NOT NULL,
  "firstname" text NOT NULL,
  "lastname" text NOT NULL,
  "is_admin" boolean NOT NULL DEFAULT false,
  "is_coach" boolean NOT NULL DEFAULT false,
  "coach_validated" boolean NOT NULL DEFAULT false,
  "email_verified" boolean NOT NULL DEFAULT false,
  "verification_token" text NULL,
  "verification_token_expires_at" timestamptz NULL,
  "verification_email_sent_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "last_seen_at" timestamptz NULL,
  PRIMARY KEY ("id")
);
-- Create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX "users_email_key" ON "users" ("email");
-- Create "sessions" table
CREATE TABLE "sessions" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "name" text NOT NULL,
  "notes" text NOT NULL,
  "date" timestamptz NOT NULL DEFAULT now(),
  "is_assessment" boolean NOT NULL DEFAULT false,
  "session_type" integer NOT NULL DEFAULT 0,
  "duration" integer NOT NULL DEFAULT 0,
  "repeater_sets" integer NULL,
  "repeater_reps" integer NULL,
  "repeater_work_time" integer NULL,
  "repeater_rest_time" integer NULL,
  "repeater_set_rest" integer NULL,
  "repeater_split_hand" boolean NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "device_id" uuid NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "server_updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "sessions_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "sessions_user_id_idx" to table: "sessions"
CREATE INDEX "sessions_user_id_idx" ON "sessions" ("user_id");
-- Create index "sessions_user_sync_idx" to table: "sessions"
CREATE INDEX "sessions_user_sync_idx" ON "sessions" ("user_id", "sync_version");
-- Create "assessments" table
CREATE TABLE "assessments" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "type" integer NOT NULL,
  "right_value" real NULL,
  "left_value" real NULL,
  "session_id" uuid NOT NULL,
  "grip_position" integer NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "device_id" uuid NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "server_updated_at" timestamptz NOT NULL DEFAULT now(),
  "user_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "assessments_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assessments_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "assessments_session_id_idx" to table: "assessments"
CREATE INDEX "assessments_session_id_idx" ON "assessments" ("session_id");
-- Create index "assessments_user_id_idx" to table: "assessments"
CREATE INDEX "assessments_user_id_idx" ON "assessments" ("user_id");
-- Create index "assessments_user_sync_idx" to table: "assessments"
CREATE INDEX "assessments_user_sync_idx" ON "assessments" ("user_id", "sync_version");
-- Create "rep_datas" table
CREATE TABLE "rep_datas" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "average_weight" real NOT NULL,
  "session_id" uuid NOT NULL,
  "is_rest" boolean NOT NULL,
  "right_hand" boolean NOT NULL,
  "duration" integer NOT NULL,
  "target_weight" real NOT NULL,
  "index" integer NOT NULL,
  "grip_position" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "device_id" uuid NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "server_updated_at" timestamptz NOT NULL DEFAULT now(),
  "user_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "rep_datas_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "rep_datas_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "rep_datas_session_id_idx" to table: "rep_datas"
CREATE INDEX "rep_datas_session_id_idx" ON "rep_datas" ("session_id");
-- Create index "rep_datas_user_id_idx" to table: "rep_datas"
CREATE INDEX "rep_datas_user_id_idx" ON "rep_datas" ("user_id");
-- Create index "rep_datas_user_sync_idx" to table: "rep_datas"
CREATE INDEX "rep_datas_user_sync_idx" ON "rep_datas" ("user_id", "sync_version");
-- Create "repeaters" table
CREATE TABLE "repeaters" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "sets" integer NOT NULL,
  "reps" integer NOT NULL,
  "worktime" integer NOT NULL,
  "resttime" integer NOT NULL,
  "set_rest" integer NOT NULL,
  "target_weight_right" real NULL,
  "target_weight_left" real NULL,
  "split_hand" boolean NOT NULL,
  "grip_position" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "device_id" uuid NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "server_updated_at" timestamptz NOT NULL DEFAULT now(),
  "user_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "repeaters_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "repeaters_user_id_idx" to table: "repeaters"
CREATE INDEX "repeaters_user_id_idx" ON "repeaters" ("user_id");
-- Create index "repeaters_user_sync_idx" to table: "repeaters"
CREATE INDEX "repeaters_user_sync_idx" ON "repeaters" ("user_id", "sync_version");
-- Create "trainings" table
CREATE TABLE "trainings" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "name" text NOT NULL,
  "repeater_id" uuid NULL,
  "is_favorite" boolean NOT NULL DEFAULT false,
  "is_assessment" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "device_id" uuid NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "server_updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "trainings_repeater_id_fkey" FOREIGN KEY ("repeater_id") REFERENCES "repeaters" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "trainings_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "trainings_repeater_id_idx" to table: "trainings"
CREATE INDEX "trainings_repeater_id_idx" ON "trainings" ("repeater_id");
-- Create index "trainings_user_id_idx" to table: "trainings"
CREATE INDEX "trainings_user_id_idx" ON "trainings" ("user_id");
-- Create index "trainings_user_sync_idx" to table: "trainings"
CREATE INDEX "trainings_user_sync_idx" ON "trainings" ("user_id", "sync_version");
-- Create "rep_templates" table
CREATE TABLE "rep_templates" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "is_rest" boolean NOT NULL,
  "right_hand" boolean NOT NULL,
  "duration" integer NOT NULL,
  "training_id" uuid NOT NULL,
  "target_weight" real NOT NULL,
  "index" integer NOT NULL,
  "grip_position" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "device_id" uuid NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "server_updated_at" timestamptz NOT NULL DEFAULT now(),
  "user_id" uuid NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "rep_templates_training_id_fkey" FOREIGN KEY ("training_id") REFERENCES "trainings" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "rep_templates_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "rep_templates_training_id_idx" to table: "rep_templates"
CREATE INDEX "rep_templates_training_id_idx" ON "rep_templates" ("training_id");
-- Create index "rep_templates_user_id_idx" to table: "rep_templates"
CREATE INDEX "rep_templates_user_id_idx" ON "rep_templates" ("user_id");
-- Create index "rep_templates_user_sync_idx" to table: "rep_templates"
CREATE INDEX "rep_templates_user_sync_idx" ON "rep_templates" ("user_id", "sync_version");
