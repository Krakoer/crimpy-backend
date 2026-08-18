-- Bound the activity label to the set the app knows: 0 hangboard, 1 climbing,
-- 2 stretching, 3 workout, 4 other. It was left unconstrained when session_type
-- was split, so any integer was storable.
ALTER TABLE "sessions" ADD CONSTRAINT "sessions_activity_check"
  CHECK ("activity" BETWEEN 0 AND 4);
