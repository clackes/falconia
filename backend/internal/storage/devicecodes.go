package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeviceCode is a pending login for the CLI.
//
// `falconia login` cannot host a browser and must not ask for a password: it
// would end up in the shell history and, worse, in the context of the agent
// that ran the command. The device-code flow sidesteps that: the CLI shows a
// short code, the person confirms it in the browser where they are already
// signed in, and the CLI collects the token by polling.
type DeviceCode struct {
	ID         uuid.UUID
	UserCode   string
	Label      string
	ApprovedBy *uuid.UUID
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// userCodeAlphabet leaves out 0/O/1/I/L/U: someone reading a code aloud or
// copying it from a screen must not be able to get it wrong, and dropping "U"
// avoids accidentally spelling unpleasant words.
const userCodeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

func newUserCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 0, 9)
	for i, b := range buf {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, userCodeAlphabet[int(b)%len(userCodeAlphabet)])
	}
	return string(out), nil
}

func hashDeviceCode(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// StartDeviceLogin generates the pair (secret device code, readable user
// code). Only the hash of the former is stored: someone reading the database
// cannot steal a login waiting for approval.
func (p *Postgres) StartDeviceLogin(ctx context.Context, label string, ttl time.Duration) (deviceCode string, dc *DeviceCode, err error) {
	if label == "" {
		label = "falconia cli"
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	deviceCode = strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))

	for attempt := 0; attempt < 10; attempt++ {
		userCode, err := newUserCode()
		if err != nil {
			return "", nil, err
		}
		d := &DeviceCode{ID: uuid.New(), UserCode: userCode, Label: label,
			ExpiresAt: time.Now().Add(ttl), CreatedAt: time.Now()}
		_, err = p.pool.Exec(ctx, `
			INSERT INTO device_codes (id, device_code_hash, user_code, label, expires_at, created_at)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			d.ID, hashDeviceCode(deviceCode), d.UserCode, d.Label, d.ExpiresAt, d.CreatedAt)
		if err == nil {
			return deviceCode, d, nil
		}
		if !isUniqueViolation(err) {
			return "", nil, err
		}
		// User code collision: try another one.
	}
	return "", nil, errors.New("could not allocate a user code")
}

// FindDeviceByUserCode resolves the code the person types in the browser.
func (p *Postgres) FindDeviceByUserCode(ctx context.Context, userCode string) (*DeviceCode, error) {
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	// People pasting the code often drop or add the dash: put it back before
	// looking it up instead of failing the login.
	compact := strings.ReplaceAll(userCode, "-", "")
	if len(compact) == 8 {
		userCode = compact[:4] + "-" + compact[4:]
	}
	d := &DeviceCode{}
	err := p.pool.QueryRow(ctx, `
		SELECT id, user_code, label, approved_by, expires_at, created_at
		  FROM device_codes WHERE user_code=$1 AND expires_at > NOW()`, userCode,
	).Scan(&d.ID, &d.UserCode, &d.Label, &d.ApprovedBy, &d.ExpiresAt, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

// ApproveDeviceLogin confirms the login and issues the key the CLI will collect.
func (p *Postgres) ApproveDeviceLogin(ctx context.Context, userCode string, userID uuid.UUID) error {
	d, err := p.FindDeviceByUserCode(ctx, userCode)
	if err != nil {
		return err
	}
	if d.ApprovedBy != nil {
		return ErrConflict
	}
	_, token, err := p.CreateAPIKey(ctx, userID, d.Label, "cli", AllScopes, 365*24*time.Hour)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE device_codes SET approved_by=$2, approved_at=NOW(), issued_token=$3
		 WHERE id=$1 AND approved_by IS NULL`, d.ID, userID, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// PollDeviceLogin is what the CLI calls in a loop.
//
// It returns ("", nil) until someone approves. Once the token exists it is
// handed over and the row deleted in the same statement: the token is given
// out exactly once, and whoever intercepted the device code a moment later
// would find nothing left to collect.
func (p *Postgres) PollDeviceLogin(ctx context.Context, deviceCode string) (string, *User, error) {
	var token string
	var userID uuid.UUID
	err := p.pool.QueryRow(ctx, `
		DELETE FROM device_codes
		 WHERE device_code_hash = $1 AND issued_token IS NOT NULL AND expires_at > NOW()
		 RETURNING issued_token, approved_by`, hashDeviceCode(deviceCode),
	).Scan(&token, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either not approved yet, or expired: a second look tells them apart,
		// so the CLI can say "expired" instead of waiting forever.
		var exists bool
		if err := p.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM device_codes WHERE device_code_hash=$1 AND expires_at > NOW())`,
			hashDeviceCode(deviceCode)).Scan(&exists); err != nil {
			return "", nil, err
		}
		if !exists {
			return "", nil, ErrNotFound
		}
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	u, err := p.GetUser(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// PurgeExpiredDeviceCodes keeps the table clean.
func (p *Postgres) PurgeExpiredDeviceCodes(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM device_codes WHERE expires_at < NOW()`)
	return err
}
