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

-- name: CreateCoachProgramSlot :one
INSERT INTO coach_program_slots (program_id, training_id, day_of_week, times_per_week, position)
VALUES (@program_id, @training_id, @day_of_week, @times_per_week, @position)
RETURNING *;

-- name: GetCoachProgramSlots :many
SELECT
  cps.id,
  cps.program_id,
  cps.training_id,
  cps.day_of_week,
  cps.times_per_week,
  cps.position,
  cps.created_at,
  ct.title          AS training_title,
  ct.training_type  AS training_type
FROM coach_program_slots cps
JOIN coach_trainings ct ON ct.id = cps.training_id
WHERE cps.program_id = @program_id
ORDER BY cps.position;

-- name: DeleteCoachProgramSlots :exec
DELETE FROM coach_program_slots WHERE program_id = @program_id;
