-- A hangboard_rep is a single hang, and the API now refuses a rep count on one.
-- Clear the counts already stored so the rows the portal sends back untouched on
-- every save do not fail that check, and so a read no longer reports a rep count
-- no client runs.

UPDATE training_items
SET reps = NULL
WHERE type = 'hangboard_rep'
  AND reps IS NOT NULL;

-- The configuration arrays of a 'rep' or 'set' item are sized from its rep
-- count, so clearing the count could leave one disagreeing with its declared
-- layout. Every stored hangboard_rep is uniform, which carries a single row
-- whatever the count, but fail the migration rather than trust that silently.
DO $$
DECLARE
  offending INTEGER;
BEGIN
  SELECT count(*) INTO offending
  FROM training_items
  WHERE type = 'hangboard_rep'
    AND granularity = 'rep'
    AND (
      jsonb_array_length(COALESCE(loads, '[]'::jsonb)) > 1
      OR jsonb_array_length(COALESCE(left_loads, '[]'::jsonb)) > 1
      OR jsonb_array_length(COALESCE(edge_sizes_mm, '[]'::jsonb)) > 1
    );

  IF offending > 0 THEN
    RAISE EXCEPTION 'clearing hangboard_rep reps left % item(s) with a per-rep layout of more than one row', offending;
  END IF;
END $$;
