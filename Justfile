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
    #!/usr/bin/env zsh
    mkdir -p pgdata
    export DOCKER_UID=$(id -u) DOCKER_GID=$(id -g) && docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --build

prod-down:
    #!/usr/bin/env zsh
    export DOCKER_UID=$(id -u) DOCKER_GID=$(id -g) && docker compose -f docker-compose.prod.yml --env-file .env.prod down

prod-logs:
    #!/usr/bin/env zsh
    export DOCKER_UID=$(id -u) DOCKER_GID=$(id -g) && docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f api

prod-restart:
    #!/usr/bin/env zsh
    export DOCKER_UID=$(id -u) DOCKER_GID=$(id -g) && docker compose -f docker-compose.prod.yml --env-file .env.prod restart api
