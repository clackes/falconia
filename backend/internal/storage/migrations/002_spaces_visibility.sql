-- ============================================================================
-- 002 — Spaces and three-level visibility.
--
-- SPACE. Every artifact lives in a space, the unit of naming and ownership.
-- Two kinds:
--   personal — created automatically for every user, slug = the user's handle
--   org      — created with the team, slug = the team's slug
-- An artifact's URL is /s/<space>/<slug>, stable over time: versions change
-- behind the same address.
--
-- VISIBILITY (superseded by 008, which adds link sharing):
--   private — only the space owner
--   shared  — the space owner + people invited by email (artifact_shares)
--   org     — every member of the team that owns the space
-- The old is_public column is translated: TRUE → 'org', FALSE → 'private',
-- then dropped.
-- ============================================================================

CREATE TABLE IF NOT EXISTS spaces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug          TEXT NOT NULL,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('personal','org')),
    owner_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    org_id        UUID REFERENCES orgs(id)  ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Exactly one of the two owners must be set.
    CHECK ((kind = 'personal' AND owner_user_id IS NOT NULL AND org_id IS NULL)
        OR (kind = 'org'      AND org_id        IS NOT NULL AND owner_user_id IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_spaces_slug      ON spaces (LOWER(slug));
CREATE UNIQUE INDEX IF NOT EXISTS uniq_spaces_personal  ON spaces (owner_user_id) WHERE kind = 'personal';
CREATE UNIQUE INDEX IF NOT EXISTS uniq_spaces_org       ON spaces (org_id)        WHERE kind = 'org';

-- One personal space for every existing user. The slug starts from the local
-- part of the email, cleaned up; a counter is appended on collision.
DO $$
DECLARE
    u          RECORD;
    base_slug  TEXT;
    try_slug   TEXT;
    n          INTEGER;
BEGIN
    FOR u IN SELECT id, email, display_name FROM users LOOP
        CONTINUE WHEN EXISTS (SELECT 1 FROM spaces WHERE owner_user_id = u.id);
        base_slug := regexp_replace(LOWER(split_part(u.email, '@', 1)), '[^a-z0-9-]+', '-', 'g');
        base_slug := trim(BOTH '-' FROM base_slug);
        IF base_slug = '' THEN base_slug := 'user'; END IF;
        try_slug := base_slug;
        n := 1;
        WHILE EXISTS (SELECT 1 FROM spaces WHERE LOWER(slug) = try_slug) LOOP
            n := n + 1;
            try_slug := base_slug || '-' || n;
        END LOOP;
        INSERT INTO spaces (slug, name, kind, owner_user_id)
        VALUES (try_slug, u.display_name, 'personal', u.id);
    END LOOP;
END $$;

-- One space for every existing team.
INSERT INTO spaces (slug, name, kind, org_id)
SELECT o.slug, o.name, 'org', o.id
FROM orgs o
WHERE NOT EXISTS (SELECT 1 FROM spaces s WHERE s.org_id = o.id)
  AND NOT EXISTS (SELECT 1 FROM spaces s WHERE LOWER(s.slug) = LOWER(o.slug));

ALTER TABLE artifacts
    ADD COLUMN IF NOT EXISTS space_id   UUID REFERENCES spaces(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private';

-- Existing artifacts go to the space of the team that owns them, or else to
-- their author's personal space.
UPDATE artifacts a
SET space_id = COALESCE(
        (SELECT s.id FROM spaces s WHERE s.org_id        = a.owner_org_id),
        (SELECT s.id FROM spaces s WHERE s.owner_user_id = a.author_id))
WHERE a.space_id IS NULL;

-- is_public only exists on databases that come from the old schema.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'artifacts' AND column_name = 'is_public') THEN
        UPDATE artifacts SET visibility = CASE WHEN is_public THEN 'org' ELSE 'private' END;
        ALTER TABLE artifacts DROP COLUMN is_public;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'artifacts_visibility_chk') THEN
        ALTER TABLE artifacts
            ADD CONSTRAINT artifacts_visibility_chk CHECK (visibility IN ('private','shared','org'));
    END IF;
END $$;

-- Every artifact must live in a space: at this point the backfill is complete.
ALTER TABLE artifacts ALTER COLUMN space_id SET NOT NULL;

-- The slug is unique within the space, not globally: two teams can both have
-- a "roadmap" artifact without coordinating.
DROP INDEX IF EXISTS uniq_artifacts_slug;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_artifacts_space_slug
    ON artifacts (space_id, LOWER(slug))
    WHERE slug IS NOT NULL AND slug <> '';

CREATE INDEX IF NOT EXISTS idx_artifacts_space
    ON artifacts (space_id, updated_at DESC);
