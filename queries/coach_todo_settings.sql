-- name: GetCoachTodoSettings :one
SELECT * FROM coach_todo_settings
WHERE coach_id = @coach_id;

-- name: UpsertCoachTodoSettings :one
INSERT INTO coach_todo_settings (
  coach_id, empty_week_day_of_week, empty_week_hour, empty_week_minute
)
VALUES (@coach_id, @empty_week_day_of_week, @empty_week_hour, @empty_week_minute)
ON CONFLICT (coach_id) DO UPDATE
  SET empty_week_day_of_week = EXCLUDED.empty_week_day_of_week,
      empty_week_hour        = EXCLUDED.empty_week_hour,
      empty_week_minute      = EXCLUDED.empty_week_minute,
      updated_at             = now()
RETURNING *;
