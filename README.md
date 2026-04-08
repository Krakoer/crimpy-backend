# Crimpy Backend API

Climbing training backend built with Go, PostgreSQL, and Fiber. Connects to a Flutter mobile app that interfaces with BLE force sensors.

## Tech Stack

- Go 1.26+ with Fiber v3
- PostgreSQL 16 (pgx driver)
- sqlc for type-safe SQL code generation
- Atlas for database migrations
- Docker Compose for development and production
- Air for hot reloading in development

## Requirements

### Development Setup (Bare Linux)

```bash
# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER
# Log out and back in for group changes to take effect

# Install Docker Compose
sudo apt-get update
sudo apt-get install docker-compose-plugin

# Install Go (1.26+)
wget https://go.dev/dl/go1.26.1.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.1.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc

# Install Just (command runner)
curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to ~/bin
echo 'export PATH=$PATH:$HOME/bin' >> ~/.bashrc
source ~/.bashrc

# Install development tools
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/swaggo/swag/cmd/swag@latest
curl -sSf https://atlasgo.sh | sh

# Verify installations
docker --version
docker compose version
go version
just --version
sqlc version
atlas version
```

## Quick Start

```bash
# Clone repository
git clone <repository-url>
cd crimpy-backend

# Start development environment
just dev

# API available at http://localhost:3000
# Swagger docs at http://localhost:3000/swagger/index.html
```

## Development Commands

```bash
just dev              # Start dev environment with hot reload
just dev-d            # Start in detached mode
just down             # Stop containers
just logs             # View API container logs
just test             # Run tests
just swagger          # Regenerate API documentation
```

## Database Commands

```bash
just make_migration <name>   # Create new migration
just migrate                 # Apply migrations
just migrate_status          # Check migration status
sqlc generate                # Regenerate Go code from SQL queries
```

## Project Structure

```
cmd/api/main.go              # Application entry point
internal/
  handler/                   # HTTP handlers (auth, training, session, repeater)
  middleware/                # JWT authentication
  db/                        # sqlc-generated code (DO NOT EDIT)
  database/                  # Database connection
  utils/                     # JWT, password hashing, admin init
queries/*.sql                # SQL queries with sqlc annotations
schema/schema.sql            # Database schema (single source of truth)
migrations/                  # Atlas-generated migrations
tests/
  handler/                   # Integration tests
  testutil/                  # Test utilities
```

## Development Workflow

1. **Start coding**: `just dev`
2. **Make database changes**: Edit `schema/schema.sql` ; `just make_migration <name>` ; `just migrate`
3. **Add/modify queries**: Edit `queries/*.sql` ; `sqlc generate`
4. **Format code**: `go fmt ./...`
5. **Test**: `just test`
6. **Update docs**: `just swagger` (after adding handler annotations)

## API Documentation

Interactive Swagger documentation is auto-generated from code annotations:
- **Local**: http://localhost:3000/swagger/index.html
- **Production**: https://api.portfolio-online.ovh/swagger/index.html

### Adding documentation for new endpoints

```go
// HandlerName godoc
// @Summary Brief description
// @Description Detailed description
// @Tags TagName
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Resource ID"
// @Success 200 {object} ResponseType
// @Router /api/resource [post]
func (h *Handler) HandlerName(c fiber.Ctx) error {
    // implementation
}
```

Run `just swagger` to regenerate documentation.

## Testing

```bash
just test                              # Run all tests
go test -v ./tests/... -run <TestName> # Run specific test
```

Tests use the `handler_test` package and verify:
- CRUD operations
- User isolation (users cannot access others' data)
- Authentication and authorization

## Production Deployment

### Initial VPS Setup

```bash
# Clone repository on VPS
git clone <repository-url>
cd crimpy-backend

# Configure environment
cp .env.prod.example .env.prod
nano .env.prod  # Edit with production values

# Set secure credentials:
# - DB_USER, DB_PASSWORD (avoid special chars like % and &)
# - JWT_SECRET (generate with: openssl rand -base64 32)
# - DOCKERHUB_USERNAME (your DockerHub username)
# - DATABASE_URL (update with your DB_PASSWORD)

# Start services
just prod-up
```

### Automated Deployment with GitHub Actions

**One-time GitHub setup:**
1. Go to repository Settings ; Secrets and variables ; Actions
2. Add `DOCKERHUB_USERNAME` (your DockerHub username)
3. Add `DOCKERHUB_TOKEN` (create at https://hub.docker.com/settings/security)

**Release process:**

```bash
# Create and push version tag
git tag v1.0.0
git push origin v1.0.0
```

GitHub Actions automatically:
- Builds Docker image for multiple platforms
- Pushes to DockerHub with version tag + `latest`
- Creates GitHub release

**Deploy to VPS:**

```bash
# On VPS
cd /path/to/crimpy-backend
just prod-pull
```

**Rollback:**

```bash
# Edit .env.prod: API_VERSION=v0.9.0
just prod-pull
```

### Production Commands

```bash
just prod-up       # Start production (with build)
just prod-pull     # Pull latest images and restart
just prod-down     # Stop production
just prod-logs     # View API logs
just prod-restart  # Restart API service
```

## Traefik Configuration

The production setup uses Traefik as a reverse proxy:
- **Host**: `api.portfolio-online.ovh`
- **Entrypoint**: `websecure` (HTTPS)
- **Certificate resolver**: `dnsResolver`

Prerequisites:
- Traefik running with external network named `proxy`
- DNS configured to point domain to VPS

## Authentication Flow

1. User registers via `/auth/register` (public)
2. User logs in via `/auth/login` (public) - returns JWT token (7-day expiry)
3. Protected routes require `Authorization: Bearer <token>` header
4. `AuthMiddleware()` validates JWT and stores claims in context
5. Handlers use `middleware.GetUserID()` to retrieve authenticated user

## Environment Variables

**Development** (auto-set by docker-compose and `just test`):
```bash
DATABASE_URL=postgres://user:pass@localhost:5432/crimpy?sslmode=disable
JWT_SECRET=devsecret
ENV=development
```

**Production** (set via `.env.prod`):
```bash
DATABASE_URL=postgres://...
JWT_SECRET=<strong-random-secret>
PORT=3000
DOCKERHUB_USERNAME=<your-username>
API_VERSION=latest
```

## Code Style

- Self-documenting code with clear names
- Comments only for tricky logic
- Run `go fmt ./...` before committing

## Key Patterns

- **User Isolation**: All handlers verify resource ownership before access/modification
- **SQL Code Generation**: Never edit `internal/db/` manually - use `sqlc generate`
- **Database Migrations**: Modify `schema/schema.sql` ; `just make_migration` ; `just migrate`
- **Testing**: External `handler_test` package with automatic database cleanup

## Contributing

1. Create feature branch
2. Make changes following code style
3. Add tests for new functionality
4. Run `just test` and `go fmt ./...`
5. Update Swagger docs if adding/modifying endpoints
6. Create pull request

## Additional Documentation

See [CLAUDE.md](CLAUDE.md) for detailed architecture documentation and Claude Code-specific guidance.

## License

MIT
