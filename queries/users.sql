-- queries/users.sql
-- name: CreateUser :one
INSERT INTO users (email, firstname, lastname, password) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: CreateAdminUser :one
INSERT INTO users (email, firstname, lastname, password, is_admin) VALUES ($1, $2, $3, $4, true) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;
