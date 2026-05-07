-- Create "tags" table
CREATE TABLE "tags" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "coach_id" uuid NOT NULL,
  "name" text NOT NULL,
  "color" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "tags_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "tags_coach_id_idx" to table: "tags"
CREATE INDEX "tags_coach_id_idx" ON "tags" ("coach_id");
-- Create "exercise_tags" table
CREATE TABLE "exercise_tags" (
  "exercise_id" uuid NOT NULL,
  "tag_id" uuid NOT NULL,
  PRIMARY KEY ("exercise_id", "tag_id"),
  CONSTRAINT "exercise_tags_exercise_id_fkey" FOREIGN KEY ("exercise_id") REFERENCES "exercises" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "exercise_tags_tag_id_fkey" FOREIGN KEY ("tag_id") REFERENCES "tags" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
