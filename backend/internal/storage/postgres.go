package storage

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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

// InvalidError is input the caller can fix: an empty comment, a malformed
// email. It carries a message meant for the person, and the HTTP layer turns
// it into a 400. Anything that is not an InvalidError is a server problem and
// its text never reaches the client, because database errors name tables,
// constraints and sometimes values.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

func invalid(msg string) error { return &InvalidError{Msg: msg} }

// IsInvalid reports whether err is an InvalidError, and returns its message.
func IsInvalid(err error) (string, bool) {
	var ie *InvalidError
	if errors.As(err, &ie) {
		return ie.Msg, true
	}
	return "", false
}

type Postgres struct {
	pool *pgxpool.Pool

	// TrustUnverifiedEmail lets an unverified address unlock what was shared
	// with it. It is set when the instance cannot verify addresses at all (no
	// mail server and verification not required): refusing every email-based
	// grant there would make sharing by email useless.
	TrustUnverifiedEmail bool
}

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
	// EmailVerifiedAt is set once the person proved they own Email.
	EmailVerifiedAt *time.Time
	// TokensValidAfter rejects every session token issued before it.
	TokensValidAfter *time.Time
}

const selectUserCols = `id, email, display_name, COALESCE(password_hash,''), is_admin, created_at,
	email_verified_at, tokens_valid_after`

func scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt,
		&u.EmailVerifiedAt, &u.TokensValidAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// signupLockID serializes sign-ups. Any constant works as long as no other
// advisory lock in the code uses it.
const signupLockID = 0x53464101

// NewUser describes an account to create.
type NewUser struct {
	Email, DisplayName, PasswordHash string
	// OnlyIfEmpty is for instances with sign-up closed: the first account may
	// still be created, and the lock guarantees it is the only one.
	OnlyIfEmpty bool
	// Verified marks the email as proven at creation, for sign-ups that came
	// through an invitation link.
	Verified bool
}

// CreateUser registers a person and sets up their personal space right away.
//
// The first user of the instance becomes its administrator: that is whoever
// ran the setup on the server, and without this rule nobody could ever become
// one. A transaction alone does not make that safe: under READ COMMITTED two
// simultaneous sign-ups on an empty instance would both see no users and both
// become admin. The transaction-scoped advisory lock makes the check and the
// insert one step. The first user's email counts as verified too: there is
// nobody else yet to prove anything to, and no mail server may exist.
func (p *Postgres) CreateUser(ctx context.Context, in NewUser) (*User, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, signupLockID); err != nil {
		return nil, err
	}
	var anyUser bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&anyUser); err != nil {
		return nil, err
	}
	if anyUser && in.OnlyIfEmpty {
		return nil, ErrForbidden
	}
	now := time.Now()
	u := &User{
		ID: uuid.New(), Email: in.Email, DisplayName: in.DisplayName,
		PasswordHash: in.PasswordHash, IsAdmin: !anyUser, CreatedAt: now,
	}
	if in.Verified || !anyUser {
		u.EmailVerifiedAt = &now
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO users (id, email, display_name, password_hash, is_admin, created_at, email_verified_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		u.ID, u.Email, u.DisplayName, u.PasswordHash, u.IsAdmin, u.CreatedAt, u.EmailVerifiedAt); err != nil {
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

// AccessEmail is the address that may unlock email-based grants for u: the
// email itself once verified (or when the instance trusts unverified ones),
// "" otherwise. Every access check that takes an email must be given this,
// never User.Email directly.
func (p *Postgres) AccessEmail(u *User) string {
	if u == nil {
		return ""
	}
	if u.EmailVerifiedAt != nil || p.TrustUnverifiedEmail {
		return strings.ToLower(u.Email)
	}
	return ""
}

// RevokeSessions invalidates every session token issued so far.
func (p *Postgres) RevokeSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := p.pool.Exec(ctx, `UPDATE users SET tokens_valid_after = NOW() WHERE id=$1`, userID)
	return err
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
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt,
			&u.EmailVerifiedAt, &u.TokensValidAfter); err != nil {
			return nil, err
		}
		u.PasswordHash = ""
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkEmailVerified is the administrator vouching for an address, for
// instances without a mail server.
func (p *Postgres) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE users SET email_verified_at = COALESCE(email_verified_at, NOW()) WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
	return createAPIKey(ctx, p.pool, userID, label, kind, scopes, ttl)
}

// execer is what pgxpool.Pool and pgx.Tx have in common, so a write can run
// either on its own or inside a caller's transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// maxKeyTTL bounds a key's lifetime. Beyond it time.Duration would overflow
// and the "expiring" key would never expire.
const maxKeyTTL = 10 * 365 * 24 * time.Hour

func createAPIKey(ctx context.Context, db execer, userID uuid.UUID, label, kind string, scopes []string, ttl time.Duration) (*APIKey, string, error) {
	label = truncate(strings.TrimSpace(label), 200)
	if ttl > maxKeyTTL || ttl < 0 {
		ttl = maxKeyTTL
	}
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
	_, err = db.Exec(ctx,
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

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence:
// Postgres rejects invalid UTF-8, so a blind byte slice would turn a long
// label into a 500.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Ping checks that the database answers, for the health endpoint.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
