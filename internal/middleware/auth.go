package middleware

import (
	"crimpy/backend/internal/utils"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// AuthMiddleware validates JWT tokens and extracts user information
func AuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			slog.Warn("auth rejected", "reason", "missing authorization header", "ip", c.IP(), "path", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing authorization header",
			})
		}

		// Extract token from "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			slog.Warn("auth rejected", "reason", "invalid authorization header format", "ip", c.IP(), "path", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid authorization header format",
			})
		}

		token := parts[1]
		claims, err := utils.ValidateJWT(token)
		if err != nil {
			slog.Warn("auth rejected", "reason", "invalid or expired token", "error", err.Error(), "ip", c.IP(), "path", c.Path())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid or expired token",
			})
		}

		// Store user information in context
		c.Locals("user_id", claims.UserID)
		c.Locals("email", claims.Email)
		c.Locals("is_admin", claims.IsAdmin)
		c.Locals("is_coach", claims.IsCoach)

		return c.Next()
	}
}

// GetUserID retrieves the user ID from the request context
func GetUserID(c fiber.Ctx) string {
	userID, _ := c.Locals("user_id").(string)
	return userID
}

// IsAdmin checks if the current user is an admin
func IsAdmin(c fiber.Ctx) bool {
	isAdmin, _ := c.Locals("is_admin").(bool)
	return isAdmin
}

// IsCoach checks if the current user is a coach
func IsCoach(c fiber.Ctx) bool {
	isCoach, _ := c.Locals("is_coach").(bool)
	return isCoach
}
