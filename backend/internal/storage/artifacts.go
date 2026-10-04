package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/clackes/falconia/backend/internal/agents"
)

// ---------------- Types ----------------

// Artifact is the stable identity of something published: its address, who
// owns it, who can see it. The content lives in its versions.
type Artifact struct {
	ID          uuid.UUID
	AuthorID    uuid.UUID
	AuthorName  string
	AuthorEmail string

	SpaceID   uuid.UUID
	SpaceSlug string
	SpaceName string
	SpaceKind string

	Title       string
	Description string
	// Visibility is the "general access" setting: "private" | "org" | "link".
	Visibility string
	// TeamRole is what team members get when Visibility is "org".
	TeamRole string
	// LinkToken is the secret part of /p/<token>. It exists once the artifact
	// has been shared by link at least once, and survives switching general
	// access back to private, like a Google Docs link does.
	LinkToken  *string
	Icon       string
	ThemeColor string
	LogoURL    string
	Slug       *string
	Forkable   bool
	Category   string
	OwnerOrgID *uuid.UUID

	CreatedAt time.Time
	UpdatedAt time.Time

	LatestVersion *int
	ForkParentID  *uuid.UUID
	OpenThreads   int
	// Agents lists every AI agent that published a version, most recent first.
	// It is the visible proof that an artifact is not tied to any one of them.
	Agents []string
}

// Path is the artifact's stable address for signed-in people.
func (a *Artifact) Path() string {
	if a.Slug != nil && *a.Slug != "" {
		return "/s/" + a.SpaceSlug + "/" + *a.Slug
	}
	return "/a/" + a.ID.String()
}

// LinkPath is the anyone-with-the-link address, or "" when the artifact is not
// shared by link.
func (a *Artifact) LinkPath() string {
	if a.Visibility != "link" || a.LinkToken == nil {
		return ""
	}
	return "/p/" + *a.LinkToken
}

// ArtifactVersion is one deploy: immutable, numbered, attributed.
type ArtifactVersion struct {
	ID            int64
	ArtifactID    uuid.UUID
	VersionNumber int
	CodePayload   string // text mirror of the entry document
	CodeHash      string
	SchemaVersion int
	AuthorID      uuid.UUID
	AuthorName    string
	CommitMessage string
	EntryPath     string
	Kind          string // "doc" | "bundle"
	FileCount     int
	TotalBytes    int64
	// Agent is the AI agent that published this version, "" for a human.
	Agent     string
	CreatedAt time.Time
}

type ArtifactMeta struct {
	Title       string
	Description string
	Visibility  string
	TeamRole    string
	Icon        string
	ThemeColor  string
	LogoURL     string
	Slug        string
	Forkable    bool
	Category    string
}

type CreateArtifactInput struct {
	SpaceID     uuid.UUID
	Title       string
	Description string
	Code        string
	Icon        string
	ThemeColor  string
	LogoURL     string
	Slug        string
	Category    string
	Visibility  string
	Forkable    *bool
	Message     string
	// Bundle fields. When Files is empty the version is a single document.
	EntryPath string
	Files     []FileSpec
}

// ValidVisibility normalizes a visibility that came from outside.
//
// The default is the most restrictive one: a typo must never widen access to
// a document. The aliases cover what agents tend to write when they guess.
func ValidVisibility(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "org", "team":
		return "org"
	case "link", "public", "anyone", "unlisted", "anyone-with-link", "anyone_with_link":
		return "link"
	default:
		// "private", "restricted" and the legacy "shared" all mean: only the
		// owner and the people added by email.
		return "private"
	}
}

// ValidTeamRole normalizes the role given to team members.
func ValidTeamRole(r string) string {
	if strings.ToLower(strings.TrimSpace(r)) == "viewer" {
		return "viewer"
	}
	return "editor"
}

// newLinkToken returns 128 random bits, URL-safe. Long enough that guessing a
// link is not a strategy; short enough to paste in a chat.
func newLinkToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ---------------- Reads ----------------

const selectArtifactCols = `
	a.id, a.author_id, u.display_name, u.email,
	a.space_id, s.slug, s.name, s.kind,
	a.title, a.description, a.visibility, a.team_role, a.link_token, a.icon, a.theme_color,
	a.logo_url, a.slug, a.forkable, a.category, a.owner_org_id,
	a.created_at, a.updated_at,
	(SELECT MAX(version_number) FROM artifact_versions v WHERE v.artifact_id = a.id),
	(SELECT parent_artifact_id FROM fork_relations f WHERE f.child_artifact_id = a.id),
	(SELECT COUNT(*) FROM comment_threads t WHERE t.artifact_id = a.id AND t.status = 'open'),
	ARRAY(SELECT v.agent FROM artifact_versions v
	       WHERE v.artifact_id = a.id AND v.agent <> ''
	       GROUP BY v.agent ORDER BY MAX(v.version_number) DESC)`

const fromArtifacts = `
	FROM artifacts a
	JOIN users  u ON u.id = a.author_id
	JOIN spaces s ON s.id = a.space_id`

func scanArtifact(row pgx.Row) (*Artifact, error) {
	a := &Artifact{}
	err := row.Scan(&a.ID, &a.AuthorID, &a.AuthorName, &a.AuthorEmail,
		&a.SpaceID, &a.SpaceSlug, &a.SpaceName, &a.SpaceKind,
		&a.Title, &a.Description, &a.Visibility, &a.TeamRole, &a.LinkToken, &a.Icon, &a.ThemeColor,
		&a.LogoURL, &a.Slug, &a.Forkable, &a.Category, &a.OwnerOrgID,
		&a.CreatedAt, &a.UpdatedAt, &a.LatestVersion, &a.ForkParentID, &a.OpenThreads, &a.Agents)
	if a.Agents == nil {
		a.Agents = []string{}
	}
	return a, err
}

func collectArtifacts(rows pgx.Rows) ([]Artifact, error) {
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (p *Postgres) GetArtifact(ctx context.Context, id uuid.UUID) (*Artifact, error) {
	a, err := scanArtifact(p.pool.QueryRow(ctx,
		`SELECT `+selectArtifactCols+fromArtifacts+` WHERE a.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// GetArtifactByPath resolves /s/<space>/<slug>, the address people pass
// around. Lookup by UUID keeps working alongside it.
func (p *Postgres) GetArtifactByPath(ctx context.Context, spaceSlug, artifactSlug string) (*Artifact, error) {
	a, err := scanArtifact(p.pool.QueryRow(ctx,
		`SELECT `+selectArtifactCols+fromArtifacts+`
		  WHERE LOWER(s.slug)=LOWER($1) AND LOWER(a.slug)=LOWER($2)`,
		strings.TrimSpace(spaceSlug), strings.TrimSpace(artifactSlug)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// GetArtifactByLinkToken resolves /p/<token>.
//
// An artifact whose general access is no longer "link" answers ErrNotFound:
// switching back to private must close the door on everyone holding the link,
// immediately, without the owner having to rotate anything.
func (p *Postgres) GetArtifactByLinkToken(ctx context.Context, token string) (*Artifact, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrNotFound
	}
	a, err := scanArtifact(p.pool.QueryRow(ctx,
		`SELECT `+selectArtifactCols+fromArtifacts+`
		  WHERE a.link_token = $1 AND a.visibility = 'link'`, token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// RecordLinkVisit remembers that a signed-in person opened an artifact through
// its link. From then on it appears in their dashboard and they can comment,
// as long as general access stays "anyone with the link".
func (p *Postgres) RecordLinkVisit(ctx context.Context, artifactID, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO artifact_link_visitors (artifact_id, user_id) VALUES ($1,$2)
		ON CONFLICT (artifact_id, user_id) DO UPDATE SET last_seen = NOW()`, artifactID, userID)
	return err
}

// RotateLinkToken issues a new link and forgets everyone who came through the
// old one. It is the "the link leaked" button.
func (p *Postgres) RotateLinkToken(ctx context.Context, artifactID uuid.UUID) (string, error) {
	tok, err := newLinkToken()
	if err != nil {
		return "", err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE artifacts SET link_token=$2, updated_at=NOW() WHERE id=$1`, artifactID, tok)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM artifact_link_visitors WHERE artifact_id=$1`, artifactID); err != nil {
		return "", err
	}
	return tok, tx.Commit(ctx)
}

const selectVersionCols = `
	v.id, v.artifact_id, v.version_number, v.code_payload, v.code_hash, v.schema_version,
	v.author_id, COALESCE(vu.display_name,''), v.commit_message,
	v.entry_path, v.kind, v.file_count, v.total_bytes, v.agent, v.created_at`

func scanVersion(row pgx.Row) (*ArtifactVersion, error) {
	v := &ArtifactVersion{}
	err := row.Scan(&v.ID, &v.ArtifactID, &v.VersionNumber, &v.CodePayload, &v.CodeHash,
		&v.SchemaVersion, &v.AuthorID, &v.AuthorName, &v.CommitMessage,
		&v.EntryPath, &v.Kind, &v.FileCount, &v.TotalBytes, &v.Agent, &v.CreatedAt)
	return v, err
}

// GetVersion returns a specific version, or the latest one when n <= 0.
func (p *Postgres) GetVersion(ctx context.Context, artifactID uuid.UUID, n int) (*ArtifactVersion, error) {
	q := `SELECT ` + selectVersionCols + `
	        FROM artifact_versions v LEFT JOIN users vu ON vu.id = v.author_id
	       WHERE v.artifact_id = $1`
	args := []any{artifactID}
	if n > 0 {
		q += ` AND v.version_number = $2`
		args = append(args, n)
	} else {
		q += ` ORDER BY v.version_number DESC LIMIT 1`
	}
	v, err := scanVersion(p.pool.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (p *Postgres) GetArtifactWithVersion(ctx context.Context, id uuid.UUID, n int) (*Artifact, *ArtifactVersion, error) {
	a, err := p.GetArtifact(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	v, err := p.GetVersion(ctx, id, n)
	if err != nil {
		return a, nil, err
	}
	return a, v, nil
}

// ListVersions is the artifact's history. The payload is left out: a list of
// fifty half-megabyte versions helps nobody.
func (p *Postgres) ListVersions(ctx context.Context, artifactID uuid.UUID) ([]ArtifactVersion, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT v.id, v.artifact_id, v.version_number, '' AS code_payload, v.code_hash, v.schema_version,
		       v.author_id, COALESCE(vu.display_name,''), v.commit_message,
		       v.entry_path, v.kind, v.file_count, v.total_bytes, v.agent, v.created_at
		  FROM artifact_versions v LEFT JOIN users vu ON vu.id = v.author_id
		 WHERE v.artifact_id=$1 ORDER BY v.version_number DESC`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArtifactVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// ---------------- Lists ----------------

// ListFeedOptions describes a query over the artifacts a person can open.
type ListFeedOptions struct {
	Query    string
	Category string
	Agent    string
	SpaceID  *uuid.UUID
	Limit    int
}

// visibleWhere is the access rule of ResolveAccess expressed as SQL, with the
// user id in $1 and the lower-cased email in $2.
//
// The filter lives in the WHERE clause, never in a loop after the query: a list
// must never show the title of something that would then answer 404, because
// even the title of a restricted document is information.
const visibleWhere = `(
	   a.author_id = $1
	OR (s.kind = 'personal' AND s.owner_user_id = $1)
	OR (s.kind = 'org' AND EXISTS (
	        SELECT 1 FROM org_members m
	         WHERE m.org_id = s.org_id AND m.user_id = $1
	           AND (m.role IN ('owner','admin') OR a.visibility = 'org')))
	OR EXISTS (SELECT 1 FROM artifact_shares sh
	            WHERE sh.artifact_id = a.id AND sh.email = $2)
	OR (a.visibility = 'link' AND EXISTS (
	        SELECT 1 FROM artifact_link_visitors lv
	         WHERE lv.artifact_id = a.id AND lv.user_id = $1))
)`

// ListVisibleTo returns the artifacts a person can actually open, most
// recently updated first.
func (p *Postgres) ListVisibleTo(ctx context.Context, userID uuid.UUID, email string, opt ListFeedOptions) ([]Artifact, error) {
	limit := opt.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{userID, strings.ToLower(email)}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	where := visibleWhere
	if opt.Query != "" {
		ph := arg(opt.Query)
		where += ` AND (a.title ILIKE '%'||` + ph + `||'%' OR a.description ILIKE '%'||` + ph + `||'%')`
	}
	if opt.Category != "" {
		where += ` AND a.category = ` + arg(opt.Category)
	}
	if opt.Agent != "" {
		where += ` AND EXISTS (SELECT 1 FROM artifact_versions av
		                        WHERE av.artifact_id = a.id AND av.agent = ` + arg(opt.Agent) + `)`
	}
	if opt.SpaceID != nil {
		where += ` AND a.space_id = ` + arg(*opt.SpaceID)
	}

	rows, err := p.pool.Query(ctx,
		`SELECT `+selectArtifactCols+fromArtifacts+`
		  WHERE `+where+`
		  ORDER BY a.updated_at DESC LIMIT `+arg(limit), args...)
	if err != nil {
		return nil, err
	}
	return collectArtifacts(rows)
}

// ListByAuthor is "my creations", wherever they live.
func (p *Postgres) ListByAuthor(ctx context.Context, authorID uuid.UUID) ([]Artifact, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+selectArtifactCols+fromArtifacts+`
		  WHERE a.author_id=$1 ORDER BY a.updated_at DESC LIMIT 500`, authorID)
	if err != nil {
		return nil, err
	}
	return collectArtifacts(rows)
}

func (p *Postgres) ListBySpace(ctx context.Context, spaceID uuid.UUID, userID uuid.UUID, email string) ([]Artifact, error) {
	return p.ListVisibleTo(ctx, userID, email, ListFeedOptions{SpaceID: &spaceID, Limit: 200})
}

// ForkChildren lists the direct descendants the caller can see.
func (p *Postgres) ForkChildren(ctx context.Context, parentID, userID uuid.UUID, email string) ([]Artifact, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+selectArtifactCols+`
		  FROM fork_relations f
		  JOIN artifacts a ON a.id = f.child_artifact_id
		  JOIN users  u ON u.id = a.author_id
		  JOIN spaces s ON s.id = a.space_id
		 WHERE f.parent_artifact_id = $3 AND `+visibleWhere+`
		 ORDER BY a.created_at DESC`, userID, strings.ToLower(email), parentID)
	if err != nil {
		return nil, err
	}
	return collectArtifacts(rows)
}

func (p *Postgres) FindExistingForkOwnedBy(ctx context.Context, parentID, userID uuid.UUID) (*Artifact, error) {
	a, err := scanArtifact(p.pool.QueryRow(ctx, `
		SELECT `+selectArtifactCols+`
		  FROM fork_relations f
		  JOIN artifacts a ON a.id = f.child_artifact_id
		  JOIN users  u ON u.id = a.author_id
		  JOIN spaces s ON s.id = a.space_id
		 WHERE f.parent_artifact_id=$1 AND a.author_id=$2
		 ORDER BY a.created_at ASC LIMIT 1`, parentID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// ---------------- Writes ----------------

// CreateArtifact records an artifact and its first version.
//
// When the requested slug is already taken in the space it does not fail: a
// counter is appended. A deploy coming from an agent or a pipeline should not
// stop because someone already called their artifact "report".
func (p *Postgres) CreateArtifact(ctx context.Context, authorID uuid.UUID, in CreateArtifactInput) (*Artifact, *ArtifactVersion, error) {
	if in.ThemeColor == "" {
		in.ThemeColor = "#0b0d12"
	}
	forkable := true
	if in.Forkable != nil {
		forkable = *in.Forkable
	}
	sp, err := p.GetSpace(ctx, in.SpaceID)
	if err != nil {
		return nil, nil, err
	}
	slug, err := p.freeArtifactSlug(ctx, in.SpaceID, firstNonEmpty(in.Slug, in.Title))
	if err != nil {
		return nil, nil, err
	}
	visibility := ValidVisibility(in.Visibility)
	if visibility == "org" && sp.Kind != "org" {
		// A personal space has no team to share with.
		visibility = "private"
	}
	var linkToken *string
	if visibility == "link" {
		tok, err := newLinkToken()
		if err != nil {
			return nil, nil, err
		}
		linkToken = &tok
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	a := &Artifact{
		ID: uuid.New(), AuthorID: authorID, SpaceID: sp.ID,
		Title: in.Title, Description: in.Description, Visibility: visibility,
		TeamRole: "editor", LinkToken: linkToken,
		Icon: in.Icon, ThemeColor: in.ThemeColor, LogoURL: in.LogoURL,
		Slug: nilIfEmpty(slug), Forkable: forkable, Category: in.Category,
		OwnerOrgID: sp.OrgID, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO artifacts (id, author_id, space_id, title, description, visibility, team_role,
		                       link_token, icon, theme_color, logo_url, slug, forkable, category,
		                       owner_org_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		a.ID, a.AuthorID, a.SpaceID, a.Title, a.Description, a.Visibility, a.TeamRole,
		a.LinkToken, a.Icon, a.ThemeColor, a.LogoURL, a.Slug, a.Forkable, a.Category,
		a.OwnerOrgID, a.CreatedAt, a.UpdatedAt); err != nil {
		return nil, nil, err
	}

	v, err := p.insertVersionTx(ctx, tx, a.ID, authorID, versionInput{
		Code: in.Code, Message: firstNonEmpty(in.Message, "initial version"),
		EntryPath: in.EntryPath, Files: in.Files,
	}, 1)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return a, v, nil
}

type versionInput struct {
	Code      string
	Message   string
	EntryPath string
	Files     []FileSpec
}

// CommitVersion publishes a new version. The URL does not change: that is the
// whole point of having versions.
func (p *Postgres) CommitVersion(ctx context.Context, artifactID, authorID uuid.UUID, in versionInput) (*ArtifactVersion, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Locking the artifact row serializes two simultaneous deploys to the same
	// artifact: without it both would compute the same version number and one
	// would die on the unique constraint.
	var next int
	err = tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT MAX(version_number) FROM artifact_versions WHERE artifact_id = a.id), 0) + 1
		  FROM artifacts a WHERE a.id = $1 FOR UPDATE`, artifactID).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	v, err := p.insertVersionTx(ctx, tx, artifactID, authorID, in, next)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE artifacts SET updated_at=NOW() WHERE id=$1`, artifactID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

func (p *Postgres) insertVersionTx(ctx context.Context, tx pgx.Tx, artifactID, authorID uuid.UUID, in versionInput, n int) (*ArtifactVersion, error) {
	entry := in.EntryPath
	if entry == "" {
		entry = "index.html"
	}
	kind := "doc"
	var totalBytes int64
	if len(in.Files) > 0 {
		kind = "bundle"
		for _, f := range in.Files {
			totalBytes += f.Size
		}
	} else {
		totalBytes = int64(len(in.Code))
	}
	sum := sha256.Sum256([]byte(in.Code))

	v := &ArtifactVersion{
		ArtifactID: artifactID, VersionNumber: n, CodePayload: in.Code,
		CodeHash: hex.EncodeToString(sum[:]), SchemaVersion: 1,
		AuthorID: authorID, CommitMessage: in.Message,
		EntryPath: entry, Kind: kind, FileCount: len(in.Files),
		TotalBytes: totalBytes, Agent: agents.From(ctx), CreatedAt: time.Now(),
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO artifact_versions (artifact_id, version_number, code_payload, code_hash,
		                               schema_version, author_id, commit_message,
		                               entry_path, kind, file_count, total_bytes, agent, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
		v.ArtifactID, v.VersionNumber, v.CodePayload, v.CodeHash, v.SchemaVersion,
		v.AuthorID, v.CommitMessage, v.EntryPath, v.Kind, v.FileCount, v.TotalBytes, v.Agent, v.CreatedAt,
	).Scan(&v.ID); err != nil {
		return nil, err
	}
	if err := insertFilesTx(ctx, tx, v.ID, in.Files); err != nil {
		return nil, err
	}
	return v, nil
}

// UpdateMeta changes the metadata. The slug, when it changes, is freed up the
// same way it is assigned at creation. Switching general access to "link" for
// the first time mints the link token.
func (p *Postgres) UpdateMeta(ctx context.Context, id uuid.UUID, m ArtifactMeta) error {
	cur, err := p.GetArtifact(ctx, id)
	if err != nil {
		return err
	}
	slug := normalizeSlug(m.Slug)
	if slug != "" && (cur.Slug == nil || !strings.EqualFold(slug, *cur.Slug)) {
		if slug, err = p.freeArtifactSlug(ctx, cur.SpaceID, slug); err != nil {
			return err
		}
	} else if slug == "" && cur.Slug != nil {
		slug = *cur.Slug
	}
	visibility := ValidVisibility(m.Visibility)
	if visibility == "org" && cur.SpaceKind != "org" {
		visibility = "private"
	}
	linkToken := cur.LinkToken
	if visibility == "link" && linkToken == nil {
		tok, err := newLinkToken()
		if err != nil {
			return err
		}
		linkToken = &tok
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE artifacts
		   SET title=$2, description=$3, visibility=$4, team_role=$5, link_token=$6, icon=$7,
		       theme_color=$8, logo_url=$9, slug=$10, forkable=$11, category=$12, updated_at=NOW()
		 WHERE id=$1`,
		id, m.Title, m.Description, visibility, ValidTeamRole(m.TeamRole), linkToken, m.Icon,
		m.ThemeColor, m.LogoURL, nilIfEmpty(slug), m.Forkable, m.Category)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MetaOf returns the editable metadata of an artifact, the starting point for
// a partial update.
func MetaOf(a *Artifact) ArtifactMeta {
	slug := ""
	if a.Slug != nil {
		slug = *a.Slug
	}
	return ArtifactMeta{
		Title: a.Title, Description: a.Description, Visibility: a.Visibility,
		TeamRole: a.TeamRole, Icon: a.Icon, ThemeColor: a.ThemeColor, LogoURL: a.LogoURL,
		Slug: slug, Forkable: a.Forkable, Category: a.Category,
	}
}

// MoveToSpace moves an artifact to another space, changing its address. Files,
// versions, comments and shares stay attached: only where it lives changes.
func (p *Postgres) MoveToSpace(ctx context.Context, artifactID, spaceID uuid.UUID) error {
	sp, err := p.GetSpace(ctx, spaceID)
	if err != nil {
		return err
	}
	cur, err := p.GetArtifact(ctx, artifactID)
	if err != nil {
		return err
	}
	slug := ""
	if cur.Slug != nil {
		slug = *cur.Slug
	}
	// The slug may be taken in the destination space by a different artifact:
	// look for the first free one there, not here.
	if slug != "" {
		if slug, err = p.freeArtifactSlug(ctx, spaceID, slug); err != nil {
			return err
		}
	}
	visibility := cur.Visibility
	if visibility == "org" && sp.Kind != "org" {
		visibility = "private"
	}
	tag, err := p.pool.Exec(ctx,
		`UPDATE artifacts SET space_id=$2, owner_org_id=$3, slug=$4, visibility=$5, updated_at=NOW() WHERE id=$1`,
		artifactID, sp.ID, sp.OrgID, nilIfEmpty(slug), visibility)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteArtifact(ctx context.Context, id uuid.UUID) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM artifacts WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Fork copies the latest version into a new artifact, inside the given space.
//
// A fork inherits nothing from its parent after birth, and it is born
// private: whoever remixes a colleague's document must not find it shared with
// the whole team by accident.
func (p *Postgres) Fork(ctx context.Context, sourceID, newAuthorID, spaceID uuid.UUID, newTitle string) (*Artifact, *ArtifactVersion, error) {
	src, err := p.GetArtifact(ctx, sourceID)
	if err != nil {
		return nil, nil, err
	}
	if !src.Forkable {
		return nil, nil, ErrForbidden
	}
	srcV, err := p.GetVersion(ctx, sourceID, 0)
	if err != nil {
		return nil, nil, err
	}
	files, err := p.ListFiles(ctx, srcV.ID)
	if err != nil {
		return nil, nil, err
	}
	specs := make([]FileSpec, 0, len(files))
	for _, f := range files {
		specs = append(specs, FileSpec{Path: f.Path, ContentType: f.ContentType, Size: f.SizeBytes, SHA256: f.SHA256})
	}
	title := newTitle
	if title == "" {
		title = src.Title
	}

	a, v, err := p.CreateArtifact(ctx, newAuthorID, CreateArtifactInput{
		SpaceID: spaceID, Title: title, Description: src.Description,
		Code: srcV.CodePayload, Icon: src.Icon, ThemeColor: src.ThemeColor,
		LogoURL: src.LogoURL, Slug: title, Category: src.Category,
		Visibility: "private", EntryPath: srcV.EntryPath, Files: specs,
		Message: fmt.Sprintf("forked from %s v%d", src.Title, srcV.VersionNumber),
	})
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO fork_relations (child_artifact_id, parent_artifact_id, forked_from_version)
		 VALUES ($1,$2,$3)`, a.ID, sourceID, srcV.VersionNumber); err != nil {
		return nil, nil, err
	}
	return a, v, nil
}

// freeArtifactSlug finds the first free slug in the space, starting from want.
func (p *Postgres) freeArtifactSlug(ctx context.Context, spaceID uuid.UUID, want string) (string, error) {
	base := normalizeSlug(want)
	if base == "" {
		return "", nil
	}
	if len(base) > 60 {
		base = strings.Trim(base[:60], "-")
	}
	for attempt := 1; attempt <= 100; attempt++ {
		candidate := base
		if attempt > 1 {
			candidate = fmt.Sprintf("%s-%d", base, attempt)
		}
		var taken bool
		if err := p.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM artifacts WHERE space_id=$1 AND LOWER(slug)=$2)`,
			spaceID, candidate).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return base + "-" + uuid.NewString()[:8], nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// NewVersionInput builds a version input from outside the package.
//
// The type stays unexported on purpose: every publisher goes through here and
// cannot invent combinations that insertVersionTx does not know how to handle.
func NewVersionInput(code, message, entryPath string, files []FileSpec) versionInput {
	return versionInput{Code: code, Message: message, EntryPath: entryPath, Files: files}
}
