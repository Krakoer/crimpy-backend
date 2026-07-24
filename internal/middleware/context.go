package middleware

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
)

// RequestTimeout bounds how long any single request may spend in the database.
const RequestTimeout = 30 * time.Second

// RequestContext attaches a cancellable, deadline-bound context to the request
// so handlers can pass c.Context() to queries. Without it c.Context() is an
// empty background context, and work continues after the client goes away.
func RequestContext() fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.RequestCtx(), RequestTimeout)
		defer cancel()

		c.SetContext(ctx)
		return c.Next()
	}
}
