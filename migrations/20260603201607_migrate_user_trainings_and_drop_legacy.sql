-- Migrate user trainings into the unified coach_trainings/coach_training_items tables,
-- then drop the legacy trainings, repeaters, and rep_templates tables.

-- 1. Migrate repeater trainings metadata (training.repeater_id IS NOT NULL)
INSERT INTO coach_trainings (id, user_id, title, training_type, is_favorite)
SELECT t.id, t.user_id, t.name, 'workout', t.is_favorite
FROM trainings t
WHERE t.repeater_id IS NOT NULL;

-- 2. Migrate repeater training items (using DO block for per-row array generation)
DO $$
DECLARE
  t_row RECORD;
  r_row RECORD;
  loads_json JSONB;
  left_loads_json JSONB;
  positions_json JSONB;
  grip_str TEXT;
BEGIN
  FOR t_row IN SELECT * FROM trainings WHERE repeater_id IS NOT NULL LOOP
    SELECT * INTO r_row FROM repeaters WHERE id = t_row.repeater_id;

    SELECT jsonb_agg(jsonb_build_object('value', COALESCE(r_row.target_weight_right, 0.0), 'unit', 'kg'))
    INTO loads_json FROM generate_series(1, r_row.reps);

    IF r_row.split_hand THEN
      SELECT jsonb_agg(jsonb_build_object('value', COALESCE(r_row.target_weight_left, 0.0), 'unit', 'kg'))
      INTO left_loads_json FROM generate_series(1, r_row.reps);
    ELSE
      left_loads_json := NULL;
    END IF;

    grip_str := CASE r_row.grip_position
      WHEN 0 THEN 'halfCrimp'
      WHEN 1 THEN 'threeFinger'
      WHEN 2 THEN 'fullCrimp'
      WHEN 3 THEN 'openHand'
      ELSE 'halfCrimp'
    END;
    SELECT jsonb_agg(grip_str::text)
    INTO positions_json FROM generate_series(1, r_row.reps);

    INSERT INTO coach_training_items (
      training_id, type, position,
      cycles, reps, worktime_seconds, rest_seconds, cycle_rest_seconds,
      hand, loads, left_loads, hand_positions
    ) VALUES (
      t_row.id, 'repeater', 0,
      r_row.sets, r_row.reps, r_row.worktime, r_row.resttime, r_row.set_rest,
      CASE WHEN r_row.split_hand THEN 'split' ELSE 'both' END,
      loads_json,
      left_loads_json,
      positions_json
    );
  END LOOP;
END $$;

-- 3. Migrate custom trainings metadata (training.repeater_id IS NULL)
INSERT INTO coach_trainings (id, user_id, title, training_type, is_favorite)
SELECT t.id, t.user_id, t.name, 'workout', t.is_favorite
FROM trainings t
WHERE t.repeater_id IS NULL;

-- 4. Migrate custom training items: each non-rest rep becomes a hangboard_rep item.
-- rest_seconds is taken from the immediately following rest rep (index + 1), defaulting to 0.
INSERT INTO coach_training_items (
  training_id, type, position,
  worktime_seconds, rest_seconds,
  hand, loads, hand_positions
)
SELECT
  work.training_id,
  'hangboard_rep',
  (ROW_NUMBER() OVER (PARTITION BY work.training_id ORDER BY work.index) - 1)::integer,
  work.duration,
  COALESCE(
    (SELECT rest.duration FROM rep_templates rest
     WHERE rest.training_id = work.training_id
       AND rest.is_rest = true
       AND rest.index = work.index + 1
     LIMIT 1),
    0
  ),
  CASE WHEN work.right_hand THEN 'right' ELSE 'left' END,
  jsonb_build_array(jsonb_build_object('value', work.target_weight, 'unit', 'kg')),
  jsonb_build_array(
    CASE work.grip_position
      WHEN 0 THEN 'halfCrimp'
      WHEN 1 THEN 'threeFinger'
      WHEN 2 THEN 'fullCrimp'
      WHEN 3 THEN 'openHand'
      ELSE 'halfCrimp'
    END
  )
FROM rep_templates work
WHERE work.is_rest = false
  AND EXISTS (
    SELECT 1 FROM trainings t
    WHERE t.id = work.training_id AND t.repeater_id IS NULL
  );

-- 5. Drop legacy tables (CASCADE handles FK dependencies)
DROP TABLE "rep_templates";
DROP TABLE "trainings";
DROP TABLE "repeaters";
