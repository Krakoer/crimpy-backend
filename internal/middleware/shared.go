package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
)

// RegisterShared registers the middleware the served app and the app the tests
// build must both run, in the order they must run in.
//
// It exists because tests/testutil.SetupFiberApp hand maintains its own copy of
// the stack, so anything registered in cmd/api alone is behaviour no test can
// see: compression pinned only there would leave the tests green while the
// served API sent every response uncompressed. One list rather than two is what
// closes that. This list is only what the test app must run too, which is not
// the same as every middleware that takes no configuration: the request
// logger, recover, the failure logger and CORS all stay in cmd/api, and moving
// one of them here would change what every integration test's app does.
//
// Compression: GET /api/trainings?include=items answers a whole coach library
// in one body, and JSON of that shape shrinks by better than a factor of ten.
// The encoding is negotiated from Accept-Encoding, so the browser portal gets
// brotli and the app gets gzip.
//
// Krakoer/crimpy#131 wanted a size threshold rather than compressing
// everything. compress.Config cannot express one: Next is its only hook and
// runs before the handler, with nothing to measure. Writing a middleware of our
// own to get it was not worth it, since no response measured came out bigger
// compressed. What is left is the floor fasthttp applies for itself, pinned by
// TestCompression_FloorSitsAtTwoHundredBytes.
func RegisterShared(app *fiber.App) {
	app.Use(compress.New())
	app.Use(RequestContext())
}
