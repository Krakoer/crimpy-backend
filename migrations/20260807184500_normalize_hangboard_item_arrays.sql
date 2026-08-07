-- Normalise hangboard items onto the declared-granularity format: every
-- configuration array holds one entry per row, the row count comes from the
-- granularity column, and the two hands are never interleaved.

-- 1. Declare the layout each item was already written with. Clients used to
-- derive it from edge_sizes_mm, falling back to the load count for app-created
-- items, which carry no edge sizes at all.
UPDATE training_items
SET granularity = CASE
  WHEN COALESCE(jsonb_array_length(edge_sizes_mm), 0) > 1 THEN
    CASE
      WHEN COALESCE(cycles, 1) > 1
       AND jsonb_array_length(edge_sizes_mm) = COALESCE(cycles, 1) * COALESCE(reps, 1)
      THEN 'set'
      ELSE 'rep'
    END
  WHEN COALESCE(jsonb_array_length(edge_sizes_mm), 0) = 1 THEN 'uniform'
  WHEN COALESCE(jsonb_array_length(loads), 0) > 1 THEN 'rep'
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
  AND loads IS NOT NULL
  AND jsonb_array_length(loads) = 2 * CASE granularity
    WHEN 'uniform' THEN 1
    WHEN 'set' THEN COALESCE(cycles, 1) * COALESCE(reps, 1)
    ELSE COALESCE(reps, 1)
  END;

-- 4. Give hand_positions its one-array-per-hand shape everywhere, and trim each
-- hand to the row count. The app wrote a flat array of grips, and the portal
-- wrote one entry per rep even for uniform items, which carry a single row.
DO $$
DECLARE
  item RECORD;
  rows_wanted INTEGER;
  by_hand JSONB;
  trimmed JSONB;
  hand_slots JSONB;
BEGIN
  FOR item IN
    SELECT id, hand_positions, granularity, cycles, reps
    FROM training_items
    WHERE type IN ('repeater', 'hangboard_rep')
      AND hand_positions IS NOT NULL
      AND jsonb_typeof(hand_positions) = 'array'
      AND jsonb_array_length(hand_positions) > 0
  LOOP
    rows_wanted := CASE item.granularity
      WHEN 'uniform' THEN 1
      WHEN 'set' THEN COALESCE(item.cycles, 1) * COALESCE(item.reps, 1)
      ELSE COALESCE(item.reps, 1)
    END;

    IF jsonb_typeof(item.hand_positions -> 0) = 'array' THEN
      by_hand := item.hand_positions;
    ELSE
      by_hand := jsonb_build_array(item.hand_positions);
    END IF;

    trimmed := '[]'::jsonb;
    FOR hand_slots IN SELECT value FROM jsonb_array_elements(by_hand) LOOP
      trimmed := trimmed || jsonb_build_array((
        SELECT COALESCE(jsonb_agg(slot ORDER BY ord), '[]'::jsonb)
        FROM jsonb_array_elements(hand_slots) WITH ORDINALITY AS slots(slot, ord)
        WHERE ord <= rows_wanted
      ));
    END LOOP;

    UPDATE training_items SET hand_positions = trimmed WHERE id = item.id;
  END LOOP;
END $$;

-- 5. Trim the remaining arrays to the row count their granularity declares, so
-- a uniform item never carries the per-rep leftovers of an earlier edit.
UPDATE training_items
SET
  loads = (
    SELECT jsonb_agg(entry ORDER BY ord)
    FROM jsonb_array_elements(loads) WITH ORDINALITY AS rows_in(entry, ord)
    WHERE ord <= CASE granularity
      WHEN 'uniform' THEN 1
      WHEN 'set' THEN COALESCE(cycles, 1) * COALESCE(reps, 1)
      ELSE COALESCE(reps, 1)
    END
  )
WHERE type IN ('repeater', 'hangboard_rep')
  AND loads IS NOT NULL
  AND jsonb_array_length(loads) > CASE granularity
    WHEN 'uniform' THEN 1
    WHEN 'set' THEN COALESCE(cycles, 1) * COALESCE(reps, 1)
    ELSE COALESCE(reps, 1)
  END;

UPDATE training_items
SET
  left_loads = (
    SELECT jsonb_agg(entry ORDER BY ord)
    FROM jsonb_array_elements(left_loads) WITH ORDINALITY AS rows_in(entry, ord)
    WHERE ord <= CASE granularity
      WHEN 'uniform' THEN 1
      WHEN 'set' THEN COALESCE(cycles, 1) * COALESCE(reps, 1)
      ELSE COALESCE(reps, 1)
    END
  )
WHERE type IN ('repeater', 'hangboard_rep')
  AND left_loads IS NOT NULL
  AND jsonb_array_length(left_loads) > CASE granularity
    WHEN 'uniform' THEN 1
    WHEN 'set' THEN COALESCE(cycles, 1) * COALESCE(reps, 1)
    ELSE COALESCE(reps, 1)
  END;
