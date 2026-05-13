-- Modify "tags" table
ALTER TABLE "tags" ALTER COLUMN "coach_id" DROP NOT NULL, ADD COLUMN "is_builtin" boolean NOT NULL DEFAULT false;
-- Seed the system stretching tag
INSERT INTO tags (coach_id, name, color, is_builtin) VALUES (NULL, 'Stretching', '#4DB6AC', TRUE);
