-- Fix the misspelled column name, preserving existing rows.
ALTER TABLE "builtin_training_weights" RENAME COLUMN "builtin_traning_id" TO "builtin_training_id";
