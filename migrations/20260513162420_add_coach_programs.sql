-- Create "coach_programs" table
CREATE TABLE "coach_programs" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "coach_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "name" text NOT NULL,
  "objective" text NULL,
  "start_date" date NOT NULL,
  "duration_weeks" integer NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_programs_coach_id_fkey" FOREIGN KEY ("coach_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "coach_programs_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "coach_programs_coach_id_idx" to table: "coach_programs"
CREATE INDEX "coach_programs_coach_id_idx" ON "coach_programs" ("coach_id");
-- Create index "coach_programs_user_id_idx" to table: "coach_programs"
CREATE INDEX "coach_programs_user_id_idx" ON "coach_programs" ("user_id");
-- Create "coach_program_slots" table
CREATE TABLE "coach_program_slots" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "program_id" uuid NOT NULL,
  "training_id" uuid NOT NULL,
  "day_of_week" integer NULL,
  "times_per_week" integer NULL,
  "position" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coach_program_slots_program_id_fkey" FOREIGN KEY ("program_id") REFERENCES "coach_programs" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "coach_program_slots_training_id_fkey" FOREIGN KEY ("training_id") REFERENCES "coach_trainings" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "coach_program_slots_day_range_check" CHECK ((day_of_week IS NULL) OR ((day_of_week >= 0) AND (day_of_week <= 6))),
  CONSTRAINT "coach_program_slots_mode_check" CHECK (((day_of_week IS NOT NULL) AND (times_per_week IS NULL)) OR ((day_of_week IS NULL) AND (times_per_week IS NOT NULL))),
  CONSTRAINT "coach_program_slots_times_check" CHECK ((times_per_week IS NULL) OR (times_per_week > 0))
);
-- Create index "coach_program_slots_program_id_idx" to table: "coach_program_slots"
CREATE INDEX "coach_program_slots_program_id_idx" ON "coach_program_slots" ("program_id");
