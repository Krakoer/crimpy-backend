make_migration name:
    atlas migrate diff {{name}} --env dev

migrate:
    atlas migrate apply --env dev

migrate_status:
    atlas migrate status --env dev

dev:
    docker compose up --build

dev-d:
    docker compose up -d --build

logs:
    docker compose logs -f api

up:
    docker compose up -d

down:
    docker compose down

test:
    DATABASE_URL=postgres://user:pass@localhost:5432/crimpy?sslmode=disable JWT_SECRET=devsecret go test -v ./tests/...

swagger:
    swag init -g cmd/api/main.go -o docs

prod-up:
    docker compose -f docker-compose.prod.yml up -d --build

prod-down:
    docker compose -f docker-compose.prod.yml down

prod-logs:
    docker compose -f docker-compose.prod.yml logs -f api

prod-restart:
    docker compose -f docker-compose.prod.yml restart api
