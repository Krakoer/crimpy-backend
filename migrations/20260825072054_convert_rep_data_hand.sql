-- A rep records which hand pulled it, and a two handed hang is a third state a
-- boolean cannot hold: it was stored as false and read back as the left hand.
-- Replace the boolean with the same textual hand the prescription items use.
--
-- The reps already stored carry no trace of the state that was lost, so a two
-- handed hang recorded before this migration stays 'left'. Nothing runs in
-- production yet, so those rows are not worth reconstructing from the session
-- prescription snapshots.

ALTER TABLE "rep_datas" ADD COLUMN "hand" TEXT;

UPDATE "rep_datas" SET "hand" = CASE WHEN "right_hand" THEN 'right' ELSE 'left' END;

ALTER TABLE "rep_datas"
  ALTER COLUMN "hand" SET NOT NULL,
  DROP COLUMN "right_hand",
  ADD CONSTRAINT "rep_datas_hand_check" CHECK (hand = ANY (ARRAY['left'::text, 'right'::text, 'both'::text]));
