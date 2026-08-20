-- Modify "sessions" table
ALTER TABLE "sessions" ADD CONSTRAINT "sessions_training_has_prescription_check" CHECK ((training_id IS NULL) OR (prescription IS NOT NULL));
