-- Create "coach_todo_settings" table
CREATE TABLE "coach_todo_settings" (
  "coach_id" uuid NOT NULL,
  "empty_week_day_of_week" integer NOT NULL DEFAULT 4,
  "empty_week_hour" integer NOT NULL DEFAULT 21,
  "empty_week_minute" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("coach_id"),
  CONSTRAINT "coach_todo_settings_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "cts_day_range_check" CHECK ((empty_week_day_of_week >= 0) AND (empty_week_day_of_week <= 6)),
  CONSTRAINT "cts_hour_range_check" CHECK ((empty_week_hour >= 0) AND (empty_week_hour <= 23)),
  CONSTRAINT "cts_minute_range_check" CHECK ((empty_week_minute >= 0) AND (empty_week_minute <= 59))
);
