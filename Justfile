make_migration name:
    atlas migrate diff {{name}} --env dev

migrate:
    atlas migrate apply --env dev

migrate_status:
    atlas migrate status --env dev

dev-build:
    docker compose -f docker-compose.dev.yml up --build

dev command:
    docker compose -f docker-compose.dev.yml {{command}}

dev-d:
    docker compose -f docker-compose.dev.yml up -d --build

logs:
    docker compose -f docker-compose.dev.yml logs -f api

up:
    docker compose -f docker-compose.dev.yml up -d

down:
    docker compose -f docker-compose.dev.yml down

status:
    docker compose -f docker-compose.dev.yml ps

test:
    DATABASE_URL=postgres://user:pass@localhost:5432/crimpy?sslmode=disable JWT_SECRET=devsecret ENV=test go test -v ./tests/...

test-coverage:
    DATABASE_URL=postgres://user:pass@localhost:5432/crimpy?sslmode=disable JWT_SECRET=devsecret ENV=test go test -coverpkg=./internal/... -coverprofile=coverage.out ./tests/...
    go tool cover -html=coverage.out -o coverage.html

swagger:
    swag init -g cmd/api/main.go -o docs

prod-up:
    mkdir -p pgdata
    docker compose --env-file .env.prod pull
    docker compose --env-file .env.prod up -d

prod-down:
    docker compose --env-file .env.prod down

prod-logs:
    docker compose --env-file .env.prod logs -f api

prod-restart:
    docker compose --env-file .env.prod restart api

prod-pull:
    docker compose --env-file .env.prod pull
    docker compose --env-file .env.prod up -d

preprod-up:
    docker compose --env-file .env.preprod -f docker-compose.preprod.yml pull
    docker compose --env-file .env.preprod -f docker-compose.preprod.yml up -d

preprod-down:
    docker compose --env-file .env.preprod -f docker-compose.preprod.yml down

preprod-logs:
    docker compose --env-file .env.preprod -f docker-compose.preprod.yml logs -f api

preprod command:
    docker compose --env-file .env.preprod -f docker-compose.preprod.yml {{command}}

preprod-release *args:
    ./scripts/preprod-release.sh {{args}}

prod-release bump *args:
    ./scripts/prod-release.sh {{bump}} {{args}}
