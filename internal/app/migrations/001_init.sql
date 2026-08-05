CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id uuid PRIMARY KEY,
    username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash char(64) PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    ip text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id);

CREATE TABLE IF NOT EXISTS streams (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    mode text NOT NULL CHECK (mode IN ('listener','caller')),
    host text NOT NULL DEFAULT '',
    port integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    stream_id text NOT NULL DEFAULT '',
    latency_ms integer NOT NULL DEFAULT 200 CHECK (latency_ms BETWEEN 20 AND 8000),
    timeout_ms integer NOT NULL DEFAULT 30000 CHECK (timeout_ms BETWEEN 5000 AND 300000),
    passphrase_enc text NOT NULL DEFAULT '',
    has_passphrase boolean NOT NULL DEFAULT false,
    auto_mp4 boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS streams_name_ci_unique ON streams(lower(name));
CREATE UNIQUE INDEX IF NOT EXISTS streams_listener_port_unique ON streams(port) WHERE mode='listener';

CREATE TABLE IF NOT EXISTS recordings (
    id uuid PRIMARY KEY,
    stream_id uuid NOT NULL REFERENCES streams(id) ON DELETE RESTRICT,
    stream_name text NOT NULL,
    auto_mp4 boolean NOT NULL DEFAULT false,
    status text NOT NULL CHECK (status IN ('waiting_input','recording','finalizing','ready','failed')),
    end_reason text NOT NULL DEFAULT '',
    requested_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    ended_at timestamptz,
    working_path text NOT NULL DEFAULT '',
    progress_ms bigint NOT NULL DEFAULT 0,
    progress_size bigint NOT NULL DEFAULT 0,
    progress_bitrate text NOT NULL DEFAULT '',
    last_progress_at timestamptz,
    error_text text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS recordings_one_active_per_stream
    ON recordings(stream_id) WHERE status IN ('waiting_input','recording','finalizing');
CREATE INDEX IF NOT EXISTS recordings_created_idx ON recordings(created_at DESC);

CREATE TABLE IF NOT EXISTS media_files (
    id uuid PRIMARY KEY,
    recording_id uuid NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('ts','mp4')),
    status text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready','deleted','invalid')),
    path text NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    duration_ms bigint NOT NULL DEFAULT 0,
    codecs jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(recording_id, kind)
);

CREATE TABLE IF NOT EXISTS mp4_jobs (
    id uuid PRIMARY KEY,
    recording_id uuid NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('queued','running','ready','failed')),
    progress numeric(6,3) NOT NULL DEFAULT 0,
    output_path text NOT NULL DEFAULT '',
    error_text text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    ended_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS mp4_one_active_per_recording
    ON mp4_jobs(recording_id) WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS mp4_jobs_created_idx ON mp4_jobs(created_at DESC);

CREATE TABLE IF NOT EXISTS worker_commands (
    id bigserial PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('start_recording','stop_recording','generate_mp4','delete_file')),
    target_id uuid NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','claimed','done','failed')),
    worker_id text NOT NULL DEFAULT '',
    error_text text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    claimed_at timestamptz,
    completed_at timestamptz
);
CREATE INDEX IF NOT EXISTS worker_commands_pending_idx ON worker_commands(id) WHERE status='pending';

CREATE TABLE IF NOT EXISTS worker_heartbeats (
    worker_id text PRIMARY KEY,
    status text NOT NULL DEFAULT 'starting',
    active_recordings integer NOT NULL DEFAULT 0,
    active_conversions integer NOT NULL DEFAULT 0,
    disk_total_bytes bigint NOT NULL DEFAULT 0,
    disk_free_bytes bigint NOT NULL DEFAULT 0,
    version text NOT NULL DEFAULT '',
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS settings (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO settings(key, value) VALUES
    ('mp4_concurrency', '2'::jsonb),
    ('soft_free_percent', '10'::jsonb),
    ('soft_free_gib', '100'::jsonb),
    ('hard_free_percent', '5'::jsonb),
    ('hard_free_gib', '20'::jsonb)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS audit_events (
    id bigserial PRIMARY KEY,
    user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    action text NOT NULL,
    target_type text NOT NULL DEFAULT '',
    target_id text NOT NULL DEFAULT '',
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_events_created_idx ON audit_events(created_at DESC);
