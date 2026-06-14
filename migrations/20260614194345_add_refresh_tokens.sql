-- Create "refresh_tokens" table
CREATE TABLE "refresh_tokens" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "token_hash" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "revoked" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "refresh_tokens_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "refresh_tokens_token_hash_key" to table: "refresh_tokens"
CREATE UNIQUE INDEX "refresh_tokens_token_hash_key" ON "refresh_tokens" ("token_hash");
-- Create index "refresh_tokens_user_id_idx" to table: "refresh_tokens"
CREATE INDEX "refresh_tokens_user_id_idx" ON "refresh_tokens" ("user_id");
