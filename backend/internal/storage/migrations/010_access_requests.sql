-- ============================================================================
-- 010 — Requests for edit access.
--
-- A viewer asks, the artifact's owner approves or declines. Approving is just
-- an editor share by email, so the request table only keeps the conversation:
-- who asked, why, and what was decided.
-- ============================================================================

CREATE TABLE IF NOT EXISTS access_requests (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id)     ON DELETE CASCADE,
    message     TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','declined')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at  TIMESTAMPTZ,
    decided_by  UUID REFERENCES users(id) ON DELETE SET NULL
);

-- One open request per person and artifact: asking again updates it.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_access_requests_pending
    ON access_requests (artifact_id, user_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_access_requests_artifact
    ON access_requests (artifact_id, created_at DESC);
