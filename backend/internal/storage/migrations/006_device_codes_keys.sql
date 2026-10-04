-- ============================================================================
-- 006 — CLI login, CI keys, administrators.
--
-- DEVICE CODE. `falconia login` cannot host a browser nor ask for the password
-- (it would end up in the shell history and the agent's logs). So it follows
-- the device-code flow: the CLI gets a pair (secret device_code, readable
-- user_code), shows the person a URL to open, and polls meanwhile. The
-- device_code is stored only as a hash: whoever reads the database cannot steal
-- a pending login.
--
-- KEYS. API keys already existed for MCP; here they gain an expiry and the
-- distinction between interactive and CI use. A key can never create other
-- keys nor delete an artifact (Glance's rule): a key ends up in an agent's
-- config or a pipeline's secrets, where far more people see it than see its
-- owner's account.
-- ============================================================================

CREATE TABLE IF NOT EXISTS device_codes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_code_hash TEXT NOT NULL UNIQUE,
    user_code        TEXT NOT NULL UNIQUE,
    label            TEXT NOT NULL DEFAULT 'sfa cli',
    approved_by      UUID REFERENCES users(id) ON DELETE CASCADE,
    approved_at      TIMESTAMPTZ,
    -- The issued token is handed to the CLI exactly once and then cleared.
    issued_token     TEXT,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_device_codes_expiry ON device_codes(expires_at);

ALTER TABLE pat_tokens
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS kind       TEXT NOT NULL DEFAULT 'cli';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'pat_tokens_kind_chk') THEN
        ALTER TABLE pat_tokens
            ADD CONSTRAINT pat_tokens_kind_chk CHECK (kind IN ('cli','ci','mcp'));
    END IF;
END $$;

-- Scopes changed name together with the product vocabulary: "datafact" was
-- the old name. Without this conversion keys already handed out would stay
-- valid but authorize nothing, and their users would see an empty tool list
-- without understanding why.
UPDATE pat_tokens
SET scopes = ARRAY(
    SELECT DISTINCT CASE s
        WHEN 'datafacts:read'  THEN 'artifacts:read'
        WHEN 'datafacts:write' THEN 'artifacts:write'
        ELSE s
    END
    FROM UNNEST(scopes) AS s
)
WHERE scopes && ARRAY['datafacts:read','datafacts:write'];

-- Keys created before comments existed had no way to declare a scope that did
-- not exist yet: whoever could write artifacts can also reply.
UPDATE pat_tokens
SET scopes = scopes || ARRAY['comments:read','comments:write']
WHERE 'artifacts:write' = ANY(scopes) AND NOT ('comments:read' = ANY(scopes));

-- The first registered user becomes the instance administrator: they ran the
-- setup on the server, and without this nobody could ever become one.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE users SET is_admin = TRUE
WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_admin);
