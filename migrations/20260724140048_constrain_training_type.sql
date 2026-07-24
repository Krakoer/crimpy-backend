-- Modify "trainings" table
ALTER TABLE "trainings" ADD CONSTRAINT "trainings_training_type_check" CHECK (training_type = ANY (ARRAY['crimpy'::text, 'climbing'::text, 'stretching'::text, 'workout'::text]));
