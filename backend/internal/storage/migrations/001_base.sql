-- ============================================================================
-- 001 — Base schema.
--
-- Idempotent: on a database coming from the old db/init.sql every
-- CREATE ... IF NOT EXISTS is a no-op and the migration is simply recorded in
-- schema_migrations.
--
-- Entities:
--   users              — people.
--   artifacts          — the artifact: identity, metadata, visibility.
--   artifact_versions  — APPEND-ONLY (a trigger rejects UPDATEs).
--                        Every deploy, from web, CLI, CI or MCP, is an INSERT.
--   fork_relations     — parent → child genealogy.
--   pat_tokens         — API keys for agents / CLI / CI / MCP. Only hashes are stored.
--   orgs/org_members   — teams and their members.
--   artifact_shares    — one-to-one sharing by email.
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email          TEXT NOT NULL UNIQUE,
    display_name   TEXT NOT NULL,
    password_hash  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS artifacts (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id      UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    title          TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    is_public      BOOLEAN NOT NULL DEFAULT FALSE,
    icon           TEXT NOT NULL DEFAULT '',
    theme_color    TEXT NOT NULL DEFAULT '#0b0d12',
    logo_url       TEXT NOT NULL DEFAULT '',
    slug           TEXT,
    forkable       BOOLEAN NOT NULL DEFAULT TRUE,
    category       TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_artifacts_author   ON artifacts(author_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_category ON artifacts(category, updated_at DESC) WHERE category <> '';

CREATE TABLE IF NOT EXISTS artifact_versions (
    id              BIGSERIAL PRIMARY KEY,
    artifact_id     UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    version_number  INTEGER NOT NULL,
    code_payload    TEXT NOT NULL,
    code_hash       TEXT NOT NULL,
    schema_version  INTEGER NOT NULL DEFAULT 1,
    author_id       UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    commit_message  TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (artifact_id, version_number)
);
CREATE INDEX IF NOT EXISTS idx_versions_artifact_latest
    ON artifact_versions(artifact_id, version_number DESC);

-- Append-only: an artifact's history is never rewritten, only extended.
CREATE OR REPLACE FUNCTION artifact_versions_reject_update() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'artifact_versions is append-only; INSERT a new row instead';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_artifact_versions_no_update ON artifact_versions;
CREATE TRIGGER trg_artifact_versions_no_update
    BEFORE UPDATE ON artifact_versions
    FOR EACH ROW EXECUTE FUNCTION artifact_versions_reject_update();

CREATE TABLE IF NOT EXISTS fork_relations (
    child_artifact_id   UUID PRIMARY KEY REFERENCES artifacts(id) ON DELETE CASCADE,
    parent_artifact_id  UUID NOT NULL     REFERENCES artifacts(id) ON DELETE RESTRICT,
    forked_from_version INTEGER NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (child_artifact_id <> parent_artifact_id)
);
CREATE INDEX IF NOT EXISTS idx_fork_parent ON fork_relations(parent_artifact_id);

CREATE TABLE IF NOT EXISTS pat_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label        TEXT NOT NULL DEFAULT 'API key',
    prefix       TEXT NOT NULL UNIQUE,
    secret_hash  TEXT NOT NULL,
    scopes       TEXT[] NOT NULL DEFAULT ARRAY['artifacts:read','artifacts:write'],
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_pat_user ON pat_tokens(user_id);

CREATE TABLE IF NOT EXISTS orgs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS org_members (
    org_id     UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_org_members_user ON org_members(user_id);

CREATE TABLE IF NOT EXISTS org_invites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
    invited_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, email)
);
CREATE INDEX IF NOT EXISTS idx_org_invites_email ON org_invites(email);

ALTER TABLE artifacts
    ADD COLUMN IF NOT EXISTS owner_org_id UUID REFERENCES orgs(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS idx_artifacts_org ON artifacts(owner_org_id) WHERE owner_org_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS artifact_shares (
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'viewer' CHECK (role IN ('viewer','editor')),
    invited_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (artifact_id, email)
);
CREATE INDEX IF NOT EXISTS idx_shares_email ON artifact_shares(email);

CREATE TABLE IF NOT EXISTS artifact_likes (
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (artifact_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_likes_user ON artifact_likes(user_id);

CREATE TABLE IF NOT EXISTS artifact_comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body        TEXT NOT NULL,
    edited      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_comments_artifact ON artifact_comments(artifact_id, created_at DESC);
