-- Create "exercises" table
CREATE TABLE "exercises" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "coach_id" uuid NOT NULL,
  "name" text NOT NULL,
  "description" text NULL,
  "comment" text NULL,
  "video_link" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "exercises_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "exercises_coach_id_idx" to table: "exercises"
CREATE INDEX "exercises_coach_id_idx" ON "exercises" ("coach_id");
