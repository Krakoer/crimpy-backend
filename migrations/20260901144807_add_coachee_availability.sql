-- Create "coach_availability_reminders" table
CREATE TABLE "coach_availability_reminders" (
  "coach_id" uuid NOT NULL,
  "enabled" boolean NOT NULL DEFAULT false,
  "day_of_week" integer NOT NULL,
  "hour" integer NOT NULL,
  "minute" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("coach_id"),
  CONSTRAINT "coach_availability_reminders_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "car_day_range_check" CHECK ((day_of_week >= 0) AND (day_of_week <= 6)),
  CONSTRAINT "car_hour_range_check" CHECK ((hour >= 0) AND (hour <= 23)),
  CONSTRAINT "car_minute_range_check" CHECK ((minute >= 0) AND (minute <= 59))
);
-- Create "coachee_day_availabilities" table
CREATE TABLE "coachee_day_availabilities" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "week_start" date NOT NULL,
  "day_of_week" integer NOT NULL,
  "is_available" boolean NOT NULL DEFAULT false,
  "duration_minutes" integer NULL,
  "note" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coachee_day_availabilities_user_id_week_start_day_of_week_key" UNIQUE ("user_id", "week_start", "day_of_week"),
  CONSTRAINT "coachee_day_availabilities_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "cda_day_range_check" CHECK ((day_of_week >= 0) AND (day_of_week <= 6)),
  CONSTRAINT "cda_duration_check" CHECK ((duration_minutes IS NULL) OR (duration_minutes > 0)),
  CONSTRAINT "cda_week_start_monday_check" CHECK (EXTRACT(isodow FROM week_start) = (1)::numeric)
);
-- Create index "coachee_day_availabilities_user_week_idx" to table: "coachee_day_availabilities"
CREATE INDEX "coachee_day_availabilities_user_week_idx" ON "coachee_day_availabilities" ("user_id", "week_start");
