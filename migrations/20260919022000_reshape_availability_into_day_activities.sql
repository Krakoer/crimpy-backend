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
-- The trim is widened past what btrim strips by default, which is the space
-- alone, to every code point unicode.IsSpace answers true for. That is the set
-- strings.TrimSpace takes off in the handler, so the two agree on what counts
-- as blank; the list is spelled out because Postgres has no name for it.
--
-- It matters because the old write path stored the note untrimmed, so a note of
-- nothing but a tab, a non breaking space or an ideographic space is a row that
-- can exist. Trimmed of spaces alone it would migrate into a label that looks
-- blank on screen and that the new API then refuses, leaving the week
-- unsaveable until the athlete deletes a character they cannot see.
--
-- Computed once in the CTE so the test for "did this day say anything" and the
-- value written cannot disagree.
--
-- The label is cut to the 200 characters the new API accepts. The old note was
-- allowed 2000, and the week is written whole, so a longer one carried across
-- intact would come back on the next save of that week as a validation error
-- about a field the athlete never typed.
WITH "trimmed" AS (
  SELECT
    a."user_id",
    a."week_start",
    a."day_of_week",
    a."is_available",
    a."duration_minutes",
    a."created_at",
    NULLIF(btrim(a."note", E' \t\n\r\f\v\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000'), '') AS "clean_note"
  FROM "coachee_day_availabilities" a
)
INSERT INTO "coachee_day_activities"
  ("declaration_id", "day_of_week", "position", "label", "duration_minutes", "created_at")
SELECT
  d."id",
  t."day_of_week",
  0,
  left(COALESCE(t."clean_note", 'Training'), 200),
  CASE WHEN t."is_available" THEN t."duration_minutes" END,
  t."created_at"
FROM "trimmed" t
JOIN "coachee_week_declarations" d
  ON d."user_id" = t."user_id" AND d."week_start" = t."week_start"
WHERE t."is_available" OR t."clean_note" IS NOT NULL;

DROP TABLE "coachee_day_availabilities";
