# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Shell Usage

Always use the `zsh` shell when running commands.

## Code Style and Documentation

We value code that explains itself through clear class, method, and variable names. Comments may be used when necessary to explain tricky logic, but should otherwise be avoided. Write self-documenting code with descriptive names rather than relying on comments.

## Project Overview

Crimpy is a climbing training backend built with Go, PostgreSQL, and Fiber (HTTP framework). It connects to a Flutter mobile app that interfaces with BLE force sensors. The backend provides REST APIs for user authentication, training management, session tracking, and repeater configurations.

**Tech Stack:**
- Go 1.26+ with Fiber v3
- PostgreSQL 16 (pgx driver)
- sqlc for type-safe SQL code generation
- Atlas for database migrations
- Docker Compose for development and production
- Air for hot reloading in development

## Essential Commands

### Development Environment
```bash
just dev              # Start dev environment (docker-compose up --build)
just dev-d            # Start in detached mode
just up               # Start without rebuild (docker-compose up -d)
just down             # Stop containers
just logs             # View API container logs
```

### Database Operations
```bash
just make_migration <name>   # Create new migration (uses Atlas)
just migrate                 # Apply migrations to dev database
just migrate_status          # Check migration status
sqlc generate                # Regenerate Go code from SQL queries
```

### Testing
```bash
just test             # Run all tests (auto-configures DATABASE_URL and JWT_SECRET)
just test-coverage    # Generate HTML coverage report
go test -v ./tests/... -run <TestName>   # Run specific test
```

### Production Deployment
```bash
just prod-up          # Start production environment (builds and starts all services)
just prod-down        # Stop production environment
just prod-logs        # View production API logs
just prod-restart     # Restart production API service
```

### Code Quality
```bash
go fmt ./...          # Format all Go files (required after editing)
```

## Architecture

### Directory Structure

**`cmd/api/main.go`** - Application entry point. Sets up Fiber app, registers middleware, initializes handlers, and defines all routes (public auth routes + protected `/api/*` routes).

**`internal/handler/`** - HTTP handlers for each resource (auth, training, session, repeater). Each handler wraps sqlc-generated queries and implements CRUD operations with user ownership validation.

**`internal/middleware/`** - JWT authentication middleware that validates Bearer tokens and stores user context (`user_id`, `email`, `is_admin`) in request locals.

**`internal/db/`** - sqlc-generated code (models, queries). **Never edit manually** - regenerate with `sqlc generate` after modifying SQL files.

**`internal/database/`** - Database connection pool initialization using pgx.

**`internal/utils/`** - JWT generation/validation, password hashing (bcrypt), admin account initialization.

**`tests/testutil/`** - Test utilities for database setup, test user creation, and Fiber app configuration.

**`queries/*.sql`** - SQL queries with sqlc annotations. Modifications here require running `sqlc generate`.

**`schema/schema.sql`** - Single source of truth for database schema. Modify this, then run `just make_migration <name>` and `just migrate`.

**`migrations/`** - Atlas-generated migration files. Applied automatically in Docker or via `just migrate`.

**`tests/handler/`** - Integration tests using `handler_test` package. Tests verify CRUD operations and critical user isolation (users cannot access others' data).

### Key Patterns

**User Isolation:** All resource handlers (trainings, sessions) verify ownership before allowing access/modification. The `GetUserID()` and `IsAdmin()` middleware helpers extract user context. Admins can bypass ownership checks.

**SQL Code Generation:** All database interactions use sqlc-generated code. To add/modify queries:
1. Edit `queries/*.sql` files with sqlc annotations
2. Run `sqlc generate` to regenerate Go code in `internal/db/`
3. Never manually edit `internal/db/` files

**Database Migrations:** Schema changes follow this workflow:
1. Modify `schema/schema.sql`
2. Run `just make_migration <description>`
3. Run `just migrate` to apply
4. Commit both schema and migration files

**Testing:** Tests are external (`package handler_test`) and isolated from production code. The `testutil` package provides database setup/cleanup and test user creation. All tests run against the dev PostgreSQL instance with automatic cleanup of test data (emails matching `%test%`).

**JSON Field Names:** Database models return Go struct fields with capitalized names (e.g., `ID`, `UserID`, `Name`). When asserting JSON responses in tests, use capitalized field names.

### Authentication Flow

1. User registers via `/auth/register` (public) - password hashed with bcrypt
2. User logs in via `/auth/login` (public) - returns JWT token (7-day expiry)
3. Protected routes require `Authorization: Bearer <token>` header
4. `AuthMiddleware()` validates JWT and stores claims in context
5. Handlers use `middleware.GetUserID()` to retrieve authenticated user

### Environment Variables

Development (auto-set by docker-compose and `just test`):
- `DATABASE_URL=postgres://user:pass@localhost:5432/crimpy?sslmode=disable`
- `JWT_SECRET=devsecret`
- `ENV=development`

Production (set via deployment):
- `DATABASE_URL` - PostgreSQL connection string
- `JWT_SECRET` - Strong random secret for JWT signing
- `PORT` - HTTP port (defaults to 3000)

## Development Workflow

1. **Starting Work:** Run `just dev` to start containers
2. **Making Database Changes:** Edit `schema/schema.sql` → `just make_migration <name>` → `just migrate`
3. **Adding/Modifying Queries:** Edit `queries/*.sql` → `sqlc generate`
4. **After Editing Code:** Run `go fmt ./...`
5. **Before Committing:** Run `just test` to ensure all tests pass
6. **Viewing Logs:** Use `just logs` for debugging

## Production Deployment

This application is configured to work with **Traefik** as a reverse proxy. The API will be accessible at `api.portfolio-online.ovh`.

### Prerequisites

- VPS with Docker and Docker Compose installed
- Traefik reverse proxy running with:
  - External network named `proxy`
  - Entry point named `websecure`
  - Certificate resolver named `dnsResolver`

### Deployment Steps

1. **Clone the repository:**
   ```bash
   git clone <repository-url>
   cd crimpy-backend
   ```

2. **Configure environment:**
   ```bash
   cp .env.prod.example .env.prod
   nano .env.prod  # Edit with your production values
   ```

3. **Set secure credentials in .env.prod:**
   - `DB_USER` and `DB_PASSWORD`: Database credentials
   - `JWT_SECRET`: Generate with `openssl rand -base64 32`
   - `DATABASE_URL`: Update with your DB_PASSWORD (keep `@db:5432` as hostname)
   - `PORT`: Internal container port (default: 3000)

4. **Launch the backend:**
   ```bash
   just prod-up
   ```

This command will:
- Start PostgreSQL database with persistent storage in `./pgdata`
- Run database migrations automatically via Atlas
- Build and start the API server
- Connect to Traefik's `proxy` network for external access
- Configure automatic restarts on failure
- Provision SSL certificate via Traefik

### Traefik Configuration

The production setup includes these Traefik labels:
- `Host`: `api.portfolio-online.ovh`
- `Entrypoint`: `websecure` (HTTPS)
- `Certificate resolver`: `dnsResolver`

To change the domain, edit the `traefik.http.routers.crimpy-api.rule` label in [docker-compose.prod.yml](docker-compose.prod.yml).

**Important:**
- Ensure `.env.prod` is never committed to version control (already in .gitignore)
- The API container exposes port 3000 internally but is only accessible via Traefik
- Database data persists in `./pgdata` directory

## Testing Notes

- Tests are in `tests/handler/` using `handler_test` package
- `just test` auto-configures database connection (no manual env setup needed)
- All test data uses emails matching `%test%` pattern for cleanup
- User isolation tests verify users cannot access others' resources (returns 403)
- Database is automatically cleaned after each test via deferred cleanup

## Interacting with the API

Use Bruno CLI (`bru` command) for API testing. Bruno collection files are stored in `bruno/` directory. See `.claude/bruno.md` for details.
