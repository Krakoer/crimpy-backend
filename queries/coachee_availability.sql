-- name: UpsertCoacheeWeekDeclaration :one
-- Declaring a week the athlete has already declared is a re-answer, not a new
-- one, so the row keeps its identity and only moves its updated_at. That is
-- what the coach feed dates the event by.
INSERT INTO coachee_week_declarations (user_id, week_start)
VALUES (@user_id, @week_start)
ON CONFLICT (user_id, week_start) DO UPDATE
  SET updated_at = now()
RETURNING *;

-- name: DeleteCoacheeWeekActivities :exec
DELETE FROM coachee_day_activities
WHERE declaration_id = @declaration_id;

-- name: InsertCoacheeDayActivity :batchexec
-- Batched rather than issued one at a time: a week may carry as many as
-- 7 x maxActivitiesPerDay activities, and a round trip each would hold the
-- write transaction open against the request deadline for no reason.
INSERT INTO coachee_day_activities (
  declaration_id, day_of_week, position, label, duration_minutes, when_text, where_text
)
VALUES (
  @declaration_id, @day_of_week, @position, @label, @duration_minutes, @when_text, @where_text
);

-- name: ListCoacheeWeekDeclarations :many
-- The window is optional on each end. A caller that sends neither gets every
-- week the athlete declared, which is what the clients read before the window
-- existed, so an older build keeps working unchanged.
SELECT * FROM coachee_week_declarations
WHERE user_id = @user_id
  AND week_start >= COALESCE(sqlc.narg('from_week')::date, '-infinity')
  AND week_start <= COALESCE(sqlc.narg('to_week')::date, 'infinity')
ORDER BY week_start;

-- name: ListCoacheeDayActivities :many
-- The activities of the weeks the window above keeps, ordered so it zips
-- straight onto the declarations. The two filters must be given the same
-- bounds, or a week would come back without what was planned in it.
SELECT a.* FROM coachee_day_activities a
JOIN coachee_week_declarations d ON d.id = a.declaration_id
WHERE d.user_id = @user_id
  AND d.week_start >= COALESCE(sqlc.narg('from_week')::date, '-infinity')
  AND d.week_start <= COALESCE(sqlc.narg('to_week')::date, 'infinity')
ORDER BY d.week_start, a.day_of_week, a.position;

-- name: ListCoacheeDeclaredWeekStarts :many
-- Every week the athlete ever declared, as dates alone and never windowed.
-- The app plans its declaration reminders off this: a nudge is dropped for a
-- week already answered, so a list missing one nudges the athlete about a week
-- they have already sent. It carries no activities, so answering it in full
-- costs one small row per declared week.
SELECT week_start FROM coachee_week_declarations
WHERE user_id = @user_id
ORDER BY week_start;

-- name: GetCoacheeWeekDeclaration :one
SELECT * FROM coachee_week_declarations
WHERE user_id = @user_id AND week_start = @week_start;

-- name: ListCoacheeWeekDayActivities :many
SELECT * FROM coachee_day_activities
WHERE declaration_id = @declaration_id
ORDER BY day_of_week, position;
