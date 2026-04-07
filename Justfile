make_migration name:
    atlas migrate diff {{name}} --env dev

migrate:
    atlas migrate apply --env dev

migrate_status:
    atlas migrate status --env dev

dev:
    docker-compose up --build

dev-d:
    docker-compose up -d --build

logs:
    docker-compose logs -f api

up:
    docker-compose up -d

down:
    docker-compose down

test:
    DATABASE_URL=postgres://user:pass@localhost:5432/crimpy?sslmode=disable JWT_SECRET=devsecret go test -v ./tests/...
