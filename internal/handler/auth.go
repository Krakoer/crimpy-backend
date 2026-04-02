package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/utils"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
)

type AuthHandler struct {
	queries *db.Queries
}

func NewAuthHandler(queries *db.Queries) *AuthHandler {
	return &AuthHandler{
		queries: queries,
	}
}

type RegisterRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
	CreatedAt string `json:"created_at"`
}

// Register handles user registration
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req RegisterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Validate required fields
	if req.Email == "" || req.Password == "" || req.Firstname == "" || req.Lastname == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "All fields are required",
		})
	}

	// Basic email validation
	if !strings.Contains(req.Email, "@") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid email format",
		})
	}

	// Hash the password
	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to process password",
		})
	}

	// Create user in database
	user, err := h.queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:     req.Email,
		Password:  hashedPassword,
		Firstname: req.Firstname,
		Lastname:  req.Lastname,
	})

	if err != nil {
		// Check for duplicate email
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "Email already registered",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create user",
		})
	}

	// Return user response (without password)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": "User registered successfully",
		"user": UserResponse{
			ID:        user.ID.String(),
			Email:     user.Email,
			Firstname: user.Firstname,
			Lastname:  user.Lastname,
			CreatedAt: user.CreatedAt.Time.String(),
		},
	})
}

// Login handles user login
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req LoginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Validate required fields
	if req.Email == "" || req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email and password are required",
		})
	}

	// Get user by email
	user, err := h.queries.GetUserByEmail(context.Background(), req.Email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid credentials",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to authenticate",
		})
	}

	// Check password
	if err := utils.CheckPassword(user.Password, req.Password); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid credentials",
		})
	}

	// Return success (JWT will be added in next step)
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Login successful",
		"user": UserResponse{
			ID:        user.ID.String(),
			Email:     user.Email,
			Firstname: user.Firstname,
			Lastname:  user.Lastname,
			CreatedAt: user.CreatedAt.Time.String(),
		},
	})
}
