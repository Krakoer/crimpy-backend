env "dev" {
  src = "file://schema/schema.sql"
  url = "postgres://user:pass@localhost:5432/crimpy?sslmode=disable"
  dev = "docker://postgres/16/dev?search_path=public"

  migration {
    dir = "file://migrations"
  }
}

env "prod" {
  url = getenv("DATABASE_URL")

  migration {
    dir = "file://migrations"
  }
}