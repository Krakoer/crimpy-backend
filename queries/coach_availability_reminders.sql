-- name: UpsertCoachAvailabilityReminder :one
INSERT INTO coach_availability_reminders (
  coach_id, enabled, day_of_week, hour, minute
)
VALUES (@coach_id, @enabled, @day_of_week, @hour, @minute)
ON CONFLICT (coach_id) DO UPDATE
  SET enabled     = EXCLUDED.enabled,
      day_of_week = EXCLUDED.day_of_week,
      hour        = EXCLUDED.hour,
      minute      = EXCLUDED.minute,
      updated_at  = now()
RETURNING *;

-- name: GetCoachAvailabilityReminder :one
SELECT * FROM coach_availability_reminders
WHERE coach_id = @coach_id;

-- name: GetAvailabilityReminderForUser :one
SELECT r.* FROM coach_availability_reminders r
JOIN coach_enrollments e ON e.coach_id = r.coach_id
WHERE e.user_id = @user_id;
