-- ============================================================================
-- 008 — Falconia: link sharing, team role, agent provenance.
--
-- GENERAL ACCESS. Sharing now works like a Google Doc: a list of people with a
-- role each, plus one "general access" setting.
--
--   private — restricted: only the owner and the people added by email
--   org     — everyone in the team that owns the space, with team_role
--   link    — anyone holding the secret link can view, even without an account
--
-- The old 'shared' value behaved exactly like 'private' (email shares always
-- applied), so it is folded into it.
--
-- LINK TOKENS. A link is /p/<link_token>, never /s/<space>/<slug>: slugs are
-- readable and guessable, a 128-bit random token is not. The token survives
-- switching general access off and on again, like a Google Docs link does, and
-- can be rotated by the owner when it leaks. Signed-in people who open a link
-- are remembered in artifact_link_visitors, so the artifact shows up in their
-- dashboard and they can comment on it; rotating the token forgets them.
--
-- AGENT PROVENANCE. Every version, comment and event records which AI agent
-- produced it (claude-code, codex, gemini-cli, ...). Empty means a human in the
-- browser. It is a display hint, never an authorization input.
-- ============================================================================

ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_visibility_chk;
UPDATE artifacts SET visibility = 'private' WHERE visibility = 'shared';
ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_visibility_chk CHECK (visibility IN ('private','org','link'));

ALTER TABLE artifacts
    ADD COLUMN IF NOT EXISTS link_token TEXT,
    ADD COLUMN IF NOT EXISTS team_role  TEXT NOT NULL DEFAULT 'editor';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'artifacts_team_role_chk') THEN
        ALTER TABLE artifacts
            ADD CONSTRAINT artifacts_team_role_chk CHECK (team_role IN ('viewer','editor'));
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uniq_artifacts_link_token
    ON artifacts (link_token) WHERE link_token IS NOT NULL;

CREATE TABLE IF NOT EXISTS artifact_link_visitors (
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id)     ON DELETE CASCADE,
    first_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (artifact_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_link_visitors_user ON artifact_link_visitors(user_id, last_seen DESC);

-- Adding a column with a default is not an UPDATE, so the append-only trigger
-- on artifact_versions does not fire.
ALTER TABLE artifact_versions ADD COLUMN IF NOT EXISTS agent TEXT NOT NULL DEFAULT '';
ALTER TABLE artifact_comments ADD COLUMN IF NOT EXISTS agent TEXT NOT NULL DEFAULT '';
ALTER TABLE artifact_events   ADD COLUMN IF NOT EXISTS agent TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_versions_agent
    ON artifact_versions (agent) WHERE agent <> '';

-- Keys get a new prefix (falconia_pat_) but old sfa_pat_ keys keep working:
-- nothing to migrate in the table, the prefix is only in the token string.
