-- Modify "refresh_tokens" table
ALTER TABLE "refresh_tokens" ADD COLUMN "revoked_at" timestamptz NULL, ADD COLUMN "replaced_by" uuid NULL, ADD CONSTRAINT "refresh_tokens_replaced_by_fkey" FOREIGN KEY ("replaced_by") REFERENCES "refresh_tokens" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
