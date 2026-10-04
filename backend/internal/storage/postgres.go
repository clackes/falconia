package storage

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	ErrConflict  = errors.New("conflict")
)

type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

// isUniqueViolation tells "this slug is already taken" apart from a real
// error. The loops that probe for a free slug depend on it.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ---------------- Users ----------------

type User struct {
	ID           uuid.UUID
	Email        string
	DisplayName  string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

const selectUserCols = `id, email, display_name, COALESCE(password_hash,''), is_admin, created_at`

func scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// CreateUser registers a person and sets up their personal space right away.
//
// The first user of the instance becomes its administrator: that is whoever
// ran the setup on the server, and without this rule nobody could ever become
// one. The check and the insert share a transaction, so two simultaneous
// sign-ups on an empty instance do not accidentally produce two admins.
func (p *Postgres) CreateUser(ctx context.Context, email, displayName, passwordHash string) (*User, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var anyUser bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&anyUser); err != nil {
		return nil, err
	}
	u := &User{
		ID: uuid.New(), Email: email, DisplayName: displayName,
		PasswordHash: passwordHash, IsAdmin: !anyUser, CreatedAt: time.Now(),
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO users (id, email, display_name, password_hash, is_admin, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, u.Email, u.DisplayName, u.PasswordHash, u.IsAdmin, u.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if _, err := p.EnsurePersonalSpace(ctx, u.ID, u.Email, u.DisplayName); err != nil {
		return nil, err
	}
	return u, nil
}

func (p *Postgres) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(p.pool.QueryRow(ctx, `SELECT `+selectUserCols+` FROM users WHERE email=$1`, email))
}

func (p *Postgres) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanUser(p.pool.QueryRow(ctx, `SELECT `+selectUserCols+` FROM users WHERE id=$1`, id))
}

// ListUsers feeds the instance admin panel.
func (p *Postgres) ListUsers(ctx context.Context, limit int) ([]User, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := p.pool.Query(ctx, `SELECT `+selectUserCols+` FROM users ORDER BY created_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.PasswordHash = ""
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) SetAdmin(ctx context.Context, id uuid.UUID, admin bool) error {
	tag, err := p.pool.Exec(ctx, `UPDATE users SET is_admin=$2 WHERE id=$1`, id, admin)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------- API keys ----------------

// Every recognized scope. A key cannot hold anything outside this list, so a
// typo never silently produces a key that authorizes nothing.
const (
	ScopeArtifactsRead  = "artifacts:read"
	ScopeArtifactsWrite = "artifacts:write"
	ScopeCommentsRead   = "comments:read"
	ScopeCommentsWrite  = "comments:write"
)

var AllScopes = []string{ScopeArtifactsRead, ScopeArtifactsWrite, ScopeCommentsRead, ScopeCommentsWrite}

// APIKey is a non-interactive credential: an agent, the CLI, CI or an MCP
// client.
//
// It is never equivalent to the account that issued it. A key ends up in an
// agent's config file or a pipeline's secrets, where more people see it than
// ever see its owner's password; that is why it cannot delete artifacts or
// issue other keys, and why the check lives in the middleware rather than in
// the caller's good will.
type APIKey struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Label      string
	Prefix     string
	Kind       string // "cli" | "ci" | "mcp"
	Scopes     []string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

func (k *APIKey) HasScope(s string) bool {
	for _, have := range k.Scopes {
		if have == s {
			return true
		}
	}
	return false
}

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// TokenPrefix starts every key Falconia issues. A distinctive prefix lets
// secret scanners (GitHub, GitGuardian, trufflehog) recognize a leaked key.
const TokenPrefix = "falconia_pat_"

// legacyTokenPrefix is what keys looked like before the project was renamed.
// They keep working: revoking every agent's configuration overnight would be
// the opposite of painless.
const legacyTokenPrefix = "sfa_pat_"

// IsAPIKey reports whether a bearer token looks like an API key rather than a
// browser session.
func IsAPIKey(tok string) bool {
	return strings.HasPrefix(tok, TokenPrefix) || strings.HasPrefix(tok, legacyTokenPrefix)
}

// randomToken produces (prefix, secret, "falconia_pat_<prefix>_<secret>").
//
// The prefix is stored in clear and acts as an index: without it, verifying a
// token would take one bcrypt per existing key.
func randomToken() (string, string, string, error) {
	prefixBytes := make([]byte, 5)
	secretBytes := make([]byte, 24)
	if _, err := rand.Read(prefixBytes); err != nil {
		return "", "", "", err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", "", err
	}
	prefix := strings.ToLower(base32NoPad.EncodeToString(prefixBytes))
	secret := strings.ToLower(base32NoPad.EncodeToString(secretBytes))
	return prefix, secret, TokenPrefix + prefix + "_" + secret, nil
}

func sanitizeScopes(in []string) []string {
	if len(in) == 0 {
		return []string{ScopeArtifactsRead, ScopeArtifactsWrite, ScopeCommentsRead, ScopeCommentsWrite}
	}
	out := []string{}
	for _, s := range in {
		for _, ok := range AllScopes {
			if s == ok {
				out = append(out, s)
				break
			}
		}
	}
	if len(out) == 0 {
		return []string{ScopeArtifactsRead}
	}
	return out
}

// CreateAPIKey issues a key. The clear value only exists here: it is returned
// once and never again, because the database only holds its bcrypt hash.
func (p *Postgres) CreateAPIKey(ctx context.Context, userID uuid.UUID, label, kind string, scopes []string, ttl time.Duration) (*APIKey, string, error) {
	if label == "" {
		label = "API key"
	}
	switch kind {
	case "cli", "ci", "mcp":
	default:
		kind = "cli"
	}
	prefix, secret, full, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}
	k := &APIKey{
		ID: uuid.New(), UserID: userID, Label: label, Prefix: prefix, Kind: kind,
		Scopes: sanitizeScopes(scopes), CreatedAt: time.Now(),
	}
	if ttl > 0 {
		exp := time.Now().Add(ttl)
		k.ExpiresAt = &exp
	}
	_, err = p.pool.Exec(ctx,
		`INSERT INTO pat_tokens (id, user_id, label, prefix, secret_hash, scopes, kind, expires_at, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		k.ID, k.UserID, k.Label, k.Prefix, string(hash), k.Scopes, k.Kind, k.ExpiresAt, k.CreatedAt)
	if err != nil {
		return nil, "", err
	}
	return k, full, nil
}

const selectKeyCols = `id, user_id, label, prefix, kind, scopes, created_at, expires_at, last_used_at`

func (p *Postgres) ListAPIKeys(ctx context.Context, userID uuid.UUID) ([]APIKey, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+selectKeyCols+` FROM pat_tokens WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Label, &k.Prefix, &k.Kind, &k.Scopes,
			&k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (p *Postgres) RevokeAPIKey(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM pat_tokens WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LookupAPIKey verifies a token and returns its owner.
//
// An expired key is rejected here rather than at the point of use, so there is
// no code path that can accept one by accident.
func (p *Postgres) LookupAPIKey(ctx context.Context, full string) (*User, *APIKey, error) {
	var rest string
	switch {
	case strings.HasPrefix(full, TokenPrefix):
		rest = strings.TrimPrefix(full, TokenPrefix)
	case strings.HasPrefix(full, legacyTokenPrefix):
		rest = strings.TrimPrefix(full, legacyTokenPrefix)
	default:
		return nil, nil, ErrForbidden
	}
	prefix, secret, ok := strings.Cut(rest, "_")
	if !ok || prefix == "" || secret == "" {
		return nil, nil, ErrForbidden
	}

	var k APIKey
	var hash string
	err := p.pool.QueryRow(ctx,
		`SELECT id, user_id, label, prefix, kind, scopes, created_at, expires_at, last_used_at, secret_hash
		   FROM pat_tokens WHERE prefix=$1`, prefix,
	).Scan(&k.ID, &k.UserID, &k.Label, &k.Prefix, &k.Kind, &k.Scopes,
		&k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrForbidden
	}
	if err != nil {
		return nil, nil, err
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return nil, nil, ErrForbidden
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) != nil {
		return nil, nil, ErrForbidden
	}
	u, err := p.GetUser(ctx, k.UserID)
	if err != nil {
		return nil, nil, err
	}
	p.touchAPIKey(k.ID)
	return u, &k, nil
}

// touchAPIKey updates last_used_at off the request path: it is a convenience
// for the keys page and not worth a synchronous write on every authenticated
// call.
func (p *Postgres) touchAPIKey(id uuid.UUID) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = p.pool.Exec(ctx, `UPDATE pat_tokens SET last_used_at=NOW() WHERE id=$1`, id)
	}()
}

// ---------------- Helper ----------------

// normalizeSlug reduces any string to [a-z0-9-], the alphabet of artifact and
// space URLs.
func normalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
