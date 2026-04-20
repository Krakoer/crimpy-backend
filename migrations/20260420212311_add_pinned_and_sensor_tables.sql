-- Create "builtin_training_weights" table
CREATE TABLE "builtin_training_weights" (
  "id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "builtin_traning_id" uuid NOT NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "custom_weight_right" real NOT NULL,
  "custom_weight_left" real NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "builtin_training_weights_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "builtin_training_weights_user_id_idx" to table: "builtin_training_weights"
CREATE INDEX "builtin_training_weights_user_id_idx" ON "builtin_training_weights" ("user_id");
-- Create index "builtin_training_weights_user_sync_idx" to table: "builtin_training_weights"
CREATE INDEX "builtin_training_weights_user_sync_idx" ON "builtin_training_weights" ("user_id", "sync_version");
-- Create "pinned_builtin_trainings" table
CREATE TABLE "pinned_builtin_trainings" (
  "builtin_training_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("builtin_training_id"),
  CONSTRAINT "pinned_builtin_trainings_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "pinned_builtin_trainings_user_id_idx" to table: "pinned_builtin_trainings"
CREATE INDEX "pinned_builtin_trainings_user_id_idx" ON "pinned_builtin_trainings" ("user_id");
-- Create index "pinned_builtin_trainings_user_sync_idx" to table: "pinned_builtin_trainings"
CREATE INDEX "pinned_builtin_trainings_user_sync_idx" ON "pinned_builtin_trainings" ("user_id", "sync_version");
-- Create "sensor_configs" table
CREATE TABLE "sensor_configs" (
  "id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "sync_version" bigint NOT NULL DEFAULT 1,
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "deleted_at" timestamptz NULL,
  "name" text NOT NULL,
  "index" bigint NOT NULL,
  "tare" real NOT NULL,
  "coef" real NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "sensor_configs_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "sensor_configs_user_id_idx" to table: "sensor_configs"
CREATE INDEX "sensor_configs_user_id_idx" ON "sensor_configs" ("user_id");
-- Create index "sensor_configs_user_sync_idx" to table: "sensor_configs"
CREATE INDEX "sensor_configs_user_sync_idx" ON "sensor_configs" ("user_id", "sync_version");
