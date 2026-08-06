ALTER TABLE streams ADD COLUMN IF NOT EXISTS deleted_at timestamptz;
ALTER TABLE recordings ADD COLUMN IF NOT EXISTS hidden_at timestamptz;

DROP INDEX IF EXISTS streams_name_ci_unique;
DROP INDEX IF EXISTS streams_listener_port_unique;

CREATE UNIQUE INDEX IF NOT EXISTS streams_name_ci_active_unique
    ON streams(lower(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS streams_listener_port_active_unique
    ON streams(port) WHERE mode='listener' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS streams_active_name_idx
    ON streams(lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS recordings_visible_created_idx
    ON recordings(created_at DESC) WHERE hidden_at IS NULL;
