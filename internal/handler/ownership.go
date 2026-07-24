package handler

import (
	"context"
	"crimpy/backend/internal/middleware"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ownedResource describes how to load a resource by its :id path parameter and
// who owns it, so every ownership check behaves and reports identically.
type ownedResource[T any] struct {
	// label names the resource in error messages, e.g. "Sensor config".
	label string
	fetch func(context.Context, pgtype.UUID) (T, error)
	owner func(T) pgtype.UUID
	// allowAdmin lets admins act on resources they do not own.
	allowAdmin bool
}

// require loads the resource named by the :id parameter and verifies the caller
// owns it. When it returns false the error response is already written and the
// handler should return nil.
func (r ownedResource[T]) require(c fiber.Ctx) (T, pgtype.UUID, bool) {
	var zero T

	idStr := c.Params("id")
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid " + strings.ToLower(r.label) + " ID"})
		return zero, id, false
	}

	callerID := middleware.GetUserID(c)
	var callerUUID pgtype.UUID
	if err := callerUUID.Scan(callerID); err != nil {
		c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid user ID"})
		return zero, id, false
	}

	resource, err := r.fetch(c.Context(), id)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": r.label + " not found"})
			return zero, id, false
		}
		slog.Error("failed to retrieve resource", "resource", r.label, "id", idStr, "error", err)
		c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve " + strings.ToLower(r.label)})
		return zero, id, false
	}

	if r.owner(resource).Bytes != callerUUID.Bytes && !(r.allowAdmin && middleware.IsAdmin(c)) {
		slog.Warn("access denied", "resource", r.label, "user_id", callerID, "id", idStr)
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Access denied"})
		return zero, id, false
	}

	return resource, id, true
}
