-- Normalise hangboard items onto the declared-granularity format: every
-- configuration array holds one entry per row, the row count comes from the
-- granularity column, and the two hands are never interleaved.

-- Helpers, dropped at the end of the migration. They keep the row count and the
-- array resizing defined once instead of once per statement, and they mirror
-- hangboardRowCount in internal/handler/training_items.go, which floors a
-- missing or non-positive count at one rather than only replacing NULL.
CREATE FUNCTION hangboard_row_count(granularity TEXT, cycles INTEGER, reps INTEGER)
RETURNS INTEGER LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE granularity
    WHEN 'set' THEN GREATEST(COALESCE(cycles, 1), 1) * GREATEST(COALESCE(reps, 1), 1)
    WHEN 'rep' THEN GREATEST(COALESCE(reps, 1), 1)
    ELSE 1
  END
$$;

-- Length of a configuration array, and zero for anything that is not one. The
-- API accepted non-array values until recently, so no statement here may assume
-- the column holds an array.
CREATE FUNCTION hangboard_len(source JSONB)
RETURNS INTEGER LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN source IS NULL OR jsonb_typeof(source) <> 'array' THEN 0
    ELSE jsonb_array_length(source)
  END
$$;

-- Resize an array to exactly the declared row count, trimming the leftovers of
-- an earlier edit and padding a short array by repeating its last entry. A
-- value that is not an array carries no configuration and becomes NULL.
CREATE FUNCTION hangboard_resize(source JSONB, wanted INTEGER)
RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN hangboard_len(source) = 0 THEN NULL
    WHEN hangboard_len(source) = wanted THEN source
    ELSE (
      SELECT jsonb_agg(
        COALESCE(source -> (position - 1), source -> (hangboard_len(source) - 1))
        ORDER BY position
      )
      FROM generate_series(1, wanted) AS position
    )
  END
$$;

-- 1. Declare the layout each item was already written with. Clients used to
-- derive it from edge_sizes_mm, falling back to the load count for app-created
-- items, which carry no edge sizes at all.
UPDATE training_items
SET granularity = CASE
  WHEN hangboard_len(edge_sizes_mm) > 1 THEN
    CASE
      WHEN GREATEST(COALESCE(cycles, 1), 1) > 1
       AND hangboard_len(edge_sizes_mm) = GREATEST(COALESCE(cycles, 1), 1) * GREATEST(COALESCE(reps, 1), 1)
      THEN 'set'
      ELSE 'rep'
    END
  WHEN hangboard_len(edge_sizes_mm) = 1 THEN 'uniform'
  WHEN hangboard_len(loads) > 1 THEN 'rep'
  ELSE 'uniform'
END
WHERE type IN ('repeater', 'hangboard_rep');

-- 2. A repeater written by the mobile app ran its 'both' mode as one hand at a
-- time, right then left within every rep. That is the 'alternate' mode now.
-- Only the app omits edge_sizes_mm, and a hangboard_rep already meant a genuine
-- two-handed hang, so neither is reclassified.
UPDATE training_items
SET hand = 'alternate'
WHERE type = 'repeater'
  AND hand = 'both'
  AND edge_sizes_mm IS NULL;

-- 3. Split the coach portal's interleaved split-hand loads into the per-hand
-- arrays every client now reads. The portal wrote the left hand at even indices
-- and the right hand at odd ones, and left the left_loads column null.
UPDATE training_items
SET
  loads = (
    SELECT jsonb_agg(entry ORDER BY ord)
    FROM jsonb_array_elements(loads) WITH ORDINALITY AS interleaved(entry, ord)
    WHERE ord % 2 = 0
  ),
  left_loads = (
    SELECT jsonb_agg(entry ORDER BY ord)
    FROM jsonb_array_elements(loads) WITH ORDINALITY AS interleaved(entry, ord)
    WHERE ord % 2 = 1
  )
WHERE hand = 'split'
  AND left_loads IS NULL
  AND edge_sizes_mm IS NOT NULL
  AND hangboard_len(loads) = 2 * hangboard_row_count(granularity, cycles, reps);

-- 4. Give hand_positions its one-array-per-hand shape everywhere, and resize
-- each hand to the row count. The app wrote a flat array of grips, and the
-- portal wrote one entry per rep even for uniform items, which carry one row.
DO $$
DECLARE
  item RECORD;
  rows_wanted INTEGER;
  by_hand JSONB;
  normalised JSONB;
  hand_slots JSONB;
BEGIN
  FOR item IN
    SELECT id, hand_positions, granularity, cycles, reps
    FROM training_items
    WHERE type IN ('repeater', 'hangboard_rep')
      AND hangboard_len(hand_positions) > 0
  LOOP
    rows_wanted := hangboard_row_count(item.granularity, item.cycles, item.reps);

    IF jsonb_typeof(item.hand_positions -> 0) = 'array' THEN
      by_hand := item.hand_positions;
    ELSE
      by_hand := jsonb_build_array(item.hand_positions);
    END IF;

    normalised := '[]'::jsonb;
    FOR hand_slots IN SELECT value FROM jsonb_array_elements(by_hand) LOOP
      normalised := normalised || jsonb_build_array(
        COALESCE(hangboard_resize(hand_slots, rows_wanted), '[]'::jsonb)
      );
    END LOOP;

    UPDATE training_items SET hand_positions = normalised WHERE id = item.id;
  END LOOP;
END $$;

-- 5. Resize the remaining arrays to the row count their granularity declares,
-- so a uniform item never carries the per-rep leftovers of an earlier edit and
-- a short array never leaves a row without a value.
UPDATE training_items
SET
  loads = hangboard_resize(loads, hangboard_row_count(granularity, cycles, reps)),
  left_loads = hangboard_resize(left_loads, hangboard_row_count(granularity, cycles, reps)),
  edge_sizes_mm = hangboard_resize(edge_sizes_mm, hangboard_row_count(granularity, cycles, reps))
WHERE type IN ('repeater', 'hangboard_rep');

-- 6. Drop the per-hand configuration the hand mode says cannot exist, so the
-- stored rows satisfy the same rule the handler enforces on write. A mode that
-- hangs both hands together keeps the first grip array, the one every client
-- reads when a single array is stored.
UPDATE training_items
SET
  left_loads = NULL,
  hand_positions = CASE
    WHEN hangboard_len(hand_positions) > 1 THEN jsonb_build_array(hand_positions -> 0)
    ELSE hand_positions
  END
WHERE type IN ('repeater', 'hangboard_rep')
  AND COALESCE(hand, 'both') NOT IN ('alternate', 'split')
  AND (left_loads IS NOT NULL OR hangboard_len(hand_positions) > 1);

-- 7. Fail the migration rather than leave a row the API would reject on the
-- next save, or a split item still carrying its two hands in one array.
DO $$
DECLARE
  offending INTEGER;
BEGIN
  SELECT count(*) INTO offending
  FROM training_items
  WHERE type IN ('repeater', 'hangboard_rep')
    AND (
      (loads IS NOT NULL AND hangboard_len(loads) <> hangboard_row_count(granularity, cycles, reps))
      OR (left_loads IS NOT NULL AND hangboard_len(left_loads) <> hangboard_row_count(granularity, cycles, reps))
      OR (edge_sizes_mm IS NOT NULL AND hangboard_len(edge_sizes_mm) <> hangboard_row_count(granularity, cycles, reps))
      OR (hand_positions IS NOT NULL AND hangboard_len(hand_positions) > CASE
        WHEN COALESCE(hand, 'both') IN ('alternate', 'split') THEN 2 ELSE 1 END)
      OR (hand = 'split' AND left_loads IS NULL AND loads IS NOT NULL)
    );

  IF offending > 0 THEN
    RAISE EXCEPTION 'hangboard normalisation left % item(s) disagreeing with their declared layout', offending;
  END IF;
END $$;

DROP FUNCTION hangboard_resize(JSONB, INTEGER);
DROP FUNCTION hangboard_len(JSONB);
DROP FUNCTION hangboard_row_count(TEXT, INTEGER, INTEGER);
