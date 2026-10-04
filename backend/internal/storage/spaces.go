package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------- Spaces ----------------

// Space is the unit of naming and ownership. Every artifact lives in exactly
// one, and its address for signed-in people is /s/<space>/<slug>: stable
// forever, even as the versions underneath change.
type Space struct {
	ID          uuid.UUID
	Slug        string
	Name        string
	Kind        string // "personal" | "org"
	OwnerUserID *uuid.UUID
	OrgID       *uuid.UUID
	CreatedAt   time.Time
	// MyRole is the role of whoever asked for the list, not a property of the
	// space: it is only meaningful inside ListMySpaces results.
	MyRole string
}

const selectSpaceCols = `s.id, s.slug, s.name, s.kind, s.owner_user_id, s.org_id, s.created_at`

func scanSpace(row pgx.Row) (*Space, error) {
	s := &Space{}
	err := row.Scan(&s.ID, &s.Slug, &s.Name, &s.Kind, &s.OwnerUserID, &s.OrgID, &s.CreatedAt)
	return s, err
}

// EnsurePersonalSpace creates a user's personal space if it does not exist.
//
// The slug starts from the local part of the email and grows a numeric suffix
// on collision. It tries a bounded number of times and then falls back to a
// random suffix, so two simultaneous sign-ups cannot spin for long.
func (p *Postgres) EnsurePersonalSpace(ctx context.Context, userID uuid.UUID, email, displayName string) (*Space, error) {
	if s, err := p.GetSpaceByOwner(ctx, userID); err == nil {
		return s, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	base := normalizeSlug(strings.SplitN(email, "@", 2)[0])
	if base == "" {
		base = "user"
	}
	for attempt := 1; attempt <= 50; attempt++ {
		candidate := base
		if attempt > 1 {
			candidate = fmt.Sprintf("%s-%d", base, attempt)
		}
		s, err := p.createSpace(ctx, candidate, displayName, "personal", &userID, nil)
		if err == nil {
			return s, nil
		}
		if !isUniqueViolation(err) {
			return nil, err
		}
	}
	return p.createSpace(ctx, base+"-"+uuid.NewString()[:8], displayName, "personal", &userID, nil)
}

// EnsureOrgSpace creates a team's space if it does not exist.
func (p *Postgres) EnsureOrgSpace(ctx context.Context, orgID uuid.UUID, slug, name string) (*Space, error) {
	if s, err := p.GetSpaceByOrg(ctx, orgID); err == nil {
		return s, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	base := normalizeSlug(slug)
	if base == "" {
		base = "team"
	}
	for attempt := 1; attempt <= 50; attempt++ {
		candidate := base
		if attempt > 1 {
			candidate = fmt.Sprintf("%s-%d", base, attempt)
		}
		s, err := p.createSpace(ctx, candidate, name, "org", nil, &orgID)
		if err == nil {
			return s, nil
		}
		if !isUniqueViolation(err) {
			return nil, err
		}
	}
	return p.createSpace(ctx, base+"-"+uuid.NewString()[:8], name, "org", nil, &orgID)
}

func (p *Postgres) createSpace(ctx context.Context, slug, name, kind string, userID, orgID *uuid.UUID) (*Space, error) {
	s := &Space{ID: uuid.New(), Slug: slug, Name: name, Kind: kind,
		OwnerUserID: userID, OrgID: orgID, CreatedAt: time.Now()}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO spaces (id, slug, name, kind, owner_user_id, org_id, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		s.ID, s.Slug, s.Name, s.Kind, s.OwnerUserID, s.OrgID, s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (p *Postgres) GetSpace(ctx context.Context, id uuid.UUID) (*Space, error) {
	s, err := scanSpace(p.pool.QueryRow(ctx, `SELECT `+selectSpaceCols+` FROM spaces s WHERE s.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (p *Postgres) GetSpaceBySlug(ctx context.Context, slug string) (*Space, error) {
	s, err := scanSpace(p.pool.QueryRow(ctx,
		`SELECT `+selectSpaceCols+` FROM spaces s WHERE LOWER(s.slug)=LOWER($1)`, strings.TrimSpace(slug)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (p *Postgres) GetSpaceByOwner(ctx context.Context, userID uuid.UUID) (*Space, error) {
	s, err := scanSpace(p.pool.QueryRow(ctx,
		`SELECT `+selectSpaceCols+` FROM spaces s WHERE s.owner_user_id=$1 AND s.kind='personal'`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (p *Postgres) GetSpaceByOrg(ctx context.Context, orgID uuid.UUID) (*Space, error) {
	s, err := scanSpace(p.pool.QueryRow(ctx,
		`SELECT `+selectSpaceCols+` FROM spaces s WHERE s.org_id=$1`, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// ListMySpaces returns the user's personal space plus those of the teams they
// belong to. It is the menu of destinations that `falconia spaces` prints and
// that the sidebar shows.
func (p *Postgres) ListMySpaces(ctx context.Context, userID uuid.UUID) ([]Space, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+selectSpaceCols+`, 'owner' AS my_role
		  FROM spaces s
		 WHERE s.owner_user_id = $1 AND s.kind = 'personal'
		UNION ALL
		SELECT `+selectSpaceCols+`, m.role AS my_role
		  FROM spaces s
		  JOIN org_members m ON m.org_id = s.org_id AND m.user_id = $1
		 WHERE s.kind = 'org'
		 ORDER BY kind DESC, name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Space{}
	for rows.Next() {
		var s Space
		if err := rows.Scan(&s.ID, &s.Slug, &s.Name, &s.Kind, &s.OwnerUserID, &s.OrgID, &s.CreatedAt, &s.MyRole); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SpaceRole returns the user's role in the space:
// "owner" | "admin" | "member" | "" (not a member).
func (p *Postgres) SpaceRole(ctx context.Context, sp *Space, userID uuid.UUID) (string, error) {
	if userID == uuid.Nil {
		return "", nil
	}
	if sp.Kind == "personal" {
		if sp.OwnerUserID != nil && *sp.OwnerUserID == userID {
			return "owner", nil
		}
		return "", nil
	}
	role, err := p.getOrgRole(ctx, *sp.OrgID, userID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return role, err
}

// ---------------- Access resolution ----------------

// Role is a person's effective permission on an artifact.
//
// Deliberately few levels: who is in charge, who can publish, who can only
// look and comment. Commenting is not a write privilege, because the point of
// the product is that anyone who sees an artifact can give feedback without
// being handed the source.
type Role string

const (
	RoleOwner  Role = "owner"  // metadata, sharing, deletion
	RoleEditor Role = "editor" // publishes new versions
	RoleViewer Role = "viewer" // reads, comments, resolves their own threads
	RoleNone   Role = "none"
)

// CanDeploy reports whether the role may publish a new version.
func (r Role) CanDeploy() bool { return r == RoleOwner || r == RoleEditor }

// CanRead reports whether the role may open the artifact.
func (r Role) CanRead() bool { return r != RoleNone }

// ResolveAccess computes a signed-in user's role on an artifact.
//
// Order matters, from the strongest grant to the weakest:
//
//  1. the author always controls their own creation;
//  2. the owner of a personal space controls everything in it;
//  3. team owners and admins control the team space, otherwise an artifact
//     would become unreachable when its author leaves;
//  4. a team member gets the artifact's team role when general access is
//     "team";
//  5. an explicit share by email always applies, whatever the general access:
//     that is its whole purpose;
//  6. someone who opened the secret link while signed in is a viewer, for as
//     long as general access stays "anyone with the link".
//
// Anonymous visitors never get a role here: they reach link-shared artifacts
// through GetArtifactByLinkToken, which only ever grants viewing.
func (p *Postgres) ResolveAccess(ctx context.Context, artifactID, userID uuid.UUID, userEmail string) (Role, error) {
	var authorID, spaceID uuid.UUID
	var visibility, teamRole string
	err := p.pool.QueryRow(ctx,
		`SELECT author_id, space_id, visibility, team_role FROM artifacts WHERE id=$1`,
		artifactID).Scan(&authorID, &spaceID, &visibility, &teamRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return RoleNone, ErrNotFound
	}
	if err != nil {
		return RoleNone, err
	}
	if userID == uuid.Nil {
		return RoleNone, ErrForbidden
	}
	if authorID == userID {
		return RoleOwner, nil
	}

	sp, err := p.GetSpace(ctx, spaceID)
	if err != nil {
		return RoleNone, err
	}
	best := RoleNone
	switch role, err := p.SpaceRole(ctx, sp, userID); {
	case err != nil:
		return RoleNone, err
	case role == "owner", role == "admin":
		return RoleOwner, nil
	case role == "member" && visibility == "org":
		if teamRole == "viewer" {
			best = RoleViewer
		} else {
			return RoleEditor, nil
		}
	}

	if userEmail != "" {
		var shareRole string
		err := p.pool.QueryRow(ctx,
			`SELECT role FROM artifact_shares WHERE artifact_id=$1 AND email=$2`,
			artifactID, strings.ToLower(userEmail)).Scan(&shareRole)
		if err == nil {
			if shareRole == "editor" {
				return RoleEditor, nil
			}
			best = RoleViewer
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return RoleNone, err
		}
	}
	if best != RoleNone {
		return best, nil
	}

	if visibility == "link" {
		var visited bool
		if err := p.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM artifact_link_visitors WHERE artifact_id=$1 AND user_id=$2)`,
			artifactID, userID).Scan(&visited); err != nil {
			return RoleNone, err
		}
		if visited {
			return RoleViewer, nil
		}
	}
	return RoleNone, ErrForbidden
}
