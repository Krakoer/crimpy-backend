-- name: CreateEnrollmentToken :one
INSERT INTO enrollment_tokens (coach_id, token, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetEnrollmentTokenByValue :one
SELECT
    et.id,
    et.coach_id,
    et.token,
    et.expires_at,
    et.used_at,
    et.used_by,
    u.firstname AS coach_firstname,
    u.lastname  AS coach_lastname,
    u.email     AS coach_email
FROM enrollment_tokens et
JOIN users u ON u.id = et.coach_id
WHERE et.token = $1;

-- name: UseEnrollmentToken :execresult
UPDATE enrollment_tokens
SET used_at = NOW(), used_by = $2
WHERE token = $1 AND used_at IS NULL;

-- name: GetCoachEnrollment :one
SELECT id, coach_id, user_id, enrolled_at
FROM coach_enrollments
WHERE coach_id = $1 AND user_id = $2;

-- name: CreateCoachEnrollment :one
INSERT INTO coach_enrollments (coach_id, user_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetEnrollmentByUser :one
SELECT
    ce.id,
    ce.coach_id,
    ce.user_id,
    ce.enrolled_at,
    u.firstname AS coach_firstname,
    u.lastname  AS coach_lastname,
    u.email     AS coach_email
FROM coach_enrollments ce
JOIN users u ON u.id = ce.coach_id
WHERE ce.user_id = $1;

-- name: GetEnrollmentsByCoach :many
SELECT
    ce.id,
    ce.coach_id,
    ce.user_id,
    ce.enrolled_at,
    u.firstname AS user_firstname,
    u.lastname  AS user_lastname,
    u.email     AS user_email
FROM coach_enrollments ce
JOIN users u ON u.id = ce.user_id
WHERE ce.coach_id = $1
ORDER BY ce.enrolled_at DESC;

-- name: DeleteCoachEnrollment :execresult
DELETE FROM coach_enrollments WHERE coach_id = $1 AND user_id = $2;

-- name: DeleteUserEnrollment :execresult
DELETE FROM coach_enrollments WHERE user_id = $1;
