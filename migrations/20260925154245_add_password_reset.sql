-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "password_reset_token_hash" text NULL, ADD COLUMN "password_reset_requested_at" timestamptz NULL;
-- Create index "users_password_reset_token_hash_idx" to table: "users"
CREATE INDEX "users_password_reset_token_hash_idx" ON "users" ("password_reset_token_hash");
