-- ============================================================================
-- 005 — Audit trail.
--
-- "Who opened what, and when". It is the answer to whoever needs to know how
-- far a document travelled before someone noticed it had a wrong number in it.
--
-- Every event is immutable. IP addresses are never stored in clear: a truncated
-- HMAC is kept, enough to tell two visitors apart on the same day and useless
-- to identify them.
-- ============================================================================

CREATE TABLE IF NOT EXISTS artifact_events (
    id             BIGSERIAL PRIMARY KEY,
    artifact_id    UUID REFERENCES artifacts(id) ON DELETE CASCADE,
    actor_id       UUID REFERENCES users(id) ON DELETE SET NULL,
    kind           TEXT NOT NULL,
    version_number INTEGER,
    -- 'web' | 'link' | 'cli' | 'ci' | 'mcp' | 'api'
    via            TEXT NOT NULL DEFAULT 'web',
    meta           JSONB NOT NULL DEFAULT '{}'::jsonb,
    visitor_hash   TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_events_artifact ON artifact_events(artifact_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_kind     ON artifact_events(kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_actor    ON artifact_events(actor_id, created_at DESC);

-- A view of an artifact counts once per visitor per hour: without this, an
-- artifact that reloads itself inflates its own counter.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_event_view_hourly
-- date_trunc on timestamptz depends on the session TimeZone and is only
-- STABLE: the explicit conversion to UTC makes it IMMUTABLE, hence indexable.
    ON artifact_events (artifact_id, visitor_hash, date_trunc('hour', created_at AT TIME ZONE 'UTC'))
    WHERE kind = 'view';
