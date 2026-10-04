-- ============================================================================
-- 004 — Review loop: anchored, resolvable threads.
--
-- This is the heart of the product. A comment is not a message at the bottom
-- of a page: it is a thread attached to a SPOT of the artifact, like in Google
-- Docs, and it has a status. The agent reads open threads, publishes the next
-- version and resolves them.
--
-- ANCHORING. The artifact runs in an opaque-origin iframe: the frontend cannot
-- read its DOM. The anchor is therefore computed INSIDE the iframe by the shim
-- and sent to the parent over a MessageChannel. The format is deliberately
-- redundant because the DOM changes from one version to the next:
--
--   {"selector": "main > section:nth-of-type(2) > h2",
--    "quote":    "Q3 revenue",
--    "x": 0.42, "y": 0.18}
--
-- On reopening, the shim tries the selector, then the quoted text, then falls
-- back to relative coordinates. An anchor that does not resolve does not
-- disappear: the thread becomes "detached" and shows in the side list.
--
-- VERSION. A thread remembers which version it was born on, so the agent knows
-- whether a comment refers to code it has already replaced.
-- ============================================================================

CREATE TABLE IF NOT EXISTS comment_threads (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id    UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL DEFAULT 1,
    -- The bundle file the thread refers to ('index.html' by default).
    page_path      TEXT NOT NULL DEFAULT 'index.html',
    anchor         JSONB,
    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
    created_by     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resolved_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_threads_artifact ON comment_threads(artifact_id, status, created_at DESC);

ALTER TABLE artifact_comments
    ADD COLUMN IF NOT EXISTS thread_id UUID REFERENCES comment_threads(id) ON DELETE CASCADE,
    -- Who wrote it: a person, or an agent using an API key.
    ADD COLUMN IF NOT EXISTS via       TEXT NOT NULL DEFAULT 'web';
CREATE INDEX IF NOT EXISTS idx_comments_thread ON artifact_comments(thread_id, created_at);

-- Pre-existing flat comments become single-message threads without an
-- anchor: they stay readable and resolvable like all the others.
DO $$
DECLARE c RECORD; tid UUID;
BEGIN
    FOR c IN SELECT * FROM artifact_comments WHERE thread_id IS NULL LOOP
        INSERT INTO comment_threads (artifact_id, created_by, created_at, updated_at)
        VALUES (c.artifact_id, c.user_id, c.created_at, c.updated_at)
        RETURNING id INTO tid;
        UPDATE artifact_comments SET thread_id = tid WHERE id = c.id;
    END LOOP;
END $$;

ALTER TABLE artifact_comments ALTER COLUMN thread_id SET NOT NULL;
