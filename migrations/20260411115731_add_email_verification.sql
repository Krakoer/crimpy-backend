-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "email_verified" boolean NOT NULL DEFAULT false, ADD COLUMN "verification_token" text NULL, ADD COLUMN "verification_token_expires_at" timestamptz NULL, ADD COLUMN "verification_email_sent_at" timestamptz NULL;
