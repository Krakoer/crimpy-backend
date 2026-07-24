package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"crimpy/backend/internal/middleware"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CoachHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewCoachHandler(queries *db.Queries, pool *pgxpool.Pool) *CoachHandler {
	return &CoachHandler{queries: queries, pool: pool}
}

type EnrollmentTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type EnrollmentTokenInfoResponse struct {
	CoachID        string `json:"coach_id"`
	CoachFirstname string `json:"coach_firstname"`
	CoachLastname  string `json:"coach_lastname"`
	CoachEmail     string `json:"coach_email"`
	ExpiresAt      string `json:"expires_at"`
}

type EnrolledUserResponse struct {
	EnrollmentID  string `json:"enrollment_id"`
	UserID        string `json:"user_id"`
	UserFirstname string `json:"user_firstname"`
	UserLastname  string `json:"user_lastname"`
	UserEmail     string `json:"user_email"`
	EnrolledAt    string `json:"enrolled_at"`
}

type UserEnrollmentResponse struct {
	EnrollmentID   string `json:"enrollment_id"`
	CoachID        string `json:"coach_id"`
	CoachFirstname string `json:"coach_firstname"`
	CoachLastname  string `json:"coach_lastname"`
	CoachEmail     string `json:"coach_email"`
	EnrolledAt     string `json:"enrolled_at"`
}

// GenerateEnrollmentToken godoc
// @Summary Generate an enrollment token
// @Description Generate a one-time enrollment link token (valid 7 days). Requires a validated coach account.
// @Tags Enrollment
// @Produce json
// @Security BearerAuth
// @Success 201 {object} EnrollmentTokenResponse "Token generated"
// @Failure 403 {object} map[string]string "Not a validated coach"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/coach/enrollment-token [post]
func (h *CoachHandler) GenerateEnrollmentToken(c fiber.Ctx) error {
	if !middleware.IsCoach(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach account required"})
	}

	userID := middleware.GetUserID(c)

	var coachUUID pgtype.UUID
	if err := coachUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	coach, err := h.queries.GetUserByID(context.Background(), coachUUID)
	if err != nil {
		slog.Error("failed to retrieve coach", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve coach"})
	}

	if !coach.CoachValidated {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach account not yet validated by admin"})
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		slog.Error("failed to generate token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	tokenStr := hex.EncodeToString(tokenBytes)

	expiresAt := pgtype.Timestamptz{}
	expiresAt.Scan(time.Now().Add(7 * 24 * time.Hour))

	record, err := h.queries.CreateEnrollmentToken(context.Background(), db.CreateEnrollmentTokenParams{
		CoachID:   coachUUID,
		Token:     tokenStr,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		slog.Error("failed to create enrollment token", "coach_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create enrollment token"})
	}

	return c.Status(fiber.StatusCreated).JSON(EnrollmentTokenResponse{
		Token:     record.Token,
		ExpiresAt: record.ExpiresAt.Time.UTC().Format(time.RFC3339),
	})
}

// GetEnrollmentTokenInfo godoc
// @Summary Get enrollment token info
// @Description Validate a token and return the coach's info. Used before accepting enrollment.
// @Tags Enrollment
// @Produce json
// @Security BearerAuth
// @Param token path string true "Enrollment token"
// @Success 200 {object} EnrollmentTokenInfoResponse "Token info"
// @Failure 400 {object} map[string]string "Token expired or already used"
// @Failure 404 {object} map[string]string "Token not found"
// @Router /api/enrollment/{token} [get]
func (h *CoachHandler) GetEnrollmentTokenInfo(c fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token is required"})
	}

	record, err := h.queries.GetEnrollmentTokenByValue(context.Background(), token)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Token not found"})
		}
		slog.Error("failed to look up enrollment token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to look up token"})
	}

	if record.UsedAt.Valid {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token has already been used"})
	}

	if time.Now().After(record.ExpiresAt.Time) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token has expired"})
	}

	return c.Status(fiber.StatusOK).JSON(EnrollmentTokenInfoResponse{
		CoachID:        record.CoachID.String(),
		CoachFirstname: record.CoachFirstname,
		CoachLastname:  record.CoachLastname,
		CoachEmail:     record.CoachEmail,
		ExpiresAt:      record.ExpiresAt.Time.UTC().Format(time.RFC3339),
	})
}

// AcceptEnrollment godoc
// @Summary Accept enrollment
// @Description Accept a coach enrollment using a one-time token.
// @Tags Enrollment
// @Produce json
// @Security BearerAuth
// @Param token path string true "Enrollment token"
// @Success 201 {object} UserEnrollmentResponse "Enrolled successfully"
// @Failure 400 {object} map[string]string "Token expired, already used, or user already enrolled"
// @Failure 403 {object} map[string]string "Coach cannot enroll themselves"
// @Failure 404 {object} map[string]string "Token not found"
// @Failure 409 {object} map[string]string "Already enrolled with a coach"
// @Router /api/enrollment/{token}/accept [post]
func (h *CoachHandler) AcceptEnrollment(c fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token is required"})
	}

	userID := middleware.GetUserID(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	record, err := h.queries.GetEnrollmentTokenByValue(context.Background(), token)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Token not found"})
		}
		slog.Error("failed to look up enrollment token", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to look up token"})
	}

	if record.UsedAt.Valid {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token has already been used"})
	}

	if time.Now().After(record.ExpiresAt.Time) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token has expired"})
	}

	if record.CoachID.Bytes == userUUID.Bytes {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach cannot enroll themselves"})
	}

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to process enrollment"})
	}
	defer tx.Rollback(context.Background())

	qtx := h.queries.WithTx(tx)

	// Guarded by used_at IS NULL so concurrent requests cannot consume the same token.
	tag, err := qtx.UseEnrollmentToken(context.Background(), db.UseEnrollmentTokenParams{
		Token:  token,
		UsedBy: userUUID,
	})
	if err != nil {
		slog.Error("failed to mark token as used", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to process enrollment"})
	}
	if tag.RowsAffected() == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token has already been used"})
	}

	enrollment, err := qtx.CreateCoachEnrollment(context.Background(), db.CreateCoachEnrollmentParams{
		CoachID: record.CoachID,
		UserID:  userUUID,
	})
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Already enrolled with a coach"})
		}
		slog.Error("failed to create enrollment", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create enrollment"})
	}

	if err := tx.Commit(context.Background()); err != nil {
		slog.Error("failed to commit enrollment transaction", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to finalize enrollment"})
	}

	return c.Status(fiber.StatusCreated).JSON(UserEnrollmentResponse{
		EnrollmentID:   enrollment.ID.String(),
		CoachID:        record.CoachID.String(),
		CoachFirstname: record.CoachFirstname,
		CoachLastname:  record.CoachLastname,
		CoachEmail:     record.CoachEmail,
		EnrolledAt:     enrollment.EnrolledAt.Time.UTC().Format(time.RFC3339),
	})
}

// GetCoachEnrollments godoc
// @Summary List coach's enrolled clients
// @Description Get all users enrolled under the authenticated coach.
// @Tags Enrollment
// @Produce json
// @Security BearerAuth
// @Success 200 {array} EnrolledUserResponse "List of enrolled users"
// @Failure 403 {object} map[string]string "Not a coach"
// @Router /api/coach/enrollments [get]
func (h *CoachHandler) GetCoachEnrollments(c fiber.Ctx) error {
	if !middleware.IsCoach(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach account required"})
	}

	userID := middleware.GetUserID(c)

	var coachUUID pgtype.UUID
	if err := coachUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	enrollments, err := h.queries.GetEnrollmentsByCoach(context.Background(), coachUUID)
	if err != nil {
		slog.Error("failed to retrieve coach enrollments", "coach_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve enrollments"})
	}

	result := make([]EnrolledUserResponse, 0, len(enrollments))
	for _, e := range enrollments {
		result = append(result, EnrolledUserResponse{
			EnrollmentID:  e.ID.String(),
			UserID:        e.UserID.String(),
			UserFirstname: e.UserFirstname,
			UserLastname:  e.UserLastname,
			UserEmail:     e.UserEmail,
			EnrolledAt:    e.EnrolledAt.Time.UTC().Format(time.RFC3339),
		})
	}

	return c.Status(fiber.StatusOK).JSON(result)
}

// GetUserEnrollment godoc
// @Summary Get user's current coach enrollment
// @Description Get the coach the authenticated user is currently enrolled with.
// @Tags Enrollment
// @Produce json
// @Security BearerAuth
// @Success 200 {object} UserEnrollmentResponse "Current enrollment"
// @Failure 404 {object} map[string]string "Not enrolled with any coach"
// @Router /api/user/enrollment [get]
func (h *CoachHandler) GetUserEnrollment(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	enrollment, err := h.queries.GetEnrollmentByUser(context.Background(), userUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Not enrolled with any coach"})
		}
		slog.Error("failed to retrieve user enrollment", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve enrollment"})
	}

	return c.Status(fiber.StatusOK).JSON(UserEnrollmentResponse{
		EnrollmentID:   enrollment.ID.String(),
		CoachID:        enrollment.CoachID.String(),
		CoachFirstname: enrollment.CoachFirstname,
		CoachLastname:  enrollment.CoachLastname,
		CoachEmail:     enrollment.CoachEmail,
		EnrolledAt:     enrollment.EnrolledAt.Time.UTC().Format(time.RFC3339),
	})
}

// UnenrollUser godoc
// @Summary Coach removes a client
// @Description Remove a user from the coach's enrolled clients.
// @Tags Enrollment
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "User ID to unenroll"
// @Success 200 {object} map[string]string "User unenrolled"
// @Failure 403 {object} map[string]string "Not a coach"
// @Failure 404 {object} map[string]string "User not enrolled by this coach"
// @Router /api/coach/enrollments/{user_id} [delete]
func (h *CoachHandler) UnenrollUser(c fiber.Ctx) error {
	if !middleware.IsCoach(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach account required"})
	}

	coachID := middleware.GetUserID(c)

	var coachUUID pgtype.UUID
	if err := coachUUID.Scan(coachID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid coach ID"})
	}

	var targetUUID pgtype.UUID
	if err := targetUUID.Scan(c.Params("user_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	tag, err := h.queries.DeleteCoachEnrollment(context.Background(), db.DeleteCoachEnrollmentParams{
		CoachID: coachUUID,
		UserID:  targetUUID,
	})
	if err != nil {
		slog.Error("failed to delete coach enrollment", "coach_id", coachID, "user_id", c.Params("user_id"), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to unenroll user"})
	}

	if tag.RowsAffected() == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not enrolled by this coach"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "User unenrolled successfully"})
}

// LeaveCoach godoc
// @Summary User leaves their coach
// @Description Remove the authenticated user from their current coach enrollment.
// @Tags Enrollment
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]string "Left coach successfully"
// @Failure 404 {object} map[string]string "Not enrolled with any coach"
// @Router /api/user/enrollment [delete]
func (h *CoachHandler) LeaveCoach(c fiber.Ctx) error {
	userID := middleware.GetUserID(c)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user ID"})
	}

	tag, err := h.queries.DeleteUserEnrollment(context.Background(), userUUID)
	if err != nil {
		slog.Error("failed to delete user enrollment", "user_id", userID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to leave coach"})
	}

	if tag.RowsAffected() == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Not enrolled with any coach"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Left coach successfully"})
}

// verifyCoachClientRelationship checks that the caller is a coach and that the given user is enrolled with them.
// Returns the parsed client UUID and an error response already written to c, or a zero-value UUID if ok is false.
func (h *CoachHandler) verifyCoachClientRelationship(c fiber.Ctx, clientIDStr string) (pgtype.UUID, bool) {
	if !middleware.IsCoach(c) {
		c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Coach account required"})
		return pgtype.UUID{}, false
	}

	coachID := middleware.GetUserID(c)

	var coachUUID pgtype.UUID
	if err := coachUUID.Scan(coachID); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid coach ID"})
		return pgtype.UUID{}, false
	}

	var clientUUID pgtype.UUID
	if err := clientUUID.Scan(clientIDStr); err != nil {
		c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid client ID"})
		return pgtype.UUID{}, false
	}

	_, err := h.queries.GetCoachEnrollment(context.Background(), db.GetCoachEnrollmentParams{
		CoachID: coachUUID,
		UserID:  clientUUID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "User is not enrolled with this coach"})
			return pgtype.UUID{}, false
		}
		slog.Error("failed to verify coach-client relationship", "coach_id", coachID, "client_id", clientIDStr, "error", err)
		c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to verify enrollment"})
		return pgtype.UUID{}, false
	}

	return clientUUID, true
}

// GetClientSessions godoc
// @Summary Get a client's sessions
// @Description Retrieve all sessions for a user enrolled with the authenticated coach.
// @Tags Coaching
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Success 200 {array} map[string]interface{} "List of sessions"
// @Failure 403 {object} map[string]string "Not a coach or user not enrolled"
// @Router /api/coach/clients/{user_id}/sessions [get]
func (h *CoachHandler) GetClientSessions(c fiber.Ctx) error {
	clientUUID, ok := h.verifyCoachClientRelationship(c, c.Params("user_id"))
	if !ok {
		return nil
	}

	sessions, err := h.queries.GetUserSessions(context.Background(), clientUUID)
	if err != nil {
		slog.Error("failed to retrieve client sessions", "client_id", c.Params("user_id"), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve sessions"})
	}

	return c.Status(fiber.StatusOK).JSON(sessions)
}

// GetClientSession godoc
// @Summary Get a client's session details
// @Description Retrieve a specific session with its rep data and assessments for a user enrolled with the authenticated coach.
// @Tags Coaching
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param session_id path string true "Session ID"
// @Success 200 {object} map[string]interface{} "Session details with rep_datas and assessments"
// @Failure 400 {object} map[string]string "Invalid session ID"
// @Failure 403 {object} map[string]string "Not a coach or user not enrolled"
// @Failure 404 {object} map[string]string "Session not found or does not belong to client"
// @Router /api/coach/clients/{user_id}/sessions/{session_id} [get]
func (h *CoachHandler) GetClientSession(c fiber.Ctx) error {
	clientUUID, ok := h.verifyCoachClientRelationship(c, c.Params("user_id"))
	if !ok {
		return nil
	}

	var sessionUUID pgtype.UUID
	if err := sessionUUID.Scan(c.Params("session_id")); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid session ID"})
	}

	session, err := h.queries.GetSession(context.Background(), sessionUUID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	if session.UserID.Bytes != clientUUID.Bytes {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Session not found"})
	}

	repDatas, _ := h.queries.GetSessionRepDatas(context.Background(), session.ID)
	assessments, _ := h.queries.GetSessionAssessments(context.Background(), session.ID)

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"session":     session,
		"rep_datas":   repDatas,
		"assessments": assessments,
	})
}

// GetClientAssessments godoc
// @Summary Get a client's assessments
// @Description Retrieve all assessments for a user enrolled with the authenticated coach.
// @Tags Coaching
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Success 200 {array} map[string]interface{} "List of assessments"
// @Failure 403 {object} map[string]string "Not a coach or user not enrolled"
// @Router /api/coach/clients/{user_id}/assessments [get]
func (h *CoachHandler) GetClientAssessments(c fiber.Ctx) error {
	clientUUID, ok := h.verifyCoachClientRelationship(c, c.Params("user_id"))
	if !ok {
		return nil
	}

	assessments, err := h.queries.GetUserAssessments(context.Background(), clientUUID)
	if err != nil {
		slog.Error("failed to retrieve client assessments", "client_id", c.Params("user_id"), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve assessments"})
	}

	return c.Status(fiber.StatusOK).JSON(assessments)
}
