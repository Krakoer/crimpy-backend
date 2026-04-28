-- Create "coach_enrollments" table
CREATE TABLE "coach_enrollments" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "coach_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "enrolled_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_enrollments_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "coach_enrollments_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "coach_enrollments_coach_id_idx" to table: "coach_enrollments"
CREATE INDEX "coach_enrollments_coach_id_idx" ON "coach_enrollments" ("coach_id");
-- Create index "coach_enrollments_user_id_key" to table: "coach_enrollments"
CREATE UNIQUE INDEX "coach_enrollments_user_id_key" ON "coach_enrollments" ("user_id");
-- Create "enrollment_tokens" table
CREATE TABLE "enrollment_tokens" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "coach_id" uuid NOT NULL,
  "token" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "used_at" timestamptz NULL,
  "used_by" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "enrollment_tokens_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "enrollment_tokens_used_by_fkey" FOREIGN KEY ("used_by") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "enrollment_tokens_coach_id_idx" to table: "enrollment_tokens"
CREATE INDEX "enrollment_tokens_coach_id_idx" ON "enrollment_tokens" ("coach_id");
-- Create index "enrollment_tokens_token_key" to table: "enrollment_tokens"
CREATE UNIQUE INDEX "enrollment_tokens_token_key" ON "enrollment_tokens" ("token");
