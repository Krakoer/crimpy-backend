-- Modify "sessions" table
ALTER TABLE "sessions" ADD CONSTRAINT "sessions_logged_has_no_program_session_check" CHECK ((origin = 'played'::text) OR (program_session_id IS NULL));
