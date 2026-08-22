-- The cycle count and cycle rest describe a repeated hang, so a hangboard_rep
-- carries neither and the API now refuses both, the way it already refuses reps.
-- No row in the dev database holds one, but clear them anyway so a database
-- seeded before the refusal does not hold trainings the portal cannot save.

UPDATE training_items
SET cycles = NULL,
    cycle_rest_seconds = NULL
WHERE type = 'hangboard_rep'
  AND (cycles IS NOT NULL OR cycle_rest_seconds IS NOT NULL);

-- A 'set' item sizes its configuration arrays from its cycle count, so clearing
-- the count could leave one disagreeing with its declared layout. Fail the
-- migration rather than leave a row the API would reject on the next save.
DO $$
DECLARE
  offending INTEGER;
BEGIN
  SELECT count(*) INTO offending
  FROM training_items
  WHERE type = 'hangboard_rep'
    AND granularity IN ('rep', 'set')
    AND (
      jsonb_array_length(COALESCE(loads, '[]'::jsonb)) > 1
      OR jsonb_array_length(COALESCE(left_loads, '[]'::jsonb)) > 1
      OR jsonb_array_length(COALESCE(edge_sizes_mm, '[]'::jsonb)) > 1
    );

  IF offending > 0 THEN
    RAISE EXCEPTION 'clearing hangboard_rep cycles left % item(s) with a multi-row layout', offending;
  END IF;
END $$;
