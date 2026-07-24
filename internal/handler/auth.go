package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"crimpy/backend/internal/utils"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	IsCoach   bool   `json:"is_coach"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserResponse struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Firstname      string `json:"firstname"`
	Lastname       string `json:"lastname"`
	IsCoach        bool   `json:"is_coach"`
	CoachValidated bool   `json:"coach_validated"`
	IsAdmin        bool   `json:"is_admin"`
	EmailVerified  bool   `json:"email_verified"`
	CreatedAt      string `json:"created_at"`
}

// Register godoc
// @Summary Register a new user
// @Description Create a new user account with email and password
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration details"
// @Success 201 {object} map[string]interface{} "User registered successfully"
// @Failure 400 {object} map[string]string "Invalid request or validation error"
// @Failure 409 {object} map[string]string "Email already registered"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/register [post]
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

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		slog.Error("failed to hash password during registration", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to process password",
		})
	}

	// Create user in database based on account type
	var user db.User
	if req.IsCoach {
		user, err = h.queries.CreateCoachUser(context.Background(), db.CreateCoachUserParams{
			Email:     req.Email,
			Password:  hashedPassword,
			Firstname: req.Firstname,
			Lastname:  req.Lastname,
		})
	} else {
		user, err = h.queries.CreateUser(context.Background(), db.CreateUserParams{
			Email:     req.Email,
			Password:  hashedPassword,
			Firstname: req.Firstname,
			Lastname:  req.Lastname,
		})
	}

	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "Email already registered",
			})
		}
		slog.Error("failed to create user", "email", req.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create user",
		})
	}

	if utils.IsTestEnv() {
		// Auto-verify email in test environment
		err = h.queries.VerifyUserEmail(context.Background(), user.ID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to verify email",
			})
		}

		// Return simple success message for tests
		message := "User registered successfully"
		if req.IsCoach {
			message = "Coach account created. Pending admin validation."
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message": message,
			"user": UserResponse{
				ID:             user.ID.String(),
				Email:          user.Email,
				Firstname:      user.Firstname,
				Lastname:       user.Lastname,
				IsCoach:        user.IsCoach,
				CoachValidated: user.CoachValidated,
				IsAdmin:        user.IsAdmin,
				EmailVerified:  true,
				CreatedAt:      user.CreatedAt.Time.String(),
			},
		})
	}

	// Generate verification token and send email (production)
	verificationToken, err := utils.GenerateVerificationToken()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate verification token",
		})
	}

	// Set token expiration to 24 hours from now
	expiresAt := pgtype.Timestamptz{}
	expiresAt.Scan(time.Now().Add(24 * time.Hour))

	err = h.queries.SetVerificationToken(context.Background(), db.SetVerificationTokenParams{
		ID:                         user.ID,
		VerificationToken:          pgtype.Text{String: verificationToken, Valid: true},
		VerificationTokenExpiresAt: expiresAt,
	})
	if err != nil {
		slog.Error("failed to save verification token", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save verification token",
		})
	}

	if err := utils.SendVerificationEmail(user.Email, user.Firstname, verificationToken, req.IsCoach); err != nil {
		slog.Error("failed to send verification email", "user_id", user.ID.String(), "email", user.Email, "error", err)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message": "User registered, but verification email failed to send. Please request a new verification email.",
			"user": UserResponse{
				ID:             user.ID.String(),
				Email:          user.Email,
				Firstname:      user.Firstname,
				Lastname:       user.Lastname,
				IsCoach:        user.IsCoach,
				CoachValidated: user.CoachValidated,
				IsAdmin:        user.IsAdmin,
				EmailVerified:  user.EmailVerified,
				CreatedAt:      user.CreatedAt.Time.String(),
			},
		})
	}

	// Return user response (without password)
	message := "User registered successfully. Please check your email to verify your account."
	if req.IsCoach {
		message = "Coach account created. Please verify your email to proceed with admin validation."
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": message,
		"user": UserResponse{
			ID:             user.ID.String(),
			Email:          user.Email,
			Firstname:      user.Firstname,
			Lastname:       user.Lastname,
			IsCoach:        user.IsCoach,
			CoachValidated: user.CoachValidated,
			IsAdmin:        user.IsAdmin,
			EmailVerified:  user.EmailVerified,
			CreatedAt:      user.CreatedAt.Time.String(),
		},
	})
}

// Login godoc
// @Summary Login user
// @Description Authenticate user and return JWT token
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login credentials"
// @Success 200 {object} map[string]interface{} "Login successful with token"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Invalid credentials"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/login [post]
func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req LoginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.Email == "" || req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email and password are required",
		})
	}

	user, err := h.queries.GetUserByEmail(context.Background(), req.Email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid credentials",
			})
		}
		slog.Error("failed to authenticate user", "email", req.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to authenticate",
		})
	}

	if err := utils.CheckPassword(user.Password, req.Password); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid credentials",
		})
	}

	// Check if email is verified
	if !user.EmailVerified {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Please verify your email before logging in. Check your inbox for the verification link.",
		})
	}

	token, err := utils.GenerateJWT(user.ID.String(), user.Email, user.IsAdmin, user.IsCoach)
	if err != nil {
		slog.Error("failed to generate JWT", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate token",
		})
	}

	refreshToken, err := h.issueRefreshToken(context.Background(), user.ID)
	if err != nil {
		slog.Error("failed to create refresh token", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate token",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":       "Login successful",
		"token":         token,
		"refresh_token": refreshToken,
		"user": UserResponse{
			ID:             user.ID.String(),
			Email:          user.Email,
			Firstname:      user.Firstname,
			Lastname:       user.Lastname,
			IsCoach:        user.IsCoach,
			CoachValidated: user.CoachValidated,
			IsAdmin:        user.IsAdmin,
			EmailVerified:  user.EmailVerified,
			CreatedAt:      user.CreatedAt.Time.String(),
		},
	})
}

func (h *AuthHandler) issueRefreshToken(ctx context.Context, userID pgtype.UUID) (string, error) {
	raw, err := utils.GenerateRefreshToken()
	if err != nil {
		return "", err
	}
	expiresAt := pgtype.Timestamptz{}
	if err := expiresAt.Scan(time.Now().Add(utils.RefreshTokenTTL)); err != nil {
		return "", err
	}
	_, err = h.queries.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: utils.HashToken(raw),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh godoc
// @Summary Refresh access token
// @Description Exchange a valid refresh token for a new access token. The refresh token is rotated: the old one is revoked and a new one is returned.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body RefreshRequest true "Refresh token"
// @Success 200 {object} map[string]string "New access and refresh tokens"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 401 {object} map[string]string "Invalid or expired refresh token"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/refresh [post]
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var req RefreshRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if req.RefreshToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Refresh token is required"})
	}

	stored, err := h.queries.GetRefreshTokenByHash(context.Background(), utils.HashToken(req.RefreshToken))
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid refresh token"})
		}
		slog.Error("failed to look up refresh token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
	}

	if stored.Revoked || stored.ExpiresAt.Time.Before(time.Now()) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid refresh token"})
	}

	user, err := h.queries.GetUserByID(context.Background(), stored.UserID)
	if err != nil {
		slog.Error("failed to load user for refresh", "user_id", stored.UserID.String(), "error", err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid refresh token"})
	}

	if err := h.queries.RevokeRefreshToken(context.Background(), stored.ID); err != nil {
		slog.Error("failed to revoke refresh token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
	}

	accessToken, err := utils.GenerateJWT(user.ID.String(), user.Email, user.IsAdmin, user.IsCoach)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	newRefresh, err := h.issueRefreshToken(context.Background(), user.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"token":         accessToken,
		"refresh_token": newRefresh,
	})
}

// Logout godoc
// @Summary Logout
// @Description Revoke a refresh token. Always succeeds so clients can clear local state.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body RefreshRequest true "Refresh token to revoke"
// @Success 200 {object} map[string]string "Logged out"
// @Router /auth/logout [post]
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	var req RefreshRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Logged out"})
	}
	if req.RefreshToken != "" {
		stored, err := h.queries.GetRefreshTokenByHash(context.Background(), utils.HashToken(req.RefreshToken))
		if err == nil {
			if err := h.queries.RevokeRefreshToken(context.Background(), stored.ID); err != nil {
				slog.Error("failed to revoke refresh token on logout", "error", err)
			}
		}
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Logged out"})
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
	UserID      string `json:"user_id"`
}

// ChangePassword godoc
// @Summary Change user password
// @Description Change password for authenticated user or admin changing another user's password
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body ChangePasswordRequest true "Password change details"
// @Success 200 {object} map[string]string "Password updated successfully"
// @Failure 400 {object} map[string]string "Invalid request or validation error"
// @Failure 401 {object} map[string]string "Invalid old password"
// @Failure 403 {object} map[string]string "Only admins can change other users' passwords"
// @Failure 404 {object} map[string]string "User not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/auth/change-password [put]
func (h *AuthHandler) ChangePassword(c fiber.Ctx) error {
	var req ChangePasswordRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	currentUserID := middleware.GetUserID(c)
	isAdmin := middleware.IsAdmin(c)

	var targetUserID string
	requireOldPassword := true

	if req.UserID != "" && req.UserID != currentUserID {
		if !isAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Only admins can change other users' passwords",
			})
		}
		targetUserID = req.UserID
		requireOldPassword = false
	} else {
		targetUserID = currentUserID
	}

	if req.NewPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "New password is required",
		})
	}

	if len(req.NewPassword) < 6 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "New password must be at least 6 characters",
		})
	}

	var userUUID pgtype.UUID
	if err := userUUID.Scan(targetUserID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	user, err := h.queries.GetUserByID(context.Background(), userUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "User not found",
			})
		}
		slog.Error("failed to retrieve user for password change", "target_user_id", targetUserID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve user",
		})
	}

	if requireOldPassword {
		if req.OldPassword == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Old password is required",
			})
		}

		if err := utils.CheckPassword(user.Password, req.OldPassword); err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid old password",
			})
		}
	}

	hashedPassword, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		slog.Error("failed to hash new password", "target_user_id", targetUserID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to process password",
		})
	}

	err = h.queries.UpdateUserPassword(context.Background(), db.UpdateUserPasswordParams{
		ID:       userUUID,
		Password: hashedPassword,
	})
	if err != nil {
		slog.Error("failed to update password", "target_user_id", targetUserID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update password",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Password updated successfully",
	})
}

// GetCurrentUser godoc
// @Summary Get current user information
// @Description Get the authenticated user's information from their JWT token
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} UserResponse "User information"
// @Failure 401 {object} map[string]string "Unauthorized - invalid or missing token"
// @Failure 404 {object} map[string]string "User not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/user [get]
func (h *AuthHandler) GetCurrentUser(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid user ID",
		})
	}

	user, err := h.queries.GetUserByID(context.Background(), userUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "User not found",
			})
		}
		slog.Error("failed to retrieve current user", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve user",
		})
	}

	return c.Status(fiber.StatusOK).JSON(UserResponse{
		ID:             user.ID.String(),
		Email:          user.Email,
		Firstname:      user.Firstname,
		Lastname:       user.Lastname,
		IsCoach:        user.IsCoach,
		CoachValidated: user.CoachValidated,
		IsAdmin:        user.IsAdmin,
		EmailVerified:  user.EmailVerified,
		CreatedAt:      user.CreatedAt.Time.String(),
	})
}

type VerifyEmailRequest struct {
	Token string `json:"token"`
}

// VerifyEmail godoc
// @Summary Verify user email
// @Description Verify user email using the token sent via email
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body VerifyEmailRequest true "Validation Token"
// @Success 200 {object} map[string]string "Email verified successfully"
// @Failure 400 {object} map[string]string "Invalid or missing token"
// @Failure 404 {object} map[string]string "Invalid or expired token"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/verify [post]
func (h *AuthHandler) VerifyEmail(c fiber.Ctx) error {
	var req VerifyEmailRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.Token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Token is required",
		})
	}

	user, err := h.queries.GetUserByVerificationToken(context.Background(), pgtype.Text{String: req.Token, Valid: true})
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Invalid or expired verification token",
			})
		}
		slog.Error("failed to look up verification token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to verify email",
		})
	}

	err = h.queries.VerifyUserEmail(context.Background(), user.ID)
	if err != nil {
		slog.Error("failed to update email verification status", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update verification status",
		})
	}

	var msg string
	if user.IsCoach {
		msg = "Email verified successfully. An admin will validate your account soon."
	} else {
		msg = "Email verified successfully."
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":  msg,
		"is_coach": user.IsCoach,
	})
}

type ResendVerificationRequest struct {
	Email string `json:"email"`
}

// ResendVerificationEmail godoc
// @Summary Resend verification email
// @Description Resend verification email with a 10-minute cooldown per email
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body ResendVerificationRequest true "Email address"
// @Success 200 {object} map[string]string "Verification email sent"
// @Failure 400 {object} map[string]string "Invalid request or email already verified"
// @Failure 404 {object} map[string]string "User not found"
// @Failure 429 {object} map[string]string "Too many requests - cooldown active"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/resend-verification [post]
func (h *AuthHandler) ResendVerificationEmail(c fiber.Ctx) error {
	var req ResendVerificationRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email is required",
		})
	}

	user, err := h.queries.GetUserByEmail(context.Background(), req.Email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "User not found",
			})
		}
		slog.Error("failed to retrieve user for resend verification", "email", req.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve user",
		})
	}

	if user.EmailVerified {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email is already verified",
		})
	}

	// Check cooldown (10 minutes)
	lastSent, err := h.queries.GetVerificationEmailSentAt(context.Background(), req.Email)
	if err == nil && lastSent.Valid {
		timeSinceLastSent := time.Since(lastSent.Time)
		if timeSinceLastSent < 10*time.Minute {
			remainingTime := 10*time.Minute - timeSinceLastSent
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Please wait before requesting another verification email. Try again in " + remainingTime.Round(time.Second).String(),
			})
		}
	}

	// Generate new verification token
	verificationToken, err := utils.GenerateVerificationToken()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate verification token",
		})
	}

	// Set token expiration to 24 hours from now
	expiresAt := pgtype.Timestamptz{}
	expiresAt.Scan(time.Now().Add(24 * time.Hour))

	err = h.queries.SetVerificationToken(context.Background(), db.SetVerificationTokenParams{
		ID:                         user.ID,
		VerificationToken:          pgtype.Text{String: verificationToken, Valid: true},
		VerificationTokenExpiresAt: expiresAt,
	})
	if err != nil {
		slog.Error("failed to save resend verification token", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save verification token",
		})
	}

	if err := utils.SendVerificationEmail(user.Email, user.Firstname, verificationToken, user.IsCoach); err != nil {
		slog.Error("failed to resend verification email", "user_id", user.ID.String(), "email", user.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to send verification email",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "Verification email sent successfully. Please check your inbox.",
	})
}
