package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------- Requests for edit access ----------------

// AccessRequest is a viewer asking the owner for editor access.
type AccessRequest struct {
	ID         uuid.UUID
	ArtifactID uuid.UUID
	UserID     uuid.UUID
	Email      string
	Name       string
	Message    string
	Status     string // "pending" | "approved" | "declined"
	CreatedAt  time.Time
	DecidedAt  *time.Time

	// Filled by ListAccessInbox, so the owner can tell which artifact it is.
	ArtifactTitle string
	ArtifactPath  string
}

const accessRequestCols = `r.id, r.artifact_id, r.user_id, u.email, u.display_name,
	r.message, r.status, r.created_at, r.decided_at`

func scanAccessRequest(row pgx.Row, extra ...any) (*AccessRequest, error) {
	var a AccessRequest
	dest := append([]any{&a.ID, &a.ArtifactID, &a.UserID, &a.Email, &a.Name,
		&a.Message, &a.Status, &a.CreatedAt, &a.DecidedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// RequestAccess opens a request, or refreshes the one already pending.
func (p *Postgres) RequestAccess(ctx context.Context, artifactID, userID uuid.UUID, message string) (*AccessRequest, error) {
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		message = message[:1000]
	}
	var id uuid.UUID
	err := p.pool.QueryRow(ctx, `
		INSERT INTO access_requests (artifact_id, user_id, message)
		VALUES ($1,$2,$3)
		ON CONFLICT (artifact_id, user_id) WHERE status = 'pending'
		DO UPDATE SET message = EXCLUDED.message, created_at = NOW()
		RETURNING id`, artifactID, userID, message).Scan(&id)
	if err != nil {
		return nil, err
	}
	return p.GetAccessRequest(ctx, id)
}

func (p *Postgres) GetAccessRequest(ctx context.Context, id uuid.UUID) (*AccessRequest, error) {
	return scanAccessRequest(p.pool.QueryRow(ctx,
		`SELECT `+accessRequestCols+` FROM access_requests r JOIN users u ON u.id = r.user_id
		  WHERE r.id = $1`, id))
}

// MyAccessRequest is the caller's latest request on an artifact, whatever its
// status: a declined one tells the interface not to offer the button blindly.
func (p *Postgres) MyAccessRequest(ctx context.Context, artifactID, userID uuid.UUID) (*AccessRequest, error) {
	return scanAccessRequest(p.pool.QueryRow(ctx,
		`SELECT `+accessRequestCols+` FROM access_requests r JOIN users u ON u.id = r.user_id
		  WHERE r.artifact_id = $1 AND r.user_id = $2
		  ORDER BY r.created_at DESC LIMIT 1`, artifactID, userID))
}

// ListAccessRequests is what is pending on one artifact, oldest first.
func (p *Postgres) ListAccessRequests(ctx context.Context, artifactID uuid.UUID) ([]AccessRequest, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+accessRequestCols+` FROM access_requests r JOIN users u ON u.id = r.user_id
		  WHERE r.artifact_id = $1 AND r.status = 'pending'
		  ORDER BY r.created_at`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessRequest{}
	for rows.Next() {
		a, err := scanAccessRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ListAccessInbox is every pending request on artifacts the user owns, with
// the same notion of owner as ResolveAccess: the author, the owner of the
// personal space, or an owner/admin of the team space.
func (p *Postgres) ListAccessInbox(ctx context.Context, userID uuid.UUID) ([]AccessRequest, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+accessRequestCols+`, a.title, s.slug, a.slug
		   FROM access_requests r
		   JOIN users u     ON u.id = r.user_id
		   JOIN artifacts a ON a.id = r.artifact_id
		   JOIN spaces s    ON s.id = a.space_id
		  WHERE r.status = 'pending'
		    AND (a.author_id = $1
		         OR (s.kind = 'personal' AND s.owner_user_id = $1)
		         OR (s.kind = 'org' AND EXISTS (
		               SELECT 1 FROM org_members m
		                WHERE m.org_id = s.org_id AND m.user_id = $1 AND m.role IN ('owner','admin'))))
		  ORDER BY r.created_at DESC
		  LIMIT 200`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessRequest{}
	for rows.Next() {
		var spaceSlug string
		var slug *string
		var title string
		a, err := scanAccessRequest(rows, &title, &spaceSlug, &slug)
		if err != nil {
			return nil, err
		}
		art := Artifact{ID: a.ArtifactID, SpaceSlug: spaceSlug, Slug: slug}
		a.ArtifactTitle, a.ArtifactPath = title, art.Path()
		out = append(out, *a)
	}
	return out, rows.Err()
}

// DecideAccessRequest closes a pending request. Approving also grants editor
// access by email, in the same transaction: a request marked approved always
// means the access exists.
func (p *Postgres) DecideAccessRequest(ctx context.Context, id, decidedBy uuid.UUID, approve bool) (*AccessRequest, error) {
	req, err := p.GetAccessRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Status != "pending" {
		return nil, ErrConflict
	}
	status := "declined"
	if approve {
		status = "approved"
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if approve {
		if _, err := tx.Exec(ctx, `
			INSERT INTO artifact_shares (artifact_id, email, role, invited_by)
			VALUES ($1,$2,'editor',$3)
			ON CONFLICT (artifact_id, email) DO UPDATE SET role = 'editor'`,
			req.ArtifactID, strings.ToLower(req.Email), decidedBy); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE access_requests SET status=$2, decided_at=NOW(), decided_by=$3 WHERE id=$1`,
		id, status, decidedBy); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p.GetAccessRequest(ctx, id)
}
