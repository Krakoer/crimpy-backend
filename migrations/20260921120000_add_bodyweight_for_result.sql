-- The weigh-in a result is divided by, written once for every read path that has
-- to divide one.
--
-- Three queries answer by this rule now: the assessment listing, the two date
-- snapshot, and the session detail. Until this migration each carried its own
-- copy of the lateral, four copies in all, since the snapshot looks the weight up
-- once per hand. Copies of a rule drift, and two screens printing two different
-- ratios for one measurement is the failure the rule exists to prevent.
--
-- The rule: the last weigh-in taken at or before the result, widening to the
-- earliest one later the same day when nothing precedes it, because an athlete
-- who weighs themselves after training rather than before still weighed that on
-- the day. Widening only where nothing precedes keeps the morning weigh-in as the
-- denominator for an athlete who also weighs in at night, which is the weight the
-- result was actually pulled at.
--
-- p_as_of caps the search the way a read "as of" an instant needs it capped, so a
-- snapshot never divides by a weight that did not exist yet at that instant. A
-- caller with nothing to cap by passes 'infinity', which leaves the rule to the
-- other two bounds.
--
-- Two bounded searches rather than one ordered by a computed preference, so each
-- is answered straight off user_bodyweights_user_id_measured_at_idx. Ordering the
-- whole series by an expression would fetch and sort every weigh-in the athlete
-- ever recorded, once per result row.
--
-- LANGUAGE sql and STABLE so the planner inlines the body into the LATERAL that
-- calls it and plans those index probes inside the surrounding query, rather than
-- running an opaque call per row. Neither STRICT nor SECURITY DEFINER, both of
-- which would block that inlining.
--
-- The day is cut in UTC explicitly rather than through a server setting, so the
-- boundary does not move with one.
--
-- Written here rather than in schema/schema.sql because Atlas Community cannot
-- parse a function in a declarative schema: it refuses the file with "functions
-- and procedures are available to logged-in users only", which would break
-- "just make_migration" for every later ticket. It ignores functions when
-- inspecting a database, so "atlas migrate diff" still reports the directory in
-- sync with the schema once this has run. schema/schema.sql carries a comment
-- pointing here so the divergence is not silent.
CREATE FUNCTION bodyweight_for_result(
  p_user_id UUID,
  p_measured_at TIMESTAMPTZ,
  p_as_of TIMESTAMPTZ
)
RETURNS TABLE (weight_kg REAL, measured_at TIMESTAMPTZ)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
  SELECT candidate.weight_kg, candidate.measured_at
  FROM (
    (
      SELECT b.weight_kg, b.measured_at, 0 AS preference
      FROM user_bodyweights b
      WHERE b.user_id = p_user_id
        AND b.measured_at <= p_measured_at
        AND b.measured_at <= p_as_of
      ORDER BY b.measured_at DESC, b.created_at DESC
      LIMIT 1
    )
    UNION ALL
    (
      SELECT b.weight_kg, b.measured_at, 1 AS preference
      FROM user_bodyweights b
      WHERE b.user_id = p_user_id
        AND b.measured_at > p_measured_at
        AND b.measured_at <= p_as_of
        AND b.measured_at < date_trunc('day', p_measured_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' + interval '1 day'
      ORDER BY b.measured_at ASC, b.created_at DESC
      LIMIT 1
    )
  ) candidate
  ORDER BY candidate.preference
  LIMIT 1
$$;
