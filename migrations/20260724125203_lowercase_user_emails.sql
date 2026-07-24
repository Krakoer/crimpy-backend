-- Emails become case insensitive: normalize existing rows, then enforce
-- uniqueness on the lowercased value.
UPDATE "users" SET "email" = lower("email") WHERE "email" <> lower("email");
-- Drop index "users_email_key" from table: "users"
DROP INDEX "users_email_key";
-- Create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX "users_email_key" ON "users" ((lower(email)));
