CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin','user')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash  TEXT PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE runs (
    run_id         TEXT PRIMARY KEY,
    status         TEXT NOT NULL DEFAULT 'queued',
    creator_type   TEXT NOT NULL,
    creator_sub    TEXT,
    creator_email  TEXT,
    source         TEXT,
    request        JSONB NOT NULL DEFAULT '{}',
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    error          TEXT,
    report         JSONB,
    risk           JSONB NOT NULL DEFAULT '{}',
    key_usage      JSONB NOT NULL DEFAULT '{}',
    estimated_cost DOUBLE PRECISION NOT NULL DEFAULT 0
);
CREATE INDEX idx_runs_created ON runs(created_at DESC);

CREATE TABLE run_events (
    id        BIGSERIAL PRIMARY KEY,
    run_id    TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
    seq       BIGINT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
    stage     TEXT NOT NULL,
    message   TEXT NOT NULL,
    data      JSONB
);
CREATE INDEX idx_run_events_run_seq ON run_events(run_id, seq);

CREATE TABLE audit_events (
    id         BIGSERIAL PRIMARY KEY,
    timestamp  TIMESTAMPTZ NOT NULL DEFAULT now(),
    run_id     TEXT,
    actor_type TEXT NOT NULL,
    actor_sub  TEXT,
    action     TEXT NOT NULL,
    result     TEXT NOT NULL,
    ip_hash    TEXT,
    ua_hash    TEXT,
    detail     TEXT
);
CREATE INDEX idx_audit_ts ON audit_events(timestamp DESC);
