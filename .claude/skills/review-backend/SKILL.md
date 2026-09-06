---
name: review-backend
description: Architecture, design and style knowledge for reviewing changes in crimpy-backend (Go, Fiber v3, sqlc, PostgreSQL). Use when reviewing a PR, branch or diff in crimpy-backend.
---

# Reviewing crimpy-backend

Go 1.26 + Fiber v3 + PostgreSQL (pgx) + sqlc + Atlas. REST API behind Traefik at
`api.crimpy.app`. Read `crimpy-backend/.claude/CLAUDE.md` for build and deploy
commands; this file is what to check in a diff.

## Layering

    cmd/api/main.go        entry point, middleware registration, all route definitions
    internal/handler/      one file per resource, HTTP layer, owns request/response types
    internal/middleware/   JWT auth, request context
    internal/db/           SQLC GENERATED, never hand-edited
    internal/database/     pgx pool setup
    internal/utils/        jwt, password, email, env, admin bootstrap
    queries/*.sql          sqlc source of truth for queries
    schema/schema.sql      single source of truth for the schema
    migrations/            Atlas-generated
    tests/handler/         integration tests, package handler_test
    contract/              cross-repo artefacts, canonical here and vendored by
                           crimpy-app and crimpy-frontend, whose CI diffs them

Flag any business logic that leaks into `cmd/api/main.go`, and any data access
that bypasses `internal/db` generated queries with hand-written SQL.

## Non-negotiables

**Generated code.** If `internal/db/*.sql.go` changed, `queries/*.sql` must have
changed too. A diff touching `internal/db/` alone means someone hand-edited
generated code. Same for schema: `schema/schema.sql` changes must ship with the
matching `migrations/` file in the same commit.

**Ownership checks.** Resource handlers must go through the generic helper in
`internal/handler/ownership.go`:

```go
func (h *ExerciseHandler) ownedExercise() ownedResource[db.Exercise] {
    return ownedResource[db.Exercise]{
        label: "Exercise",
        fetch: h.queries.GetExercise,
        owner: func(e db.Exercise) pgtype.UUID { return e.CoachID },
    }
}
```

Then `resource, id, ok := h.ownedExercise().require(c)`; return `nil` when `!ok`.
A hand-rolled ownership check in a new handler is a finding: it will drift from
the shared error shapes and status codes. Coach-only endpoints additionally go
through `requireValidatedCoach(c, queries)`.

**Missing ownership check entirely** is the highest-severity finding in this repo.
Any handler that loads a row by `:id` and does not verify the caller owns it is a
cross-tenant data leak. Check every new endpoint for it.

**Request context.** Handlers pass `c.Context()` to queries. `RequestContext()`
middleware binds a 30s deadline to it. `context.Background()` or `context.TODO()`
in a handler is a finding: the query outlives the client.

## Handler conventions

- One `XxxHandler` struct per resource with `NewXxxHandler(queries, pool)`.
- Request and response types live next to the handler: `CreateXxxRequest`,
  `UpdateXxxRequest`, `XxxResponse`, `XxxListResponse`.
- A `xxxToResponse(row)` mapper converts db rows to responses. Do not serialize
  `db.*` structs straight to JSON: pgtype fields leak an ugly shape.
- Nullable columns: check `.Valid`, then take the address of the inner value.
  ```go
  if e.Description.Valid { resp.Description = &e.Description.String }
  ```
- Timestamps out: `x.Time.UTC().Format(time.RFC3339)`.
- IDs out: `x.ID.String()`.
- Errors out: `c.Status(code).JSON(fiber.Map{"error": "Message"})`. Consistent
  status codes: 400 bad input, 401 bad auth, 403 not yours / not validated,
  404 missing, 500 server. Never return a raw `err.Error()` to the client.
- Logging is `log/slog` with key-value pairs, not formatted strings:
  `slog.Error("failed to retrieve resource", "resource", label, "error", err)`.
  Access denials log at `Warn`, failures at `Error`. No `fmt.Println`.

## Swagger

Every new or changed endpoint needs its godoc annotation block (`@Summary`,
`@Tags`, `@Security BearerAuth` on protected routes, `@Param`, `@Success`,
`@Failure`, `@Router`), and `just swagger` must have been run so `docs/` is
updated in the same PR. A handler change with no `docs/` diff is a finding.

## Tests

- Live in `tests/handler/`, `package handler_test`, external to production code.
- Shape: `t.Setenv("JWT_SECRET", ...)`, `pool, queries := testutil.SetupTestDB(t)`,
  `defer testutil.CleanupTestDB(t, pool)`, build the app with
  `testutil.SetupFiberApp(testutil.HandlerConfig{...})`.
- Test user emails MUST contain `test` (cleanup deletes on `%test%`). An email
  without it leaks rows into the dev database between runs.
- Every new endpoint needs at least: success, invalid input, and a
  **user-isolation test asserting 403** when another user's resource is targeted.
  A new endpoint with no isolation test is a finding.

## Before merge

`go fmt ./...`, `go build ./...`, `just test` all clean. `sqlc generate` and
`just swagger` re-run if their inputs changed.

## Style

Self-documenting names over comments; comment only genuinely tricky logic (the
doc comments on `ownedResource` are the right level). No unicode anywhere in code
or docs: no long dashes, ellipsis characters, arrows or emojis. Small atomic
commits, message at most 2 lines, no co-author or Claude trailer.
