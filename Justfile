# Database migration commands

# Generate a new migration based on schema changes
make_migration name:
    atlas migrate diff {{name}} --env dev

migrate:
    atlas migrate apply --env dev

migrate_status:
    atlas migrate status --env dev

up:
    docker-compose up -d

down:
    docker-compose down
