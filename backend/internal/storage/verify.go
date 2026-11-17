package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tokens sent by email or handed out as links follow the usual rules for
// such secrets: random (256 bits), single-use, short-lived, and stored only
// as a hash, so a leaked database holds nothing that can be clicked.

func newSecretToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(t)))
	return hex.EncodeToString(sum[:])
}

// CreateEmailVerification issues a verification link token for the user's
// current address. Older links for the same user are dropped: only the most
// recent email should work.
func (p *Postgres) CreateEmailVerification(ctx context.Context, u *User, ttl time.Duration) (string, error) {
	token, hash, err := newSecretToken()
	if err != nil {
		return "", err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE user_id=$1`, u.ID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO email_verifications (token_hash, user_id, email, expires_at) VALUES ($1,$2,$3,$4)`,
		hash, u.ID, strings.ToLower(u.Email), time.Now().Add(ttl)); err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

// ConsumeEmailVerification marks the address verified and spends the token.
// The token is bound to the address it was sent to: if the account's email
// changed since, the link proves nothing about the new one.
func (p *Postgres) ConsumeEmailVerification(ctx context.Context, token string) (*User, error) {
	var userID uuid.UUID
	var email string
	err := p.pool.QueryRow(ctx, `
		DELETE FROM email_verifications
		 WHERE token_hash=$1 AND expires_at > NOW()
		 RETURNING user_id, email`, hashToken(token)).Scan(&userID, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE users SET email_verified_at = COALESCE(email_verified_at, NOW())
		 WHERE id=$1 AND LOWER(email)=$2`, userID, email)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.GetUser(ctx, userID)
}

// Invitation describes a pending team invitation found by its link.
type Invitation struct {
	Email   string
	Role    string
	OrgName string
}

// LookupInvitation resolves an invitation link.
func (p *Postgres) LookupInvitation(ctx context.Context, token string) (*Invitation, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrNotFound
	}
	inv := &Invitation{}
	err := p.pool.QueryRow(ctx, `
		SELECT i.email, i.role, o.name FROM org_invites i JOIN orgs o ON o.id = i.org_id
		 WHERE i.token_hash=$1 AND (i.expires_at IS NULL OR i.expires_at > NOW())`, hashToken(token),
	).Scan(&inv.Email, &inv.Role, &inv.OrgName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return inv, err
}

// PurgeExpiredTokens removes verification links and invitation links that can
// no longer be used. Expired invitations themselves stay: the admin still sees
// them as pending and can send a fresh link.
func (p *Postgres) PurgeExpiredTokens(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM email_verifications WHERE expires_at < NOW()`); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx,
		`UPDATE org_invites SET token_hash=NULL WHERE token_hash IS NOT NULL AND expires_at < NOW()`)
	return err
}
