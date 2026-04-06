package middleware

import (
	"crimpy/backend/internal/utils"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// AuthMiddleware validates JWT tokens and extracts user information
func AuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing authorization header",
			})
		}

		// Extract token from "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid authorization header format",
			})
		}

		token := parts[1]
		claims, err := utils.ValidateJWT(token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid or expired token",
			})
		}

		// Store user information in context
		c.Locals("user_id", claims.UserID)
		c.Locals("email", claims.Email)
		c.Locals("is_admin", claims.IsAdmin)

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
