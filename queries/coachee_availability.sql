-- name: UpsertCoacheeDayAvailability :one
INSERT INTO coachee_day_availabilities (
  user_id, week_start, day_of_week, is_available, duration_minutes, note
)
VALUES (
  @user_id, @week_start, @day_of_week, @is_available, @duration_minutes, @note
)
ON CONFLICT (user_id, week_start, day_of_week) DO UPDATE
  SET is_available     = EXCLUDED.is_available,
      duration_minutes = EXCLUDED.duration_minutes,
      note             = EXCLUDED.note,
      updated_at       = now()
RETURNING *;

-- name: GetCoacheeAvailability :many
SELECT * FROM coachee_day_availabilities
WHERE user_id = @user_id
ORDER BY week_start, day_of_week;

-- name: GetCoacheeWeekAvailability :many
SELECT * FROM coachee_day_availabilities
WHERE user_id = @user_id AND week_start = @week_start
ORDER BY day_of_week;
