package handler_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"crimpy/backend/tests/testutil"

	"github.com/andybalholm/brotli"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
)

// readTrainings reads the library under one Accept-Encoding and hands back the
// raw bytes exactly as they went over the wire, since what this file is about
// is the byte count and not the decoded rows. app.Test decodes nothing, so the
// body here is what a client would have to decode itself.
func readTrainings(t *testing.T, app *fiber.App, token, query, acceptEncoding string) (*http.Response, []byte) {
	t.Helper()
	req := testutil.NewJSONRequestWithAuth(http.MethodGet, "/api/trainings"+query, nil, token)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read the response body: %v", err)
	}
	return resp, body
}

// seedCompressibleLibrary writes trainings big enough that the whole library
// answers well over the size below which fasthttp leaves a body alone, so the
// assertions below are about the middleware and not about a body too small to
// be worth compressing.
func seedCompressibleLibrary(t *testing.T, app *fiber.App, token string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		status, body := postJSON(t, app, "/api/trainings", token, map[string]interface{}{
			"title":         "Maximal hangs and repeaters",
			"description":   "Coach built session mixing maximal hangs, repeaters and a pulling circuit",
			"training_type": "workout",
			"goal":          "Hold finger strength while adding a first block of power endurance",
			"comment":       "Run this twice in the week with at least two full days between",
			"items": []map[string]interface{}{
				{
					"type":        "group",
					"group_title": "Warm up",
					"goal":        "Raise tissue temperature and open the fingers before loading them",
					"items": []map[string]interface{}{
						{"type": "free", "free_text": "Five minutes easy traversing on jugs", "duration": 300, "rest_seconds": 60, "protocol": "Move continuously, no cutting loose"},
						{"type": "free", "free_text": "Shoulder band work", "reps": 15, "rest_seconds": 45, "protocol": "Two sets each side, slow eccentric"},
					},
				},
				{
					"type": "repeater", "cycles": 6, "reps": 6, "duration": 7, "rest_seconds": 3,
					"cycle_rest_seconds": 150, "hand": "both", "granularity": "uniform",
					"edge_sizes_mm": []int{20}, "loads": []map[string]interface{}{{"kg": 0}},
					"goal":     "Local capillary and aerobic capacity in the forearms",
					"protocol": "Seven on three off, six repetitions, bodyweight",
				},
			},
		})
		if status != fiber.StatusCreated {
			t.Fatalf("Expected 201 creating training %d, got %d: %v", i, status, body)
		}
	}
}

func varyMentionsAcceptEncoding(resp *http.Response) bool {
	return strings.Contains(strings.ToLower(resp.Header.Get(fiber.HeaderVary)), "accept-encoding")
}

// A library read is the response Krakoer/crimpy#131 is about. It has to arrive
// compressed when the client says it can decode, and the bytes have to be the
// same JSON once decoded, otherwise the saving is a corrupted library.
func TestCompression_LibraryReadIsGzippedWhenTheClientOffersIt(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "gziplibrary@test.com")
	app := assessmentApp(t, pool, queries)

	seedCompressibleLibrary(t, app, token, 5)

	plainResp, plain := readTrainings(t, app, token, "?include=items", "")
	if plainResp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading the library, got %d", plainResp.StatusCode)
	}
	if encoding := plainResp.Header.Get(fiber.HeaderContentEncoding); encoding != "" {
		t.Fatalf("Expected no Content-Encoding without Accept-Encoding, got %q", encoding)
	}
	if !json.Valid(plain) {
		t.Fatalf("Expected the uncompressed body to be JSON, got %q", truncateBody(plain))
	}

	gzipResp, compressed := readTrainings(t, app, token, "?include=items", "gzip")
	if gzipResp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading the library gzipped, got %d", gzipResp.StatusCode)
	}
	if encoding := gzipResp.Header.Get(fiber.HeaderContentEncoding); encoding != "gzip" {
		t.Fatalf("Expected Content-Encoding gzip, got %q", encoding)
	}
	if len(compressed) >= len(plain) {
		t.Errorf("Expected the gzipped library to be smaller than %d bytes, got %d", len(plain), len(compressed))
	}
	if !varyMentionsAcceptEncoding(gzipResp) {
		t.Errorf("Expected Vary to name Accept-Encoding, got %q", gzipResp.Header.Get(fiber.HeaderVary))
	}
	// The ticket asked whether anything relies on Content-Length being the
	// uncompressed size. Nothing does, and the header itself is the compressed
	// length, so a client that did rely on it would be reading a correct number
	// for the bytes it actually receives rather than a stale one.
	if gzipResp.ContentLength != int64(len(compressed)) {
		t.Errorf("Expected Content-Length %d to be the compressed length, got %d", len(compressed), gzipResp.ContentLength)
	}

	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("Expected a gzip stream, got %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to decompress the library: %v", err)
	}
	if !bytes.Equal(decoded, plain) {
		t.Errorf("Expected the gzipped library to decode to the plain one, got %q", truncateBody(decoded))
	}
}

// The coach portal is a browser and offers brotli, which the middleware prefers
// over gzip. Both encodings have to answer the same library.
func TestCompression_LibraryReadIsBrotliForABrowser(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "brotlilibrary@test.com")
	app := assessmentApp(t, pool, queries)

	seedCompressibleLibrary(t, app, token, 5)

	_, plain := readTrainings(t, app, token, "?include=items", "")
	resp, compressed := readTrainings(t, app, token, "?include=items", "gzip, deflate, br, zstd")

	if encoding := resp.Header.Get(fiber.HeaderContentEncoding); encoding != "br" {
		t.Fatalf("Expected Content-Encoding br when the client offers it, got %q", encoding)
	}
	if len(compressed) >= len(plain) {
		t.Errorf("Expected the brotli library to be smaller than %d bytes, got %d", len(plain), len(compressed))
	}

	decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(compressed)))
	if err != nil {
		t.Fatalf("Failed to decompress the library: %v", err)
	}
	if !bytes.Equal(decoded, plain) {
		t.Errorf("Expected the brotli library to decode to the plain one, got %q", truncateBody(decoded))
	}
}

// A client that offers nothing gets the body it has always got. The app on an
// old build and anything reading the API with curl are that client.
func TestCompression_LibraryReadIsPlainWithoutAcceptEncoding(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "plainlibrary@test.com")
	app := assessmentApp(t, pool, queries)

	seedCompressibleLibrary(t, app, token, 5)

	resp, body := readTrainings(t, app, token, "?include=items", "")
	if encoding := resp.Header.Get(fiber.HeaderContentEncoding); encoding != "" {
		t.Fatalf("Expected no Content-Encoding, got %q", encoding)
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("Expected a plain JSON library, got %v: %q", err, truncateBody(body))
	}
	if len(rows) != 5 {
		t.Fatalf("Expected five trainings, got %d", len(rows))
	}
	if _, ok := rows[0]["items"]; !ok {
		t.Errorf("Expected the rows to carry their items, got %v", rows[0])
	}
}

// compressionFloor is fasthttp's minCompressLen: below it a body is left
// uncompressed, because the compressed form is likely to come out bigger.
// Fiber's compress middleware has no size setting of its own to override it,
// so this is the whole of the API's threshold behaviour.
const compressionFloor = 200

// compressionFloorApp carries the compression middleware and nothing else, so a
// response of an exact size can be asked for. The tests above run against the
// app testutil builds, which is the production stack; this one exists because
// no Crimpy endpoint answers a body in the narrow band the floor sits in, and a
// floor that is a third party's constant is exactly the thing an upgrade moves
// without telling anyone.
func compressionFloorApp() *fiber.App {
	app := fiber.New()
	app.Use(compress.New())
	filler := func(size int) fiber.Handler {
		return func(c fiber.Ctx) error {
			c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
			return c.Send(bytes.Repeat([]byte("a"), size))
		}
	}
	app.Get("/under-the-floor", filler(compressionFloor-1))
	app.Get("/at-the-floor", filler(compressionFloor))
	return app
}

// Both sides of the floor, so it is pinned rather than approached from one
// direction: a body one byte short stays plain however compressible it is, and
// a body exactly at it is compressed. A fasthttp upgrade that moves the
// constant either way fails here instead of changing what production sends.
func TestCompression_FloorSitsAtTwoHundredBytes(t *testing.T) {
	app := compressionFloorApp()

	for _, testCase := range []struct {
		path     string
		encoding string
	}{
		{"/under-the-floor", ""},
		{"/at-the-floor", "gzip"},
	} {
		req := testutil.NewRequest(http.MethodGet, testCase.path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("Request to %s failed: %v", testCase.path, err)
		}
		resp.Body.Close()
		if got := resp.Header.Get(fiber.HeaderContentEncoding); got != testCase.encoding {
			t.Errorf("Expected %s to answer Content-Encoding %q, got %q", testCase.path, testCase.encoding, got)
		}
	}
}

// And the smallest body the API really sends, an empty library, stays plain
// JSON for a client that offered every encoding there is.
func TestCompression_EmptyLibraryAnswersPlainJSON(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key")
	pool, queries := testutil.SetupTestDB(t)
	defer testutil.CleanupTestDB(t, pool)
	_, token := testutil.CreateTestUser(t, queries, "smallbody@test.com")
	app := assessmentApp(t, pool, queries)

	resp, body := readTrainings(t, app, token, "", "gzip, deflate, br, zstd")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("Expected 200 reading an empty library, got %d", resp.StatusCode)
	}
	if encoding := resp.Header.Get(fiber.HeaderContentEncoding); encoding != "" {
		t.Errorf("Expected no Content-Encoding on a two byte body, got %q", encoding)
	}
	if string(body) != "[]" {
		t.Errorf("Expected an empty library to answer [], got %q", truncateBody(body))
	}
}

// maxPrintedBodyBytes caps how much of a failing body reaches the test log. It
// has nothing to do with compressionFloor above and must not move with it.
const maxPrintedBodyBytes = 512

func truncateBody(body []byte) []byte {
	if len(body) > maxPrintedBodyBytes {
		return body[:maxPrintedBodyBytes]
	}
	return body
}
