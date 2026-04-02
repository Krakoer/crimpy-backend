CREATE TABLE "users" (
  "id"           UUID        NOT NULL DEFAULT gen_random_uuid(),
  "email"        TEXT        NOT NULL,
  "password"     TEXT        NOT NULL,
  "firstname"    TEXT        NOT NULL,
  "lastname"     TEXT        NOT NULL,
  "created_at"   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "users_email_key" ON "users" ("email");
