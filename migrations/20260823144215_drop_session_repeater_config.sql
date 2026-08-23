-- The six repeater columns described the set shape of a session, but nothing
-- outside the app's dummy data generator ever wrote them: the screen that saves
-- a finished run built its session without them, so they are null on every
-- session an athlete actually played. Both breakdowns gated on them were
-- therefore dead, and the reps now name the prescription item they were played
-- from, which describes the same shape and survives a training holding several
-- blocks. No real data is lost by dropping them.

-- Modify "sessions" table
ALTER TABLE "sessions" DROP COLUMN "repeater_sets", DROP COLUMN "repeater_reps", DROP COLUMN "repeater_work_time", DROP COLUMN "repeater_rest_time", DROP COLUMN "repeater_set_rest", DROP COLUMN "repeater_split_hand";
