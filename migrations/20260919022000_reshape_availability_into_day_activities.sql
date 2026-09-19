-- Reshape a declared week from one row per day into a per-week declaration that
-- an unbounded list of activities hangs off.
--
-- The old table carried the declaration implicitly: any row for a week meant the
-- coachee had answered for it. An unbounded list has no such row for a day with
-- nothing on it, so a week the athlete declares entirely free would produce no
-- rows at all and read back as never answered. "coachee_week_declarations"
-- states the declaration in its own right, which is what this migration has to
-- carry across without losing a single week.

CREATE TABLE "coachee_week_declarations" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "user_id" uuid NOT NULL,
  "week_start" date NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coachee_week_declarations_user_id_week_start_key" UNIQUE ("user_id", "week_start"),
  CONSTRAINT "coachee_week_declarations_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "cwd_week_start_monday_check" CHECK (EXTRACT(isodow FROM week_start) = (1)::numeric)
);

-- Every distinct week present in the old table, whatever its days said. A week
-- whose seven rows were all "not available" carried no information beyond the
-- declaration itself, and is exactly the week that would be lost by backfilling
-- off the activities instead.
INSERT INTO "coachee_week_declarations" ("user_id", "week_start", "created_at", "updated_at")
SELECT "user_id", "week_start", MIN("created_at"), MAX("updated_at")
FROM "coachee_day_availabilities"
GROUP BY "user_id", "week_start";

CREATE TABLE "coachee_day_activities" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "declaration_id" uuid NOT NULL,
  "day_of_week" integer NOT NULL,
  "position" integer NOT NULL,
  "label" text NOT NULL,
  "duration_minutes" integer NULL,
  "when_text" text NULL,
  "where_text" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "coachee_day_activities_declaration_id_day_of_week_position_key" UNIQUE ("declaration_id", "day_of_week", "position"),
  CONSTRAINT "coachee_day_activities_declaration_id_fkey" FOREIGN KEY ("declaration_id") REFERENCES "coachee_week_declarations" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "cda_day_range_check" CHECK ((day_of_week >= 0) AND (day_of_week <= 6)),
  CONSTRAINT "cda_duration_check" CHECK ((duration_minutes IS NULL) OR (duration_minutes > 0)),
  CONSTRAINT "cda_label_not_blank_check" CHECK (btrim(label) <> ''::text),
  CONSTRAINT "cda_position_check" CHECK (position >= 0)
);

-- A day that said anything becomes its one activity. The note is the label when
-- there is one, since it is what the athlete actually wrote about that day, and
-- that holds for a day declared unavailable too: "travelling" is a thing they
-- are doing, which is what an activity now is. An available day with no note
-- keeps its duration under a plain label. A day that said nothing produces no
-- activity, which is an empty day under the new shape and not a lost one: its
-- week is already carried by the declaration above.
--
-- The label is cut to the 200 characters the new API accepts. The old note was
-- allowed 2000, and the week is written whole, so a longer one carried across
-- intact would come back on the next save of that week as a validation error
-- about a field the athlete never typed, and the week could not be saved again
-- until they shortened it by hand.
INSERT INTO "coachee_day_activities"
  ("declaration_id", "day_of_week", "position", "label", "duration_minutes", "created_at")
SELECT
  d."id",
  a."day_of_week",
  0,
  left(COALESCE(NULLIF(btrim(a."note"), ''), 'Training'), 200),
  CASE WHEN a."is_available" THEN a."duration_minutes" END,
  a."created_at"
FROM "coachee_day_availabilities" a
JOIN "coachee_week_declarations" d
  ON d."user_id" = a."user_id" AND d."week_start" = a."week_start"
WHERE a."is_available" OR NULLIF(btrim(a."note"), '') IS NOT NULL;

DROP TABLE "coachee_day_availabilities";
