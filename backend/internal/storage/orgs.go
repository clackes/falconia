package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------- Sharing by email ----------------

// Share is access granted to one person on a single artifact.
//
// The invitation targets an email, not an account: you can share with someone
// who has never signed in, and the permission kicks in by itself when they do.
// Whoever shares does not know, and should not need to know, who already has
// an account.
type Share struct {
	ArtifactID uuid.UUID
	Email      string
	Role       string // "viewer" | "editor"
	InvitedBy  uuid.UUID
	CreatedAt  time.Time
}

func (p *Postgres) AddShare(ctx context.Context, artifactID, invitedBy uuid.UUID, email, role string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("invalid email")
	}
	if role != "editor" {
		role = "viewer"
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO artifact_shares (artifact_id, email, role, invited_by)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (artifact_id, email) DO UPDATE SET role = EXCLUDED.role`,
		artifactID, email, role, invitedBy)
	return err
}

func (p *Postgres) ListShares(ctx context.Context, artifactID uuid.UUID) ([]Share, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT artifact_id, email, role, invited_by, created_at
		   FROM artifact_shares WHERE artifact_id=$1 ORDER BY created_at`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Share{}
	for rows.Next() {
		var s Share
		if err := rows.Scan(&s.ArtifactID, &s.Email, &s.Role, &s.InvitedBy, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) RemoveShare(ctx context.Context, artifactID uuid.UUID, email string) error {
	tag, err := p.pool.Exec(ctx,
		`DELETE FROM artifact_shares WHERE artifact_id=$1 AND email=$2`,
		artifactID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSharedWithMe is what other people shared with me: by email, or through
// a link I opened while signed in.
func (p *Postgres) ListSharedWithMe(ctx context.Context, userID uuid.UUID, email string) ([]Artifact, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+selectArtifactCols+fromArtifacts+`
		 WHERE a.author_id <> $2
		   AND (EXISTS (SELECT 1 FROM artifact_shares sh WHERE sh.artifact_id = a.id AND sh.email = $1)
		     OR (a.visibility = 'link' AND EXISTS (
		            SELECT 1 FROM artifact_link_visitors lv WHERE lv.artifact_id = a.id AND lv.user_id = $2)))
		 ORDER BY a.updated_at DESC LIMIT 200`,
		strings.ToLower(email), userID)
	if err != nil {
		return nil, err
	}
	return collectArtifacts(rows)
}

// ---------------- Teams ----------------
//
// The database calls them orgs; the interface calls them teams. A team is a
// group of people sharing a space, whatever agents each of them uses.

type Org struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	CreatedBy uuid.UUID
	CreatedAt time.Time
	MyRole    string
	SpaceSlug string
}

type Membership struct {
	OrgID    uuid.UUID
	UserID   uuid.UUID
	Email    string
	Name     string
	Role     string
	JoinedAt time.Time
	// Pending marks people invited who have not signed up yet.
	Pending bool
}

// CreateOrg founds a team, creates its space and adds the founder as owner.
func (p *Postgres) CreateOrg(ctx context.Context, name, slug string, byUser uuid.UUID) (*Org, error) {
	base := normalizeSlug(firstNonEmpty(slug, name))
	if base == "" {
		return nil, errors.New("invalid team name")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	o := &Org{ID: uuid.New(), Slug: base, Name: name, CreatedBy: byUser, CreatedAt: time.Now(), MyRole: "owner"}
	if _, err := tx.Exec(ctx,
		`INSERT INTO orgs (id, slug, name, created_by, created_at) VALUES ($1,$2,$3,$4,$5)`,
		o.ID, o.Slug, o.Name, o.CreatedBy, o.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1,$2,'owner')`,
		o.ID, byUser); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	sp, err := p.EnsureOrgSpace(ctx, o.ID, o.Slug, o.Name)
	if err != nil {
		return nil, err
	}
	o.SpaceSlug = sp.Slug
	return o, nil
}

func (p *Postgres) ListMyOrgs(ctx context.Context, userID uuid.UUID) ([]Org, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT o.id, o.slug, o.name, o.created_by, o.created_at, m.role, COALESCE(s.slug,'')
		  FROM orgs o
		  JOIN org_members m ON m.org_id = o.id AND m.user_id = $1
		  LEFT JOIN spaces s ON s.org_id = o.id
		 ORDER BY o.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Org{}
	for rows.Next() {
		var o Org
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedBy, &o.CreatedAt, &o.MyRole, &o.SpaceSlug); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (p *Postgres) GetOrg(ctx context.Context, id uuid.UUID) (*Org, error) {
	o := &Org{}
	err := p.pool.QueryRow(ctx,
		`SELECT o.id, o.slug, o.name, o.created_by, o.created_at, COALESCE(s.slug,'')
		   FROM orgs o LEFT JOIN spaces s ON s.org_id = o.id WHERE o.id=$1`, id,
	).Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedBy, &o.CreatedAt, &o.SpaceSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

func (p *Postgres) getOrgRole(ctx context.Context, orgID, userID uuid.UUID) (string, error) {
	var role string
	err := p.pool.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id=$1 AND user_id=$2`, orgID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// OrgRole is the public version of getOrgRole, used by handlers to decide who
// may invite.
func (p *Postgres) OrgRole(ctx context.Context, orgID, userID uuid.UUID) (string, error) {
	role, err := p.getOrgRole(ctx, orgID, userID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return role, err
}

// ListOrgMembers lists actual members and, after them, invitees who have not
// signed up yet: whoever runs a team needs to see those too, or they will
// re-invite the same person every week.
func (p *Postgres) ListOrgMembers(ctx context.Context, orgID uuid.UUID) ([]Membership, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT m.org_id, m.user_id, u.email, u.display_name, m.role, m.joined_at, FALSE AS pending
		  FROM org_members m JOIN users u ON u.id = m.user_id
		 WHERE m.org_id = $1
		UNION ALL
		SELECT i.org_id, '00000000-0000-0000-0000-000000000000'::uuid, i.email, '', i.role, i.created_at, TRUE
		  FROM org_invites i
		 WHERE i.org_id = $1
		   AND NOT EXISTS (SELECT 1 FROM users u2
		                    JOIN org_members m2 ON m2.user_id = u2.id AND m2.org_id = i.org_id
		                   WHERE u2.email = i.email)
		 ORDER BY pending, role, email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt, &m.Pending); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InviteToOrg adds people who already have an account right away and parks
// the others as pending invites. It returns "member" or "invite" so the
// interface can say the right thing.
func (p *Postgres) InviteToOrg(ctx context.Context, orgID, byUser uuid.UUID, email, role string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return "", errors.New("invalid email")
	}
	switch role {
	case "owner", "admin", "member":
	default:
		role = "member"
	}
	u, err := p.GetUserByEmail(ctx, email)
	if err == nil {
		_, err = p.pool.Exec(ctx, `
			INSERT INTO org_members (org_id, user_id, role) VALUES ($1,$2,$3)
			ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
			orgID, u.ID, role)
		return "member", err
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO org_invites (org_id, email, role, invited_by) VALUES ($1,$2,$3,$4)
		ON CONFLICT (org_id, email) DO UPDATE SET role = EXCLUDED.role`,
		orgID, email, role, byUser)
	return "invite", err
}

func (p *Postgres) RemoveOrgMember(ctx context.Context, orgID, userID uuid.UUID) error {
	// The last owner cannot be removed: a team nobody can administer is a dead
	// end you can only get out of through the database.
	var owners int
	if err := p.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM org_members WHERE org_id=$1 AND role='owner'`, orgID).Scan(&owners); err != nil {
		return err
	}
	var role string
	if err := p.pool.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id=$1 AND user_id=$2`, orgID, userID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if role == "owner" && owners <= 1 {
		return ErrForbidden
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM org_members WHERE org_id=$1 AND user_id=$2`, orgID, userID)
	return err
}

// AcceptPendingInvites turns invitations addressed to the email of whoever
// just signed in into memberships. It runs on every login, not only at
// sign-up: an invitation can arrive later.
func (p *Postgres) AcceptPendingInvites(ctx context.Context, userID uuid.UUID, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		WITH mine AS (DELETE FROM org_invites WHERE email = $2 RETURNING org_id, role)
		INSERT INTO org_members (org_id, user_id, role)
		SELECT org_id, $1, role FROM mine
		ON CONFLICT (org_id, user_id) DO NOTHING`, userID, email)
	return err
}
