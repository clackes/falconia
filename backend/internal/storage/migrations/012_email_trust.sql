-- 012: email addresses that have been proven, invitation links, and signing
-- out everywhere.
--
-- Access is granted by email in two places: artifact shares and team
-- invitations. Without proof that a person owns the address they signed up
-- with, anyone could register someone else's email before they do and inherit
-- everything shared with it. email_verified_at records that proof.
--
-- Accounts that already exist are marked verified: they have been using their
-- access, and cutting it off with an upgrade would break every team on the
-- instance. The rule applies to accounts created from now on.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;
UPDATE users SET email_verified_at = created_at WHERE email_verified_at IS NULL;

-- Sessions are stateless JWTs; this is the one piece of state that lets a
-- person invalidate all of them at once ("sign out everywhere").
ALTER TABLE users ADD COLUMN IF NOT EXISTS tokens_valid_after TIMESTAMPTZ;

-- Single-use verification links. Only the SHA-256 of the token is stored: the
-- link itself lives in the person's mailbox.
CREATE TABLE IF NOT EXISTS email_verifications (
    token_hash TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_email_verifications_user ON email_verifications(user_id);

-- An invitation to a team can now travel as a link. Holding the link is the
-- proof of owning the address it was sent to, so signing up through it
-- verifies the email, and works even when sign-up is otherwise closed.
ALTER TABLE org_invites ADD COLUMN IF NOT EXISTS token_hash TEXT;
ALTER TABLE org_invites ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_org_invites_token ON org_invites(token_hash) WHERE token_hash IS NOT NULL;

