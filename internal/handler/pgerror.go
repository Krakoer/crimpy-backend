package handler

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation reports whether err is the database refusing a duplicate row,
// along with the constraint that rejected it, so callers can tell which one it was.
func uniqueViolation(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// foreignKeyViolation reports whether err is the database refusing to delete a
// row another table still references, along with the constraint that rejected it.
func foreignKeyViolation(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return pgErr.ConstraintName, true
	}
	return "", false
}
