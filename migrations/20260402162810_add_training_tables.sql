-- Create "sessions" table
CREATE TABLE "sessions" (
  "id" serial NOT NULL,
  "user_id" uuid NOT NULL,
  "name" text NOT NULL,
  "notes" text NOT NULL,
  "date" timestamptz NOT NULL DEFAULT now(),
  "data_path" text NOT NULL,
  "is_assessment" boolean NOT NULL DEFAULT false,
  "session_type" integer NOT NULL DEFAULT 0,
  "duration" integer NOT NULL DEFAULT 0,
  "repeater_sets" integer NULL,
  "repeater_reps" integer NULL,
  "repeater_work_time" integer NULL,
  "repeater_rest_time" integer NULL,
  "repeater_set_rest" integer NULL,
  "repeater_split_hand" boolean NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "sessions_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "sessions_user_id_idx" to table: "sessions"
CREATE INDEX "sessions_user_id_idx" ON "sessions" ("user_id");
-- Create "assessments" table
CREATE TABLE "assessments" (
  "id" serial NOT NULL,
  "type" integer NOT NULL,
  "right_value" real NULL,
  "left_value" real NULL,
  "session_id" integer NOT NULL,
  "grip_position" integer NULL DEFAULT 0,
  PRIMARY KEY ("id"),
  CONSTRAINT "assessments_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "assessments_session_id_idx" to table: "assessments"
CREATE INDEX "assessments_session_id_idx" ON "assessments" ("session_id");
-- Create "rep_datas" table
CREATE TABLE "rep_datas" (
  "id" serial NOT NULL,
  "average_weight" real NOT NULL,
  "session_id" integer NOT NULL,
  "is_rest" boolean NOT NULL,
  "right_hand" boolean NOT NULL,
  "duration" integer NOT NULL,
  "target_weight" real NOT NULL,
  "index" integer NOT NULL,
  "grip_position" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("id"),
  CONSTRAINT "rep_datas_session_id_fkey" FOREIGN KEY ("session_id") REFERENCES "sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "rep_datas_session_id_idx" to table: "rep_datas"
CREATE INDEX "rep_datas_session_id_idx" ON "rep_datas" ("session_id");
-- Create "repeaters" table
CREATE TABLE "repeaters" (
  "id" serial NOT NULL,
  "sets" integer NOT NULL,
  "reps" integer NOT NULL,
  "worktime" integer NOT NULL,
  "resttime" integer NOT NULL,
  "set_rest" integer NOT NULL,
  "target_weight_right" real NULL,
  "target_weight_left" real NULL,
  "split_hand" boolean NOT NULL,
  "grip_position" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("id")
);
-- Create "trainings" table
CREATE TABLE "trainings" (
  "id" serial NOT NULL,
  "user_id" uuid NOT NULL,
  "name" text NOT NULL,
  "repeater_id" integer NULL,
  "is_builtin" boolean NOT NULL DEFAULT false,
  "is_favorite" boolean NOT NULL DEFAULT false,
  "is_assessment" boolean NOT NULL DEFAULT false,
  PRIMARY KEY ("id"),
  CONSTRAINT "trainings_repeater_id_fkey" FOREIGN KEY ("repeater_id") REFERENCES "repeaters" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "trainings_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "trainings_repeater_id_idx" to table: "trainings"
CREATE INDEX "trainings_repeater_id_idx" ON "trainings" ("repeater_id");
-- Create index "trainings_user_id_idx" to table: "trainings"
CREATE INDEX "trainings_user_id_idx" ON "trainings" ("user_id");
-- Create "rep_templates" table
CREATE TABLE "rep_templates" (
  "id" serial NOT NULL,
  "is_rest" boolean NOT NULL,
  "right_hand" boolean NOT NULL,
  "duration" integer NOT NULL,
  "training_id" integer NOT NULL,
  "target_weight" real NOT NULL,
  "index" integer NOT NULL,
  "grip_position" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("id"),
  CONSTRAINT "rep_templates_training_id_fkey" FOREIGN KEY ("training_id") REFERENCES "trainings" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "rep_templates_training_id_idx" to table: "rep_templates"
CREATE INDEX "rep_templates_training_id_idx" ON "rep_templates" ("training_id");
