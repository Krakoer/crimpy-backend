-- name: CreateCoachProgram :one
INSERT INTO coach_programs (coach_id, user_id, name, objective, start_date, duration_weeks)
VALUES (@coach_id, @user_id, @name, @objective, @start_date, @duration_weeks)
RETURNING *;

-- name: GetCoachProgram :one
SELECT * FROM coach_programs WHERE id = @id;

-- name: GetCoachProgramsForClient :many
SELECT * FROM coach_programs
WHERE coach_id = @coach_id AND user_id = @user_id
ORDER BY start_date DESC;

-- name: GetMyPrograms :many
SELECT * FROM coach_programs WHERE user_id = @user_id ORDER BY start_date DESC;

-- name: UpdateCoachProgram :one
UPDATE coach_programs
SET name = @name, objective = @objective, start_date = @start_date,
    duration_weeks = @duration_weeks, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteCoachProgram :exec
DELETE FROM coach_programs WHERE id = @id;

