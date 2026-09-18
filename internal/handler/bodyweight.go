package handler

import (
	"context"
	"crimpy/backend/internal/db"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The bounds the column's own check enforces, restated here so a bad number is
// refused with a sentence rather than as a constraint violation.
const (
	minBodyweightKg = 0
	maxBodyweightKg = 500
)

// How many measurements a read hands back by default, and the most it will. A
// coach reads a trend rather than a life history, and the series is written at
// most a few times a week.
const (
	defaultBodyweightLimit = 60
	maxBodyweightLimit     = 365
)

type BodyweightHandler struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewBodyweightHandler(queries *db.Queries, pool *pgxpool.Pool) *BodyweightHandler {
	return &BodyweightHandler{queries: queries, pool: pool}
}

func (h *BodyweightHandler) ownedBodyweight() ownedResource[db.UserBodyweight] {
	return ownedResource[db.UserBodyweight]{
		label: "Bodyweight",
		fetch: h.queries.GetUserBodyweight,
		owner: func(b db.UserBodyweight) pgtype.UUID { return b.UserID },
	}
}

type CreateBodyweightRequest struct {
	WeightKg float32 `json:"weight_kg"`
	// When the athlete weighed themselves, RFC3339. Absent means now, which is
	// what a measurement taken in the app while online is. A device that was
	// offline sends the moment it actually happened.
	MeasuredAt *string `json:"measured_at,omitempty"`
}

type BodyweightResponse struct {
	ID         string  `json:"id"`
	UserID     string  `json:"user_id"`
	WeightKg   float32 `json:"weight_kg"`
	MeasuredAt string  `json:"measured_at"`
	CreatedAt  string  `json:"created_at"`
}

func bodyweightToResponse(b db.UserBodyweight) BodyweightResponse {
	return BodyweightResponse{
		ID:         b.ID.String(),
		UserID:     b.UserID.String(),
		WeightKg:   b.WeightKg,
		MeasuredAt: b.MeasuredAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:  b.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// validateBodyweight refuses a weight the column would refuse anyway, and a
// measurement dated in the future, which is a clock that is wrong rather than a
// measurement: it would otherwise sit at the head of the series and be what
// every prescription freezes against until the real date catches up.
func validateBodyweight(req CreateBodyweightRequest) (pgtype.Timestamptz, error) {
	var measuredAt pgtype.Timestamptz
	if req.WeightKg <= minBodyweightKg || req.WeightKg > maxBodyweightKg {
		return measuredAt, fmt.Errorf("weight_kg must be above %d and at most %d", minBodyweightKg, maxBodyweightKg)
	}
	if req.MeasuredAt == nil {
		measuredAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		return measuredAt, nil
	}
	parsed, err := time.Parse(time.RFC3339, *req.MeasuredAt)
	if err != nil {
		return measuredAt, errors.New("measured_at must be an RFC3339 instant")
	}
	if parsed.After(time.Now().UTC().Add(time.Hour)) {
		return measuredAt, errors.New("measured_at cannot be in the future")
	}
	measuredAt = pgtype.Timestamptz{Time: parsed.UTC(), Valid: true}
	return measuredAt, nil
}

func parseBodyweightLimit(raw string) (int32, error) {
	if raw == "" {
		return defaultBodyweightLimit, nil
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || n <= 0 || n > maxBodyweightLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxBodyweightLimit)
	}
	return int32(n), nil
}

func (h *BodyweightHandler) listFor(c fiber.Ctx, userUUID pgtype.UUID) error {
	limit, err := parseBodyweightLimit(c.Query("limit"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	rows, err := h.queries.GetUserBodyweights(c.Context(), db.GetUserBodyweightsParams{
		UserID:   userUUID,
		RowLimit: limit,
	})
	if err != nil {
		slog.Error("failed to retrieve bodyweights", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to retrieve bodyweights"})
	}

	result := make([]BodyweightResponse, 0, len(rows))
	for _, row := range rows {
		result = append(result, bodyweightToResponse(row))
	}
	return c.Status(fiber.StatusOK).JSON(result)
}

// CreateMyBodyweight godoc
// @Summary Record my bodyweight
// @Description Append a dated measurement to the authenticated user's bodyweight series. measured_at is when the athlete weighed themselves and defaults to now, so a measurement taken while the device was offline keeps the day it belongs to when it is sent. The series is what makes a strength number readable as a ratio, and what a percent_bw prescription is frozen against.
// @Tags Bodyweight
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateBodyweightRequest true "The measurement"
// @Success 201 {object} BodyweightResponse "The recorded measurement"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/bodyweights [post]
func (h *BodyweightHandler) CreateMyBodyweight(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}

	var req CreateBodyweightRequest
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	measuredAt, err := validateBodyweight(req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	row, err := h.queries.CreateUserBodyweight(c.Context(), db.CreateUserBodyweightParams{
		UserID:     userUUID,
		WeightKg:   req.WeightKg,
		MeasuredAt: measuredAt,
	})
	if err != nil {
		slog.Error("failed to record bodyweight", "user_id", userUUID.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to record bodyweight"})
	}

	return c.Status(fiber.StatusCreated).JSON(bodyweightToResponse(row))
}

// GetMyBodyweights godoc
// @Summary List my bodyweight series
// @Description The authenticated user's measurements, most recently measured first.
// @Tags Bodyweight
// @Produce json
// @Security BearerAuth
// @Param limit query int false "How many measurements to return, 1 to 365, default 60"
// @Success 200 {array} BodyweightResponse "The series, newest first"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/bodyweights [get]
func (h *BodyweightHandler) GetMyBodyweights(c fiber.Ctx) error {
	userUUID, ok := requireCallerUUID(c)
	if !ok {
		return nil
	}
	return h.listFor(c, userUUID)
}

// DeleteMyBodyweight godoc
// @Summary Remove one of my measurements
// @Description Delete a measurement from the authenticated user's own series, for a number typed wrong. A measurement a past session was frozen against is unaffected: that value lives in the session's own snapshot.
// @Tags Bodyweight
// @Produce json
// @Security BearerAuth
// @Param id path string true "Measurement ID"
// @Success 200 {object} map[string]string "Deleted"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 404 {object} map[string]string "Measurement not found"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/user/bodyweights/{id} [delete]
func (h *BodyweightHandler) DeleteMyBodyweight(c fiber.Ctx) error {
	_, id, ok := h.ownedBodyweight().require(c)
	if !ok {
		return nil
	}

	if err := h.queries.DeleteUserBodyweight(c.Context(), id); err != nil {
		slog.Error("failed to delete bodyweight", "bodyweight_id", id.String(), "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete bodyweight"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Bodyweight deleted successfully"})
}

// GetClientBodyweights godoc
// @Summary List a client's bodyweight series
// @Description The measurements of an athlete enrolled with the authenticated coach, most recently measured first, so a strength number can be read as a ratio to the bodyweight of the day rather than as an absolute.
// @Tags Bodyweight
// @Produce json
// @Security BearerAuth
// @Param user_id path string true "Client user ID"
// @Param limit query int false "How many measurements to return, 1 to 365, default 60"
// @Success 200 {array} BodyweightResponse "The series, newest first"
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Access denied"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/coach/clients/{user_id}/bodyweights [get]
func (h *BodyweightHandler) GetClientBodyweights(c fiber.Ctx) error {
	coachUUID, ok := requireValidatedCoach(c, h.queries)
	if !ok {
		return nil
	}
	clientUUID, ok := verifyClientEnrolled(c, h.queries, coachUUID, c.Params("user_id"))
	if !ok {
		return nil
	}
	return h.listFor(c, clientUUID)
}

// latestBodyweight is the value in effect for a user, or nil when they have
// never recorded one. Used to freeze what a percent_bw load was resolved
// against, so the absence of a measurement is a case the caller has to answer
// for rather than a zero that would read as a real weight.
func latestBodyweight(ctx context.Context, qtx *db.Queries, userID pgtype.UUID) (*float32, error) {
	row, err := qtx.GetUserLatestBodyweight(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row.WeightKg, nil
}
