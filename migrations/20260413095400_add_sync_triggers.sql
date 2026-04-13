-- Trigger function to auto-update server_updated_at and sync_version
CREATE OR REPLACE FUNCTION bump_sync_version()
RETURNS TRIGGER AS $$
BEGIN
  NEW.server_updated_at := now();
  NEW.updated_at := now();
  NEW.sync_version := COALESCE(
    (SELECT MAX(sync_version) FROM sessions WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM assessments WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM trainings WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM repeaters WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM rep_templates WHERE user_id = NEW.user_id), 0
  ) + COALESCE(
    (SELECT MAX(sync_version) FROM rep_datas WHERE user_id = NEW.user_id), 0
  ) + 1;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply triggers to all synced tables
CREATE TRIGGER trg_bump_sync_version_sessions
BEFORE INSERT OR UPDATE ON sessions
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_assessments
BEFORE INSERT OR UPDATE ON assessments
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_trainings
BEFORE INSERT OR UPDATE ON trainings
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_repeaters
BEFORE INSERT OR UPDATE ON repeaters
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_rep_templates
BEFORE INSERT OR UPDATE ON rep_templates
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();

CREATE TRIGGER trg_bump_sync_version_rep_datas
BEFORE INSERT OR UPDATE ON rep_datas
FOR EACH ROW EXECUTE FUNCTION bump_sync_version();
