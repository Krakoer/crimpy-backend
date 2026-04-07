# Code generation

Install sqlc using `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`

then run `sqlc generate`

# Testing

## Test Structure

Tests are located in `tests/handler/` directory, separate from production code in `internal/handler/`.

## Setup

### Quick Start

```bash
just dev     # Start the development environment
just test    # Run all tests
```

The `just test` command automatically uses the development database configuration, so no manual environment variable setup is needed.

## Running Tests

### Using Just (recommended)
```bash
just test
just test-coverage
```

### Using Go directly
```bash
go test -v ./tests/...
go test -v ./tests/handler -run TestAuthHandler
go test -v ./tests/handler -run TestTrainingHandler
go test -v ./tests/handler -run TestSessionHandler
go test -v ./tests/handler -run TestRepeaterHandler
```

### Run specific test
```bash
go test -v ./tests/... -run TestTrainingHandler_GetTraining_UserIsolation
```

### Coverage report
```bash
go test -coverprofile=coverage.out ./tests/...
go tool cover -html=coverage.out -o coverage.html
```

## Test Structure

Tests use the `testutil` package (`tests/testutil/setup.go`) which provides:
- Database setup and cleanup
- Test user creation with JWT tokens
- Fiber app configuration for testing


# TODO
- [ ] Linting - CI
- [x] Unit tests
- [x] Password change route
- [x] Prebuilt superadmin
- [ ] Put in prod