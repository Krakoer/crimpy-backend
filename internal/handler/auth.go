package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"crimpy/backend/internal/utils"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// minPasswordLength is the shortest password accepted at registration and on change.
const minPasswordLength = 6

// normalizeEmail makes address comparison case and whitespace insensitive.
// Every read and write of users.email goes through it.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

type AuthHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewAuthHandler(queries *db.Queries, pool *pgxpool.Pool) *AuthHandler {
	return &AuthHandler{queries: queries, pool: pool}
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
// @Description Create a new user account with email and password. An address that already has an account gets the same answer as a new one, so the endpoint does not tell a caller which addresses are registered: its owner is emailed instead, with a fresh verification link if the account is still unverified, or a note that someone tried to register with it otherwise.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration details"
// @Success 201 {object} map[string]interface{} "User registered successfully"
// @Failure 400 {object} map[string]string "Invalid request or validation error"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/register [post]
func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req RegisterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	req.Email = normalizeEmail(req.Email)

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

	if len(req.Password) < minPasswordLength {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Password must be at least 6 characters",
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
		user, err = h.queries.CreateCoachUser(c.Context(), db.CreateCoachUserParams{
			Email:     req.Email,
			Password:  hashedPassword,
			Firstname: req.Firstname,
			Lastname:  req.Lastname,
		})
	} else {
		user, err = h.queries.CreateUser(c.Context(), db.CreateUserParams{
			Email:     req.Email,
			Password:  hashedPassword,
			Firstname: req.Firstname,
			Lastname:  req.Lastname,
		})
	}

	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			h.notifyOwnerOfTakenAddress(c.Context(), req.Email)
			return registrationAccepted(c, req.IsCoach, takenAddressUserResponse(req))
		}
		slog.Error("failed to create user", "email", req.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create user",
		})
	}

	if utils.IsTestEnv() {
		// Auto-verify email in test environment
		err = h.queries.VerifyUserEmail(c.Context(), user.ID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to verify email",
			})
		}
		user.EmailVerified = true
		return registrationAccepted(c, req.IsCoach, newUserResponse(user))
	}

	if _, err := h.sendVerificationEmail(c.Context(), user); err != nil {
		slog.Error("failed to send verification email", "user_id", user.ID.String(), "email", user.Email, "error", err)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message": "User registered, but verification email failed to send. Please request a new verification email.",
			"user":    newUserResponse(user),
		})
	}

	return registrationAccepted(c, req.IsCoach, newUserResponse(user))
}

// registrationAccepted is the one answer a registration gets, whether it
// created an account or found the address taken.
func registrationAccepted(c fiber.Ctx, isCoach bool, user UserResponse) error {
	message := "User registered successfully. Please check your email to verify your account."
	if isCoach {
		message = "Coach account created. Please verify your email to proceed with admin validation."
	}
	if utils.IsTestEnv() {
		message = "User registered successfully"
		if isCoach {
			message = "Coach account created. Pending admin validation."
		}
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message": message,
		"user":    user,
	})
}

func newUserResponse(user db.User) UserResponse {
	return UserResponse{
		ID:             user.ID.String(),
		Email:          user.Email,
		Firstname:      user.Firstname,
		Lastname:       user.Lastname,
		IsCoach:        user.IsCoach,
		CoachValidated: user.CoachValidated,
		IsAdmin:        user.IsAdmin,
		EmailVerified:  user.EmailVerified,
		CreatedAt:      user.CreatedAt.Time.String(),
	}
}

// takenAddressUserResponse is shaped like the user a new registration returns,
// built from the request alone, so nothing in it tells a caller the address
// already had an account. Truncating the clock drops its monotonic reading and
// matches the precision Postgres stores, so created_at prints the same way.
func takenAddressUserResponse(req RegisterRequest) UserResponse {
	emailVerified := utils.IsTestEnv()
	return UserResponse{
		ID:            uuid.NewString(),
		Email:         req.Email,
		Firstname:     req.Firstname,
		Lastname:      req.Lastname,
		IsCoach:       req.IsCoach,
		EmailVerified: emailVerified,
		CreatedAt:     time.Now().Truncate(time.Microsecond).String(),
	}
}

// notifyOwnerOfTakenAddress emails whoever owns an address someone just tried
// to register: an unverified owner is most likely the one retrying, and gets a
// fresh verification link unless one went out in the last cooldown; a verified
// owner is told about the attempt and pointed at sign in. Failures are logged
// and never surface, since the answer must not differ from a new registration.
func (h *AuthHandler) notifyOwnerOfTakenAddress(ctx context.Context, email string) {
	owner, err := h.queries.GetUserByEmail(ctx, email)
	if err != nil {
		slog.Error("failed to load the owner of a taken address", "error", err)
		return
	}

	if owner.EmailVerified {
		claimed, err := h.queries.ClaimAccountNotice(ctx, db.ClaimAccountNoticeParams{
			ID:           owner.ID,
			ResendCutoff: pgtype.Timestamptz{Time: time.Now().Add(-accountNoticeCooldown), Valid: true},
		})
		if err != nil {
			slog.Error("failed to claim an account already registered notice", "user_id", owner.ID.String(), "error", err)
			return
		}
		if claimed == 0 {
			return
		}
		if err := utils.SendAccountAlreadyRegisteredEmail(ctx, owner.Email, owner.Firstname); err != nil {
			slog.Error("failed to send account already registered email", "user_id", owner.ID.String(), "error", err)
		}
		return
	}

	if _, err := h.sendVerificationEmail(ctx, owner); err != nil {
		slog.Error("failed to resend verification email for a taken address", "user_id", owner.ID.String(), "error", err)
	}
}

// accountNoticeCooldown is how long after telling an owner about an attempt to
// register with their address another attempt goes unmentioned. It is claimed
// in the same update that checks it, so concurrent attempts send one email.
const accountNoticeCooldown = 10 * time.Minute

// verificationEmailCooldown is how long after a verification email another one
// is held back for the same account.
const verificationEmailCooldown = 10 * time.Minute

// sendVerificationEmail stores a new verification token, valid 24 hours, and
// emails the link, unless the account is verified or was sent one within the
// cooldown. The write is what checks both, so concurrent requests send at most
// one email. Reports whether it sent.
func (h *AuthHandler) sendVerificationEmail(ctx context.Context, user db.User) (bool, error) {
	verificationToken, err := utils.GenerateVerificationToken()
	if err != nil {
		return false, err
	}

	now := time.Now()
	stored, err := h.queries.SetVerificationToken(ctx, db.SetVerificationTokenParams{
		ID:                         user.ID,
		VerificationToken:          pgtype.Text{String: verificationToken, Valid: true},
		VerificationTokenExpiresAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ResendCutoff:               pgtype.Timestamptz{Time: now.Add(-verificationEmailCooldown), Valid: true},
	})
	if err != nil {
		return false, err
	}
	if stored == 0 {
		return false, nil
	}

	return true, utils.SendVerificationEmail(ctx, user.Email, user.Firstname, verificationToken, user.IsCoach)
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

	req.Email = normalizeEmail(req.Email)

	if req.Email == "" || req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email and password are required",
		})
	}

	user, err := h.queries.GetUserByEmail(c.Context(), req.Email)
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

	refreshToken, _, err := issueRefreshToken(c.Context(), h.queries, user.ID)
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

func issueRefreshToken(ctx context.Context, q *db.Queries, userID pgtype.UUID) (string, pgtype.UUID, error) {
	raw, err := utils.GenerateRefreshToken()
	if err != nil {
		return "", pgtype.UUID{}, err
	}
	expiresAt := pgtype.Timestamptz{}
	if err := expiresAt.Scan(time.Now().Add(utils.RefreshTokenTTL)); err != nil {
		return "", pgtype.UUID{}, err
	}
	created, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: utils.HashToken(raw),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", pgtype.UUID{}, err
	}
	return raw, created.ID, nil
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh godoc
// @Summary Refresh access token
// @Description Exchange a valid refresh token for a new access token. The refresh token is rotated: the old one is revoked and a new one is returned. For one minute after a rotation the old token may be presented again, as long as its successor was never used and the session was not ended by sign out or a password change. That reissue revokes the undelivered successor.
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

	tx, err := h.pool.Begin(c.Context())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
	}
	defer tx.Rollback(c.Context())

	qtx := h.queries.WithTx(tx)

	// Locked so two refreshes presenting the same token take turns, and the
	// second one sees what the first did to it.
	//
	// Holding this lock, the successor insert below takes FOR KEY SHARE on the
	// user row for its foreign key. An admin deleting that user at the same
	// moment holds the row FOR UPDATE and cascades into this token, so one of
	// the two is aborted as a deadlock: the refresh answers 500 for an account
	// that is going away, or the admin retries the delete. Neither leaves a
	// token behind, so it is accepted rather than locked around.
	stored, err := qtx.LockRefreshTokenByHash(c.Context(), utils.HashToken(req.RefreshToken))
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid refresh token"})
		}
		slog.Error("failed to look up refresh token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
	}

	if stored.ExpiresAt.Time.Before(time.Now()) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid refresh token"})
	}
	if stored.Revoked {
		allowed, err := reissuableWithinGrace(c.Context(), qtx, stored)
		if err != nil {
			slog.Error("failed to check refresh token grace", "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
		}
		if !allowed {
			slog.Warn("auth rejected", "reason", "revoked refresh token presented", "user_id", stored.UserID.String(), "ip", c.IP())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid refresh token"})
		}
		// The client never got the successor, so it goes: one live token per
		// chain, and the one about to be issued is it.
		if err := qtx.RevokeRefreshToken(c.Context(), stored.ReplacedBy); err != nil {
			slog.Error("failed to revoke undelivered refresh token", "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
		}
	}

	user, err := qtx.GetUserByID(c.Context(), stored.UserID)
	if err != nil {
		slog.Error("failed to load user for refresh", "user_id", stored.UserID.String(), "error", err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid refresh token"})
	}

	accessToken, err := utils.GenerateJWT(user.ID.String(), user.Email, user.IsAdmin, user.IsCoach)
	if err != nil {
		slog.Error("failed to generate JWT", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	newRefresh, newRefreshID, err := issueRefreshToken(c.Context(), qtx, user.ID)
	if err != nil {
		slog.Error("failed to create refresh token", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	if err := qtx.RotateRefreshToken(c.Context(), db.RotateRefreshTokenParams{
		ID:         stored.ID,
		ReplacedBy: newRefreshID,
	}); err != nil {
		slog.Error("failed to rotate refresh token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
	}

	// Nothing is revoked until the successor is stored with it, so a failure
	// anywhere above leaves the presented token as good as it was.
	if err := tx.Commit(c.Context()); err != nil {
		slog.Error("failed to commit refresh token rotation", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to refresh token"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"token":         accessToken,
		"refresh_token": newRefresh,
	})
}

// reissuableWithinGrace reports whether a revoked refresh token may be
// exchanged again because the answer to its rotation never reached the client.
// That holds only shortly after the rotation, and only while the successor is
// untouched: a successor that was presented, revoked or expired proves the
// client did get it, or that the chain was ended on purpose.
func reissuableWithinGrace(ctx context.Context, q *db.Queries, stored db.RefreshToken) (bool, error) {
	if !stored.ReplacedBy.Valid || !stored.RevokedAt.Valid {
		return false, nil
	}
	if time.Since(stored.RevokedAt.Time) > utils.RefreshTokenReuseGrace {
		return false, nil
	}
	successor, err := q.LockRefreshTokenByID(ctx, stored.ReplacedBy)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return !successor.Revoked && successor.ExpiresAt.Time.After(time.Now()), nil
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
		if err := h.revokeWithUndeliveredSuccessor(c.Context(), utils.HashToken(req.RefreshToken)); err != nil {
			slog.Error("failed to revoke refresh token on logout", "error", err)
		}
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Logged out"})
}

// revokeWithUndeliveredSuccessor revokes a refresh token, and the successor its
// rotation minted if that was never used: a client signing out with a rotated
// token is the one that never received it, and leaving it live would keep the
// session open. Rows are locked token first, then successor, the order Refresh
// takes them in.
func (h *AuthHandler) revokeWithUndeliveredSuccessor(ctx context.Context, tokenHash string) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	stored, err := qtx.LockRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	if stored.ReplacedBy.Valid {
		successor, err := qtx.LockRefreshTokenByID(ctx, stored.ReplacedBy)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil && !successor.Revoked {
			if err := qtx.RevokeRefreshToken(ctx, successor.ID); err != nil {
				return err
			}
		}
	}
	if err := qtx.RevokeRefreshToken(ctx, stored.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// errUserGone reports a password change for an account deleted while the
// request was being handled.
var errUserGone = errors.New("user no longer exists")

// updatePasswordAndRevokeSessions stores the new password and ends every
// session established with the old one, or does neither: a password reported
// as changed while those sessions live on is the failure this exists to rule
// out.
//
// The user row is locked first, then the refresh tokens. A refresh holds a
// token while its successor insert takes FOR KEY SHARE on the user row, and
// that does not wait on the FOR NO KEY UPDATE this password update takes,
// which is why the two orders cannot deadlock. An update that also wrote email
// or id would take FOR UPDATE instead and deadlock with every refresh running
// during a password change.
//
// The refresh token rows are locked in one statement and revoked in the next:
// a refresh holding one of them commits its successor in between, and only a
// statement started after that commit sees the successor to revoke it. That
// covers the refresh already running when the revoke starts; a second one,
// presenting that successor in the gap between the two statements, would need
// a full client round trip to fit there.
func (h *AuthHandler) updatePasswordAndRevokeSessions(ctx context.Context, userID pgtype.UUID, hashedPassword string) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	updated, err := qtx.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		ID:       userID,
		Password: hashedPassword,
	})
	if err != nil {
		return err
	}
	if updated == 0 {
		return errUserGone
	}
	if err := revokeAllSessions(ctx, qtx, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// revokeAllSessions revokes every refresh token of an account. It runs inside
// a transaction that has already locked the user row by writing its password,
// for the lock order described on updatePasswordAndRevokeSessions.
func revokeAllSessions(ctx context.Context, qtx *db.Queries, userID pgtype.UUID) error {
	if err := qtx.LockUserRefreshTokens(ctx, userID); err != nil {
		return err
	}
	return qtx.RevokeUserRefreshTokens(ctx, userID)
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

	if len(req.NewPassword) < minPasswordLength {
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

	user, err := h.queries.GetUserByID(c.Context(), userUUID)
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

	if err := h.updatePasswordAndRevokeSessions(c.Context(), userUUID, hashedPassword); err != nil {
		if errors.Is(err, errUserGone) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "User not found",
			})
		}
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

	user, err := h.queries.GetUserByID(c.Context(), userUUID)
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

	user, err := h.queries.GetUserByVerificationToken(c.Context(), pgtype.Text{String: req.Token, Valid: true})
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

	err = h.queries.VerifyUserEmail(c.Context(), user.ID)
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

// resendVerificationAnswer is the same for an unknown address, a verified one,
// one inside its cooldown and one that was just sent a link, so the endpoint
// does not tell a caller which addresses are registered or verified.
const resendVerificationAnswer = "If this address has an account waiting for verification, a new verification link has been sent to it."

// ResendVerificationEmail godoc
// @Summary Resend verification email
// @Description Send a new verification link to an unverified account. The answer is the same whether or not the address has an account, is already verified, or asked less than 10 minutes ago, in which case nothing is sent.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body ResendVerificationRequest true "Email address"
// @Success 200 {object} map[string]string "Verification email sent if the account needs one"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/resend-verification [post]
func (h *AuthHandler) ResendVerificationEmail(c fiber.Ctx) error {
	var req ResendVerificationRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	req.Email = normalizeEmail(req.Email)

	if req.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email is required",
		})
	}

	user, err := h.queries.GetUserByEmail(c.Context(), req.Email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": resendVerificationAnswer})
		}
		slog.Error("failed to retrieve user for resend verification", "email", req.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve user",
		})
	}

	if _, err := h.sendVerificationEmail(c.Context(), user); err != nil {
		slog.Error("failed to resend verification email", "user_id", user.ID.String(), "email", user.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to send verification email",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": resendVerificationAnswer})
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// forgotPasswordAnswer is the same whether or not the address has an account,
// and whether or not the cooldown held the email back. Only a failed send
// answers otherwise, since that is a retry the caller has to know about.
const forgotPasswordAnswer = "If an account exists for this email, a password reset link has been sent to it."

// ForgotPassword godoc
// @Summary Request a password reset email
// @Description Email a password reset link, valid for one hour, to the account with this address. The answer is the same whether or not the account exists. A request made within a minute of the previous one for the same account sends nothing.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body ForgotPasswordRequest true "Email address"
// @Success 200 {object} map[string]string "Reset email sent if the account exists"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/forgot-password [post]
func (h *AuthHandler) ForgotPassword(c fiber.Ctx) error {
	var req ForgotPasswordRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	req.Email = normalizeEmail(req.Email)

	if req.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email is required",
		})
	}

	user, err := h.queries.GetUserByEmail(c.Context(), req.Email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": forgotPasswordAnswer})
		}
		slog.Error("failed to retrieve user for password reset", "email", req.Email, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to request password reset",
		})
	}

	resetToken, err := utils.GenerateVerificationToken()
	if err != nil {
		slog.Error("failed to generate password reset token", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to request password reset",
		})
	}

	now := time.Now()
	stored, err := h.queries.SetPasswordResetToken(c.Context(), db.SetPasswordResetTokenParams{
		ID:           user.ID,
		TokenHash:    pgtype.Text{String: utils.HashToken(resetToken), Valid: true},
		RequestedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		ResendCutoff: pgtype.Timestamptz{Time: now.Add(-utils.PasswordResetResendCooldown), Valid: true},
	})
	if err != nil {
		slog.Error("failed to save password reset token", "user_id", user.ID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to request password reset",
		})
	}
	if stored == 0 {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": forgotPasswordAnswer})
	}

	if err := utils.SendPasswordResetEmail(c.Context(), user.Email, user.Firstname, resetToken); err != nil {
		slog.Error("failed to send password reset email", "user_id", user.ID.String(), "error", err)
		// Dropped so the cooldown does not hold back a retry for an email that
		// never went out. Detached from the request, whose deadline may be what
		// failed the send.
		clearCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Context()), 5*time.Second)
		defer cancel()
		if err := h.queries.ClearPasswordResetToken(clearCtx, user.ID); err != nil {
			slog.Error("failed to clear unsent password reset token", "user_id", user.ID.String(), "error", err)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to send password reset email",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": forgotPasswordAnswer})
}

type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// errResetTokenInvalid reports a reset token that is unknown, already used,
// superseded by a newer request, or older than PasswordResetTokenTTL.
var errResetTokenInvalid = errors.New("invalid or expired password reset token")

// resetPasswordAndRevokeSessions consumes a reset token, stores the new
// password and ends every session of the account, or does none of it. The
// token is matched and cleared by the same update, so two requests presenting
// it cannot both succeed.
func (h *AuthHandler) resetPasswordAndRevokeSessions(ctx context.Context, resetToken, hashedPassword string) (db.ResetPasswordWithTokenRow, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return db.ResetPasswordWithTokenRow{}, err
	}
	defer tx.Rollback(ctx)

	qtx := h.queries.WithTx(tx)

	user, err := qtx.ResetPasswordWithToken(ctx, db.ResetPasswordWithTokenParams{
		Password:    hashedPassword,
		TokenHash:   pgtype.Text{String: utils.HashToken(resetToken), Valid: true},
		IssuedAfter: pgtype.Timestamptz{Time: time.Now().Add(-utils.PasswordResetTokenTTL), Valid: true},
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return db.ResetPasswordWithTokenRow{}, errResetTokenInvalid
		}
		return db.ResetPasswordWithTokenRow{}, err
	}
	if err := revokeAllSessions(ctx, qtx, user.ID); err != nil {
		return db.ResetPasswordWithTokenRow{}, err
	}
	return user, tx.Commit(ctx)
}

// ResetPassword godoc
// @Summary Reset password with an emailed token
// @Description Set a new password using the token from a password reset email. The token is single use and valid for one hour. Every session of the account is signed out, and the email address is marked verified since the link reached it.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body ResetPasswordRequest true "Reset token and new password"
// @Success 200 {object} map[string]interface{} "Password reset"
// @Failure 400 {object} map[string]string "Invalid request or validation error"
// @Failure 404 {object} map[string]string "Invalid or expired reset token"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /auth/reset-password [post]
func (h *AuthHandler) ResetPassword(c fiber.Ctx) error {
	var req ResetPasswordRequest
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

	if len(req.NewPassword) < minPasswordLength {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "New password must be at least 6 characters",
		})
	}

	hashedPassword, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		slog.Error("failed to hash password during reset", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to process password",
		})
	}

	user, err := h.resetPasswordAndRevokeSessions(c.Context(), req.Token, hashedPassword)
	if err != nil {
		if errors.Is(err, errResetTokenInvalid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "This reset link is invalid or has expired. Please request a new one.",
			})
		}
		slog.Error("failed to reset password", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to reset password",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message":  "Password reset successfully. You can now sign in with your new password.",
		"is_coach": user.IsCoach,
	})
}
