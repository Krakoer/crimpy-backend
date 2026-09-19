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
SELECT * FROM coachee_week_declarations
WHERE user_id = @user_id
ORDER BY week_start;

-- name: ListCoacheeDayActivities :many
-- Every activity the athlete holds, across every week they declared, ordered so
-- it zips straight onto the declarations above.
SELECT a.* FROM coachee_day_activities a
JOIN coachee_week_declarations d ON d.id = a.declaration_id
WHERE d.user_id = @user_id
ORDER BY d.week_start, a.day_of_week, a.position;

-- name: GetCoacheeWeekDeclaration :one
SELECT * FROM coachee_week_declarations
WHERE user_id = @user_id AND week_start = @week_start;

-- name: ListCoacheeWeekDayActivities :many
SELECT * FROM coachee_day_activities
WHERE declaration_id = @declaration_id
ORDER BY day_of_week, position;
