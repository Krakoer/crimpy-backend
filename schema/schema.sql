CREATE TABLE "users" (
  "id"                            UUID        NOT NULL DEFAULT gen_random_uuid(),
  "email"                         TEXT        NOT NULL,
  "password"                      TEXT        NOT NULL,
  "firstname"                     TEXT        NOT NULL,
  "lastname"                      TEXT        NOT NULL,
  "is_admin"                      BOOLEAN     NOT NULL DEFAULT false,
  "is_coach"                      BOOLEAN     NOT NULL DEFAULT false,
  "coach_validated"               BOOLEAN     NOT NULL DEFAULT false,
  "email_verified"                BOOLEAN     NOT NULL DEFAULT false,
  "verification_token"            TEXT,
  "verification_token_expires_at" TIMESTAMPTZ,
  "verification_email_sent_at"    TIMESTAMPTZ,
  "created_at"                    TIMESTAMPTZ NOT NULL DEFAULT now(),
  "last_seen_at"                  TIMESTAMPTZ,
  PRIMARY KEY ("id")
);

-- Emails are stored normalized to lowercase; the index enforces that two
-- addresses differing only by case cannot both be registered.
CREATE UNIQUE INDEX "users_email_key" ON "users" (lower("email"));

-- Long-lived refresh tokens used to mint new short-lived access tokens.
-- Only the SHA-256 hash of the token is stored.
CREATE TABLE "refresh_tokens" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "token_hash" TEXT        NOT NULL,
  "expires_at" TIMESTAMPTZ NOT NULL,
  "revoked"    BOOLEAN     NOT NULL DEFAULT false,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "refresh_tokens_token_hash_key" ON "refresh_tokens" ("token_hash");
CREATE INDEX "refresh_tokens_user_id_idx" ON "refresh_tokens"("user_id");

-- Stores the training sessions the user has done.
--
-- Two independent columns describe a session. "activity" is what was done and is
-- a label only: it drives colors, filters and the week histogram, never what the
-- app permits. "origin" is how the session came to exist and is set by the code
-- path that wrote it, never chosen by a coach or an athlete. Any activity can
-- have either origin, so a hangboard session played in the app and a run logged
-- by hand are both first class.
CREATE TABLE "sessions" (
  "id"                  UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"                TEXT        NOT NULL,
  "notes"               TEXT        NOT NULL,
  "date"                TIMESTAMPTZ NOT NULL DEFAULT now(),
  "is_assessment"       BOOLEAN     NOT NULL DEFAULT false,
  -- 0 hangboard, 1 climbing, 2 stretching, 3 workout, 4 other.
  "activity"            INTEGER     NOT NULL DEFAULT 0,
  -- 'played': run step by step in the app, so it owns its reps and timings and
  -- only its notes may be edited. 'logged': entered by hand afterwards, so every
  -- field stays editable.
  "origin"              TEXT        NOT NULL DEFAULT 'logged',
  -- What the session was played from, so it can be shown against what was
  -- prescribed. Both null for a session that answers no prescription, and for
  -- templates deleted since. A logged session may carry program_session_id too:
  -- a coach slot with nothing to step through is completed by hand, and that is
  -- still an answer to the prescription. Only a played one freezes the week,
  -- see is_locked in queries/coach_program_weeks.sql.
  "training_id"         UUID,
  "program_session_id"  UUID,
  -- The prescription resolved at create time: the training as it read then,
  -- with the program session overrides already merged into its items. The
  -- template it was resolved from stays editable, so only this snapshot still
  -- describes what the athlete was actually asked to do.
  --
  -- Also set, with no "training_id" beside it, on a run of a training the
  -- server cannot read: one the client generates on the device and hands over
  -- with the session. So this is not null exactly when the session answers a
  -- prescription, which is weaker than naming a training; the check below is
  -- the implication that still holds.
  "prescription"        JSONB,
  -- The force curve the sensor recorded, on an assessment session only: the
  -- samples are what a critical force or an MVC result means, while on an
  -- ordinary repeater they are noise nothing reads. Shaped {"t0", "ms", "kg"},
  -- a start instant and two equal length arrays, rather than a point per object
  -- with its own timestamp, which is about five times the bytes for the same
  -- curve. Null on every session that carries no curve. Kept off the history
  -- list query for the reason the prescription is, see GetUserSessions.
  "samples"             JSONB,
  "duration"            INTEGER     NOT NULL DEFAULT 0,
  -- The coach's answer to the notes the athlete left after the session. "notes"
  -- is the athlete's side of that exchange, so the two live on the row together
  -- rather than in a thread table: one session carries at most one exchange.
  -- Null while the coach has not answered.
  "coach_reply"         TEXT,
  -- When the answer was last written. Read back by the app to order the replies
  -- it has to announce.
  "coach_reply_at"      TIMESTAMPTZ,
  -- When the athlete opened the answer. Null on an answer still unread, which is
  -- what the app raises a notification for. Reset by a coach rewriting their
  -- answer, since the athlete has then not seen what it now says.
  "coach_reply_read_at" TIMESTAMPTZ,
  -- How much recovery the session cost, on the coach's session RPE scale: 5 is
  -- active recovery, 10 needs three or more full rest days. Reported by the
  -- athlete, and null until they do, which stays the normal case: the prompt is
  -- skippable and the value is editable long after the session.
  --
  -- Not the set RPE scale, which measures reps left in reserve and lives on the
  -- item a set was played from. A bare number is ambiguous between the two, so
  -- nothing else may be stored here.
  "rpe"                 INTEGER,
  -- The scale's ECHEC, a session the athlete could not carry through. Kept off
  -- the column above rather than given a sentinel inside it: a sentinel would
  -- have to sit outside the range the check constraint exists to hold, and
  -- every reader averaging or plotting RPE would have to know to drop it.
  "rpe_failed"          BOOLEAN     NOT NULL DEFAULT false,
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "sessions_activity_check" CHECK (activity BETWEEN 0 AND 4),
  CONSTRAINT "sessions_origin_check" CHECK (origin IN ('played', 'logged')),
  -- A session run from a training carries the snapshot of it. Holding the link
  -- without the snapshot would say the session was prescribed something while
  -- keeping no record of what, which no write path produces. Not the converse:
  -- deleting the training nulls the link and the snapshot rightly outlives it.
  CONSTRAINT "sessions_training_has_prescription_check"
    CHECK (training_id IS NULL OR prescription IS NOT NULL),
  -- Only an assessment carries a curve, which is the decision this column was
  -- added under. Enforced here so a client cannot quietly start filling it for
  -- every repeater session and grow the table by an order of magnitude.
  CONSTRAINT "sessions_samples_assessment_only_check"
    CHECK (samples IS NULL OR is_assessment),
  -- The three reply columns describe one answer, so they cannot disagree about
  -- whether there is one: no timestamp without the text it dates, and nothing
  -- read that was never written.
  CONSTRAINT "sessions_coach_reply_at_check"
    CHECK ((coach_reply IS NULL) = (coach_reply_at IS NULL)),
  CONSTRAINT "sessions_coach_reply_read_at_check"
    CHECK (coach_reply_read_at IS NULL OR coach_reply IS NOT NULL),
  -- The scale starts at 5 because that is where its written anchors start: the
  -- values below it name nothing, and a number with no anchor is what makes an
  -- RPE unreadable.
  CONSTRAINT "sessions_rpe_check"
    CHECK (rpe IS NULL OR rpe BETWEEN 5 AND 10),
  -- ECHEC is a value of the scale, not a grade beside one, so a session cannot
  -- be both failed and rated.
  CONSTRAINT "sessions_rpe_failed_check"
    CHECK (NOT rpe_failed OR rpe IS NULL)
);

-- Stores the assessments the user has done, with the results.
CREATE TABLE "assessments" (
  "id"            UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"       UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  -- Which assessment was measured. The definition is declared further down this
  -- file, so the foreign key is added at the bottom.
  "assessment_id" UUID        NOT NULL,
  "right_value"   REAL,
  "left_value"    REAL,
  "session_id"    UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "grip_position" INTEGER     DEFAULT 0,
  "updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);


-- Stores the data for the repetitions done during a session.
CREATE TABLE "rep_datas" (
  "id"             UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"        UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "average_weight" REAL        NOT NULL,
  "session_id"     UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "is_rest"        BOOLEAN     NOT NULL,
  -- Which hand pulled the rep: 'left', 'right', or 'both' for a hang the athlete
  -- took two handed. A boolean cannot carry the third state, so a two handed rep
  -- used to be stored, and read back, as the left hand.
  "hand"           TEXT        NOT NULL,
  "duration"       INTEGER     NOT NULL,
  "target_weight"  REAL        NOT NULL,
  "index"          INTEGER     NOT NULL,
  "grip_position"  INTEGER     NOT NULL DEFAULT 0,
  -- Depth in millimeters of the edge the rep was pulled on, null when the step
  -- prescribes no edge (rests, exercises done off the hangboard).
  "edge_size_mm"   INTEGER,
  -- Which item of the prescription the rep was played from, so the reps of a
  -- training can be read block by block instead of as one pooled list. Null for
  -- a rep recorded outside a training, and for sessions played before the app
  -- started sending it.
  --
  -- Deliberately not a foreign key: it points into the session's frozen
  -- prescription snapshot, which keeps the item ids it was resolved with, not
  -- into the still editable "training_items" row. A reference would have to null
  -- itself when the coach deletes the item, losing the grouping the snapshot can
  -- still describe.
  --
  -- Text rather than a uuid because a snapshot names its own items: one frozen
  -- from a training carries the row ids it was resolved with, while one a client
  -- sent for a run of nothing it owns carries the keys that client generated.
  "training_item_id" TEXT,
  -- Whether the step prescribed a load nothing measured, which is a sensor that
  -- dropped while a hang it was meant to read was running. Such a rep stores no
  -- target, exactly as a step nothing was ever going to measure does (a both
  -- hands hang, a run started without a sensor), so this flag is what tells the
  -- two apart: a rep flagged here was performed but could not be graded, so an
  -- on-target ratio leaves it out of both sides, while a step that never had a
  -- target still counts as part of the run it was played in.
  "target_unmeasured" BOOLEAN NOT NULL DEFAULT FALSE,
  "updated_at"     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "rep_datas_hand_check" CHECK (hand IN ('left', 'right', 'both')),
  CONSTRAINT "rep_datas_item_check" CHECK (
    training_item_id IS NULL
    OR (training_item_id <> '' AND char_length(training_item_id) <= 200)
  )
);

-- Stores the IDs of pinned builtin trainings
CREATE TABLE "pinned_builtin_trainings" (
  "builtin_training_id" UUID        NOT NULL,
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("builtin_training_id", "user_id")
);

-- A dated bodyweight measurement. Every finger and pulling number a coach reads
-- is a ratio to the bodyweight of the day rather than an absolute, so the series
-- is what makes those numbers comparable across a season, and what a percent_bw
-- prescription is frozen against. Deliberately a plain series: one number and
-- when it was taken, not body composition.
CREATE TABLE "user_bodyweights" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"     UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "weight_kg"   REAL        NOT NULL,
  -- When the athlete weighed themselves, which is not when the row reached the
  -- server: a measurement taken offline is sent when the device next has a
  -- network, and the day it belongs to is the day it was taken.
  "measured_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "user_bodyweights_weight_check"
    CHECK (weight_kg > 0 AND weight_kg <= 500),
  PRIMARY KEY ("id")
);

-- Stores the saved sensor configs.
CREATE TABLE "sensor_configs" (
  "id"         UUID        NOT NULL,
  "user_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"       TEXT        NOT NULL,
  "index"      BIGINT      NOT NULL,
  "tare"       REAL        NOT NULL,
  "coef"       REAL        NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

-- Stores custom weights for builtin trainings per user.
CREATE TABLE "builtin_training_weights" (
  "id"                  UUID        NOT NULL,
  "user_id"             UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "builtin_training_id" UUID        NOT NULL,
  "custom_weight_left"  REAL        NOT NULL,
  "custom_weight_right" REAL        NOT NULL,
  "updated_at"          TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "builtin_training_weights_user_training_unique" UNIQUE ("user_id", "builtin_training_id")
);

-- Stores exercises created by coaches for use in session templates.
CREATE TABLE "exercises" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"        TEXT        NOT NULL,
  "description" TEXT,
  "comment"     TEXT,
  "video_link"  TEXT,
  "is_favorite" BOOLEAN     NOT NULL DEFAULT false,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "exercises_coach_id_idx" ON "exercises"("coach_id");

-- Stores tags created by coaches to categorize exercises.
-- coach_id is NULL for system (builtin) tags.
CREATE TABLE "tags" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"   UUID        REFERENCES "users"("id") ON DELETE CASCADE,
  "name"       TEXT        NOT NULL,
  "color"      TEXT        NOT NULL,
  "is_builtin" BOOLEAN     NOT NULL DEFAULT FALSE,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "tags_coach_id_idx" ON "tags"("coach_id");

-- Associates tags with exercises (many-to-many).
CREATE TABLE "exercise_tags" (
  "exercise_id" UUID NOT NULL REFERENCES "exercises"("id") ON DELETE CASCADE,
  "tag_id"      UUID NOT NULL REFERENCES "tags"("id") ON DELETE CASCADE,
  PRIMARY KEY ("exercise_id", "tag_id")
);

-- Stores training templates for all users (user-created and coach-created).
CREATE TABLE "trainings" (
  "id"            UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"       UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "title"         TEXT        NOT NULL,
  "description"   TEXT,
  "training_type" TEXT        NOT NULL DEFAULT 'workout',
  "goal"          TEXT,
  "comment"       TEXT,
  "is_favorite"   BOOLEAN     NOT NULL DEFAULT FALSE,
  "created_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "trainings_training_type_check"
    CHECK (training_type IN ('hangboard', 'climbing', 'stretching', 'workout', 'other'))
);

CREATE INDEX "trainings_user_id_idx" ON "trainings"("user_id");

-- Stores every assessment that can be measured, the ones Crimpy ships and the
-- ones a coach writes. There is no builtin/custom split anywhere above this
-- table: a result and a percentage reference both name a row here by id.
--
-- A builtin has no owner, no training and asks no question: the app runs it from
-- a sensor protocol keyed by the row id. A custom one carries all three, and is
-- run like any other training, ending on the question its prompt asks.
CREATE TABLE "assessment_definitions" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"     UUID        REFERENCES "users"("id") ON DELETE CASCADE,
  "training_id" UUID        REFERENCES "trainings"("id") ON DELETE RESTRICT,
  "label"       TEXT        NOT NULL,
  "prompt"      TEXT,
  -- What the result is measured in, which decides the item fields a percentage
  -- of it may drive: kilograms a load, seconds a duration, repetitions reps.
  "unit"        TEXT        NOT NULL,
  -- Whether the two hands are measured apart, as a one arm test is.
  "per_hand"    BOOLEAN     NOT NULL DEFAULT FALSE,
  -- Whether a result in kilograms reads as a ratio to the bodyweight it was
  -- pulled at, (bodyweight + result) / bodyweight, rather than as a load. A
  -- finger strength number is not comparable across a season until it is
  -- divided by the weight that hung off it.
  --
  -- Strictly a display concern: the kilograms and the dated bodyweight series
  -- are what is stored, never the ratio, so the formula can be corrected
  -- without rewriting history. Nothing reads this to resolve a prescription.
  "bodyweight_relative" BOOLEAN NOT NULL DEFAULT FALSE,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "assessment_definitions_unit_check"
    CHECK (unit IN ('kilograms', 'seconds', 'repetitions')),
  -- A ratio to a bodyweight only means something when the result is a weight.
  -- Seconds or repetitions divided by kilograms is not a number anybody reads.
  CONSTRAINT "assessment_definitions_bodyweight_relative_check"
    CHECK (NOT bodyweight_relative OR unit = 'kilograms'),
  CONSTRAINT "assessment_definitions_builtin_check"
    CHECK (num_nonnulls(user_id, training_id, prompt) IN (0, 3))
);

CREATE INDEX "assessment_definitions_user_id_idx" ON "assessment_definitions"("user_id");
CREATE UNIQUE INDEX "assessment_definitions_training_id_idx"
  ON "assessment_definitions"("training_id") WHERE training_id IS NOT NULL;

-- Stores individual items within a training (repeaters, hangboard reps, free notes,
-- exercises, circuits, groups). Items are stored flat; nesting is via parent_id.
--
-- Hangboard configuration is stored as JSONB arrays whose layout is declared by
-- the granularity column rather than inferred from any array length. Every
-- client writes the same shape: one entry per configuration row, with the row
-- count fixed by the granularity (1 uniform, reps per rep, cycles * reps per
-- set indexed set * reps + rep). Per-hand fields carry one array per hand so a
-- row index always means the same thing in every array.
--
-- Types: 'repeater', 'hangboard_rep', 'free', 'exercise', 'circuit', 'group', 'emom'
CREATE TABLE "training_items" (
  "id"                   UUID        NOT NULL DEFAULT gen_random_uuid(),
  "training_id"          UUID        NOT NULL REFERENCES "trainings"("id") ON DELETE CASCADE,
  "parent_id"            UUID        REFERENCES "training_items"("id") ON DELETE CASCADE,
  "type"                 TEXT        NOT NULL,
  "position"             INTEGER     NOT NULL DEFAULT 0,
  -- Circuit, repeater and emom cycles (scalar). A hangboard_rep carries
  -- neither, for the same reason it carries no reps.
  "cycles"               INTEGER,
  "cycle_rest_seconds"   INTEGER,
  -- How often a round starts, for an 'emom' and nothing else. It is what makes
  -- the block every minute on the minute rather than a circuit: the work of a
  -- round is self paced and whatever is left of the interval is the rest, so
  -- the round after it starts on the clock however fast the round before it
  -- was. A circuit names the rest instead and lets the block drift.
  "interval_seconds"     INTEGER,
  -- Exercise and repeater reps (scalar). A hangboard_rep is a single hang and
  -- carries none: the repeater is the block that repeats a hang.
  "reps"                 INTEGER,
  -- Whether the rep count is left open, which is an AMRAP: the coach prescribes
  -- no number and the athlete does as many as they can, then records how many
  -- that was. The stored "reps" is then read by nothing.
  --
  -- Exercises only. A repeater lays its loads, grips and edges out one row per
  -- rep, so a rep count nothing knows until the block has been run leaves those
  -- arrays with no length to be written against.
  "reps_is_max"          BOOLEAN     NOT NULL DEFAULT FALSE,
  "duration"             INTEGER,
  "rest_seconds"         INTEGER,
  -- Exercise-specific (scalar)
  "exercise_id"          UUID        REFERENCES "exercises"("id") ON DELETE CASCADE,
  -- Repeater and hangboard_rep worktime (replaces hb_worktime_seconds)
  "worktime_seconds"     INTEGER,
  -- How the two hands are worked, for repeater and hangboard_rep items:
  --   'both'      both hands on the board at once, one hang per rep
  --   'alternate' one hand at a time, right then left within each rep
  --   'split'     one hand at a time, every rep of a set on one hand before the other
  --   'left'      left hand only
  --   'right'     right hand only
  -- Only 'both' puts two hands on the board simultaneously; the other modes hang
  -- a single hand at a time and can therefore be measured by the force sensor.
  "hand"                 TEXT,
  -- Layout of the configuration arrays below: 'uniform' (1 row), 'rep' (reps
  -- rows) or 'set' (cycles * reps rows, indexed set * reps + rep).
  "granularity"          TEXT,
  -- Free item text content
  "free_text"            TEXT,
  -- Optional coach comment shown to the athlete (e.g. "first rep in pronation")
  "comment"              TEXT,
  -- Why the exercise is in the program (e.g. "resi doigts", "explo jambes").
  -- Distinct from "comment" above and deliberately not an override key: a goal
  -- is what the block is for and holds across the weeks that retune it, while a
  -- comment says how to execute this instance. A week that really trains
  -- something else is a different block, not the same one relabelled.
  "goal"                 TEXT,
  -- The rule the athlete resolves while performing the block, in the coach's
  -- own prose ("to failure or 40s; past 40s add 5kg, short of it put your feet
  -- on the ground"). A prescription is often a condition rather than a number,
  -- and the numeric columns above can only carry the number. Free text rather
  -- than a condition/threshold/adjustment grammar: what the athlete does with
  -- it is read by a person, and what came out of it is recorded on
  -- "session_item_results" beside it. Nothing evaluates this column.
  --
  -- Distinct from "comment", which says how to execute the movement, and from
  -- "goal", which says what the block is for. Not an override key either
  -- (contract/override-keys.json), for the reason both of those are not: a week
  -- that resolves differently is retuning the numbers the protocol reads, not
  -- the protocol.
  "protocol"             TEXT,
  -- Configurable fields (JSONB arrays, see the layout note above the table).
  -- Each holds exactly one entry per configuration row.
  -- loads: [{value: float, unit: string}]. Right hand of a two-handed mode,
  -- and the only load for 'both', 'left' and 'right'.
  "loads"                JSONB,
  -- left_loads: [{value: float, unit: string}]. Left hand of 'alternate' and
  -- 'split' items; null otherwise.
  "left_loads"           JSONB,
  -- hand_positions: [[string]] indexed [hand][row], left hand first. Carries
  -- one array per hand, or a single array when both hands share the grip.
  "hand_positions"       JSONB,
  -- edge_sizes_mm: [int]
  "edge_sizes_mm"        JSONB,
  -- Whether load is maximum effort (as hard as possible) rather than a fixed value
  "load_is_max"          BOOLEAN     NOT NULL DEFAULT FALSE,
  -- Scalar fields expressed as a percentage of the athlete last assessment
  -- instead of a fixed number, keyed by field name ('duration', 'reps'):
  -- {"duration": {"assessment_id": "<uuid>", "percent": 75, "fallback": 60}}
  -- The id names a row in "assessment_definitions", whose unit must match the
  -- field: seconds a duration, repetitions reps, kilograms a load. Loads carry
  -- the same reference inline, as a 'percent_assessment' unit.
  "variable_targets"     JSONB,
  -- Group-specific
  "group_title"          TEXT,
  "created_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"           TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "training_items_hand_check"
    CHECK (hand IS NULL OR hand IN ('both', 'alternate', 'split', 'left', 'right')),
  CONSTRAINT "training_items_granularity_check"
    CHECK (granularity IS NULL OR granularity IN ('uniform', 'rep', 'set')),
  -- An interval is what an emom is, so the two stand or fall together: a block
  -- typed emom without one has no clock to start its rounds on, and any other
  -- type carrying one names a field nothing reading it would run.
  CONSTRAINT "training_items_emom_interval_check"
    CHECK ((type = 'emom') = (interval_seconds IS NOT NULL)),
  CONSTRAINT "training_items_interval_positive_check"
    CHECK (interval_seconds IS NULL OR interval_seconds > 0),
  CONSTRAINT "training_items_reps_is_max_check"
    CHECK (reps_is_max = FALSE OR type = 'exercise')
);

CREATE INDEX "training_items_training_id_idx" ON "training_items"("training_id");
CREATE INDEX "training_items_parent_id_idx" ON "training_items"("parent_id");

-- Stores what the athlete reported on one pass through a prescribed step. It
-- started as the two counts the prescription itself left open, an AMRAP with no
-- rep count until it has been run and an emom the athlete dropped out of, and
-- now carries what they did on any step at all: the set of pull ups that passed
-- through no sensor, the dip taken at a load nobody prescribed, and the line of
-- text that is the whole of the coaching loop ("28, hard on the shoulders",
-- "did it with a band, no dumbbell available").
--
-- One row per pass through an item, not per field: that is what lets one pass
-- carry a count, a load and a note together, and it is how the athlete fills it
-- in, a line per exercise. "occurrence" tells the passes apart when the item
-- sits inside a block that repeats, counting from 0 in the order the run played
-- them.
--
-- Every reported field is nullable, since a pass reports whichever of them the
-- athlete has something to say about, and a row reporting none of them is the
-- row that should never have been written.
CREATE TABLE "session_item_results" (
  "id"               UUID        NOT NULL DEFAULT gen_random_uuid(),
  "session_id"       UUID        NOT NULL REFERENCES "sessions"("id") ON DELETE CASCADE,
  "user_id"          UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  -- Which item of the prescription the row answers. Deliberately not a
  -- foreign key, and text rather than a uuid, for the reasons
  -- "rep_datas"."training_item_id" is neither: it points into the session
  -- frozen prescription snapshot, which names its own items, not into the still
  -- editable "training_items" row.
  "training_item_id" TEXT        NOT NULL,
  "occurrence"       INTEGER     NOT NULL DEFAULT 0,
  -- How many repetitions the pass actually did, which an AMRAP has no other
  -- record of.
  "reps"             INTEGER,
  -- How many rounds of a block the pass was carried through before the athlete
  -- dropped out, which an emom has no other record of.
  "cycles"           INTEGER,
  -- The load the pass was actually worked at, in kilograms, the unit the sensor
  -- measures and the one a prescribed load resolves to. Reported rather than
  -- measured, so it covers the dip and the weighted pull up no sensor sees.
  "load_kg"          REAL,
  -- How long the pass actually held, in seconds.
  "duration_seconds" INTEGER,
  -- What the athlete wrote about the pass, free text, the column the whole
  -- issue is about.
  "note"             TEXT,
  "updated_at"       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "session_item_results_reps_check" CHECK (reps IS NULL OR reps >= 0),
  CONSTRAINT "session_item_results_cycles_check" CHECK (cycles IS NULL OR cycles >= 0),
  CONSTRAINT "session_item_results_load_check" CHECK (load_kg IS NULL OR load_kg >= 0),
  CONSTRAINT "session_item_results_duration_check" CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
  CONSTRAINT "session_item_results_note_check" CHECK (note IS NULL OR (note <> '' AND char_length(note) <= 2000)),
  CONSTRAINT "session_item_results_reported_check" CHECK (
    reps IS NOT NULL OR cycles IS NOT NULL OR load_kg IS NOT NULL
    OR duration_seconds IS NOT NULL OR note IS NOT NULL
  ),
  CONSTRAINT "session_item_results_occurrence_check" CHECK (occurrence >= 0),
  CONSTRAINT "session_item_results_item_check" CHECK (
    training_item_id <> '' AND char_length(training_item_id) <= 200
  ),
  CONSTRAINT "session_item_results_unique" UNIQUE ("session_id", "training_item_id", "occurrence")
);

-- Stores one-time enrollment invitation tokens generated by coaches.
CREATE TABLE "enrollment_tokens" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"   UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "token"      TEXT        NOT NULL,
  "expires_at" TIMESTAMPTZ NOT NULL,
  "used_at"    TIMESTAMPTZ,
  "used_by"    UUID        REFERENCES "users"("id") ON DELETE SET NULL,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "enrollment_tokens_token_key" ON "enrollment_tokens" ("token");
CREATE INDEX "enrollment_tokens_coach_id_idx" ON "enrollment_tokens" ("coach_id");

-- Stores active coach-client relationships. A user may only be enrolled with one coach at a time.
CREATE TABLE "coach_enrollments" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "user_id"     UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "enrolled_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "coach_enrollments_user_id_key" ON "coach_enrollments" ("user_id");
CREATE INDEX "coach_enrollments_coach_id_idx" ON "coach_enrollments" ("coach_id");

-- Stores training programs created by coaches for specific enrolled users.
CREATE TABLE "coach_programs" (
  "id"              UUID        NOT NULL DEFAULT gen_random_uuid(),
  "coach_id"        UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "user_id"         UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "name"            TEXT        NOT NULL,
  "objective"       TEXT,
  "start_date"      DATE        NOT NULL,
  "duration_weeks"  INTEGER,
  "created_at"      TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"      TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_programs_coach_id_idx" ON "coach_programs"("coach_id");
CREATE INDEX "coach_programs_user_id_idx"  ON "coach_programs"("user_id");

-- Represents a specific week in a training program.
CREATE TABLE "coach_program_weeks" (
  "id"          UUID        NOT NULL DEFAULT gen_random_uuid(),
  "program_id"  UUID        NOT NULL REFERENCES "coach_programs"("id") ON DELETE CASCADE,
  "week_number" INTEGER     NOT NULL,
  -- Short label for the training phase this week belongs to (e.g. "capacity",
  -- "deload", "tests"). Reused across the weeks of one block, which is how a
  -- coach scans the arc of a program. Distinct from "notes" below: the name is
  -- what the week is, the notes are a message about this particular week.
  "name"        TEXT,
  "notes"       TEXT,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "coach_program_weeks_number_check" CHECK (week_number > 0),
  UNIQUE ("program_id", "week_number"),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_program_weeks_program_id_idx" ON "coach_program_weeks"("program_id");

-- A training session assigned to a specific week.
-- day_of_week 0=Mon...6=Sun; NULL means unscheduled (times_per_week required).
CREATE TABLE "coach_program_week_sessions" (
  "id"             UUID        NOT NULL DEFAULT gen_random_uuid(),
  "week_id"        UUID        NOT NULL REFERENCES "coach_program_weeks"("id") ON DELETE CASCADE,
  "training_id"    UUID        NOT NULL REFERENCES "trainings"("id") ON DELETE RESTRICT,
  "day_of_week"    INTEGER,
  "times_per_week" INTEGER,
  "is_everyday"    BOOLEAN     NOT NULL DEFAULT FALSE,
  "position"       INTEGER     NOT NULL DEFAULT 0,
  "notes"          TEXT,
  "created_at"     TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"     TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "cpws_mode_check" CHECK (
    (day_of_week IS NOT NULL AND times_per_week IS NULL AND is_everyday = FALSE)
    OR (day_of_week IS NULL AND times_per_week IS NOT NULL AND is_everyday = FALSE)
    OR (day_of_week IS NULL AND times_per_week IS NULL AND is_everyday = TRUE)
  ),
  CONSTRAINT "cpws_day_range_check"
    CHECK (day_of_week IS NULL OR (day_of_week >= 0 AND day_of_week <= 6)),
  CONSTRAINT "cpws_times_check"
    CHECK (times_per_week IS NULL OR times_per_week > 0),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_program_week_sessions_week_id_idx" ON "coach_program_week_sessions"("week_id");

-- Sparse per-item overrides for a session (e.g. different loads for a specific week).
-- overrides JSONB: only the fields being changed from the training template.
CREATE TABLE "coach_program_session_overrides" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "session_id" UUID        NOT NULL REFERENCES "coach_program_week_sessions"("id") ON DELETE CASCADE,
  "item_id"    UUID        NOT NULL REFERENCES "training_items"("id") ON DELETE CASCADE,
  "overrides"  JSONB       NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE ("session_id", "item_id"),
  PRIMARY KEY ("id")
);

CREATE INDEX "coach_program_session_overrides_session_id_idx" ON "coach_program_session_overrides"("session_id");

-- A coachee saying "here is my week", so a coach builds the program around the
-- week the athlete actually has. week_start is the Monday of that week.
--
-- The row is the declaration itself. What the athlete plans lives in
-- "coachee_day_activities" hanging off it, and a week where they plan nothing
-- at all is still a declared week. Inferring the declaration from the presence
-- of activity rows would make an empty week indistinguishable from a week never
-- answered, and the reminder would keep nudging an athlete who already answered.
CREATE TABLE "coachee_week_declarations" (
  "id"         UUID        NOT NULL DEFAULT gen_random_uuid(),
  "user_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "week_start" DATE        NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "cwd_week_start_monday_check"
    CHECK (EXTRACT(ISODOW FROM week_start) = 1),
  UNIQUE ("user_id", "week_start"),
  PRIMARY KEY ("id")
);

-- One thing the athlete plans to do on one day of a declared week, day_of_week
-- 0=Mon...6=Sun. A day holds as many as the athlete cares to enter, ordered by
-- "position" so the list reads back the way it was written. Everything but the
-- label is optional: "when" and "where" are free text, and the morning against
-- afternoon distinction lives in "when_text" rather than in the schema.
--
-- The week is written whole, so a save deletes every activity of the
-- declaration and re-inserts the ones sent. That is why there is no updated_at
-- here: the week is dated by its declaration.
CREATE TABLE "coachee_day_activities" (
  "id"               UUID        NOT NULL DEFAULT gen_random_uuid(),
  "declaration_id"   UUID        NOT NULL REFERENCES "coachee_week_declarations"("id") ON DELETE CASCADE,
  "day_of_week"      INTEGER     NOT NULL,
  "position"         INTEGER     NOT NULL,
  "label"            TEXT        NOT NULL,
  "duration_minutes" INTEGER,
  "when_text"        TEXT,
  "where_text"       TEXT,
  "created_at"       TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "cda_day_range_check" CHECK (day_of_week >= 0 AND day_of_week <= 6),
  CONSTRAINT "cda_position_check" CHECK (position >= 0),
  CONSTRAINT "cda_label_not_blank_check" CHECK (btrim(label) <> ''),
  CONSTRAINT "cda_duration_check"
    CHECK (duration_minutes IS NULL OR duration_minutes > 0),
  UNIQUE ("declaration_id", "day_of_week", "position"),
  PRIMARY KEY ("id")
);

-- One reminder per coach, applying to every coachee they train. The hour is a
-- wall clock time delivered by the athlete app in the athlete's own timezone,
-- which is why it is stored as plain integers rather than a TIME or TIMESTAMPTZ.
CREATE TABLE "coach_availability_reminders" (
  "coach_id"    UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "enabled"     BOOLEAN     NOT NULL DEFAULT FALSE,
  "day_of_week" INTEGER     NOT NULL,
  "hour"        INTEGER     NOT NULL,
  "minute"      INTEGER     NOT NULL DEFAULT 0,
  "created_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"  TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "car_day_range_check" CHECK (day_of_week >= 0 AND day_of_week <= 6),
  CONSTRAINT "car_hour_range_check" CHECK (hour >= 0 AND hour <= 23),
  CONSTRAINT "car_minute_range_check" CHECK (minute >= 0 AND minute <= 59),
  PRIMARY KEY ("coach_id")
);

-- When in the week a coach wants the programs whose next week holds no session
-- to show up in their TODO. Shaped like "coach_availability_reminders" and read
-- the same way, a weekday plus a wall clock time, except the clock is the
-- coach's own: the portal sends its UTC offset and the moment is judged against
-- the coach's local week, not the server's. A coach with no row is read at the
-- defaults below rather than told nothing was configured.
CREATE TABLE "coach_todo_settings" (
  "coach_id"               UUID        NOT NULL REFERENCES "users"("id") ON DELETE CASCADE,
  "empty_week_day_of_week" INTEGER     NOT NULL DEFAULT 4,
  "empty_week_hour"        INTEGER     NOT NULL DEFAULT 21,
  "empty_week_minute"      INTEGER     NOT NULL DEFAULT 0,
  "created_at"             TIMESTAMPTZ NOT NULL DEFAULT now(),
  "updated_at"             TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT "cts_day_range_check"
    CHECK (empty_week_day_of_week >= 0 AND empty_week_day_of_week <= 6),
  CONSTRAINT "cts_hour_range_check"
    CHECK (empty_week_hour >= 0 AND empty_week_hour <= 23),
  CONSTRAINT "cts_minute_range_check"
    CHECK (empty_week_minute >= 0 AND empty_week_minute <= 59),
  PRIMARY KEY ("coach_id")
);

-- Declared here rather than inline on "sessions" because both targets are
-- created further down this file.
ALTER TABLE "sessions"
  ADD CONSTRAINT "sessions_training_id_fkey"
  FOREIGN KEY ("training_id") REFERENCES "trainings"("id") ON DELETE SET NULL;

ALTER TABLE "sessions"
  ADD CONSTRAINT "sessions_program_session_id_fkey"
  FOREIGN KEY ("program_session_id") REFERENCES "coach_program_week_sessions"("id") ON DELETE SET NULL;

-- RESTRICT: deleting a definition must never silently erase the history measured
-- against it. The handler refuses the delete with a count instead.
ALTER TABLE "assessments"
  ADD CONSTRAINT "assessments_assessment_id_fkey"
  FOREIGN KEY ("assessment_id") REFERENCES "assessment_definitions"("id") ON DELETE RESTRICT;

-- Indexes for foreign keys to improve query performance
CREATE INDEX "sessions_user_id_idx" ON "sessions"("user_id");
CREATE INDEX "sessions_training_id_idx" ON "sessions"("training_id");
CREATE INDEX "sessions_program_session_id_idx" ON "sessions"("program_session_id");
CREATE INDEX "assessments_session_id_idx" ON "assessments"("session_id");
CREATE INDEX "assessments_user_id_idx" ON "assessments"("user_id");
CREATE INDEX "assessments_assessment_id_idx" ON "assessments"("assessment_id");
CREATE INDEX "rep_datas_session_id_idx" ON "rep_datas"("session_id");
CREATE INDEX "session_item_results_session_id_idx" ON "session_item_results"("session_id");
CREATE INDEX "session_item_results_user_id_idx" ON "session_item_results"("user_id");
CREATE INDEX "rep_datas_user_id_idx" ON "rep_datas"("user_id");
CREATE INDEX "pinned_builtin_trainings_user_id_idx" ON "pinned_builtin_trainings"("user_id");
CREATE INDEX "sensor_configs_user_id_idx" ON "sensor_configs"("user_id");
CREATE INDEX "builtin_training_weights_user_id_idx" ON "builtin_training_weights"("user_id");
-- Every read of the series is one athlete's, newest first, which is also how the
-- latest value is found when a prescription is frozen.
CREATE INDEX "user_bodyweights_user_id_measured_at_idx"
  ON "user_bodyweights"("user_id", "measured_at" DESC);

