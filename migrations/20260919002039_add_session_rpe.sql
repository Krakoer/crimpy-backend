-- Modify "sessions" table
ALTER TABLE "sessions" ADD CONSTRAINT "sessions_rpe_check" CHECK ((rpe IS NULL) OR ((rpe >= 5) AND (rpe <= 10))), ADD CONSTRAINT "sessions_rpe_failed_check" CHECK ((NOT rpe_failed) OR (rpe IS NULL)), ADD COLUMN "rpe" integer NULL, ADD COLUMN "rpe_failed" boolean NOT NULL DEFAULT false;
