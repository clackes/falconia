package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/clackes/falconia/backend/internal/agents"
)

// Anchor is the spot in an artifact a thread is attached to.
//
// Three clues instead of one, because an artifact's DOM changes with every
// version and a selector alone ages badly:
//
//	Selector — the exact path, valid until the structure changes;
//	Quote    — the text the person was looking at, which often survives a
//	           markup refactor;
//	X, Y     — coordinates relative to the page, 0 to 1, the last resort.
//
// The shim inside the iframe tries them in that order. When none resolves, the
// thread does not disappear: it becomes "detached" and stays in the side
// panel, because a comment that can no longer be anchored is still worth more
// than a lost one.
type Anchor struct {
	Selector string  `json:"selector,omitempty"`
	Quote    string  `json:"quote,omitempty"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
}

// Thread is a conversation about one spot of an artifact.
type Thread struct {
	ID            uuid.UUID
	ArtifactID    uuid.UUID
	VersionNumber int
	PagePath      string
	Anchor        *Anchor
	Status        string // "open" | "resolved"
	CreatedBy     uuid.UUID
	CreatedByName string
	ResolvedBy    *uuid.UUID
	ResolvedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Comments      []Comment
}

// Comment is one message inside a thread.
type Comment struct {
	ID        uuid.UUID
	ThreadID  uuid.UUID
	UserID    uuid.UUID
	UserName  string
	Body      string
	Via       string // "web" | "cli" | "ci" | "mcp" | "api"
	Agent     string // the AI agent that wrote it, "" for a human
	Edited    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

const selectThreadCols = `
	t.id, t.artifact_id, t.version_number, t.page_path, t.anchor, t.status,
	t.created_by, COALESCE(cu.display_name,''), t.resolved_by, t.resolved_at,
	t.created_at, t.updated_at`

const fromThreads = `
	FROM comment_threads t LEFT JOIN users cu ON cu.id = t.created_by`

func scanThread(row pgx.Row) (*Thread, error) {
	t := &Thread{}
	var raw []byte
	if err := row.Scan(&t.ID, &t.ArtifactID, &t.VersionNumber, &t.PagePath, &raw, &t.Status,
		&t.CreatedBy, &t.CreatedByName, &t.ResolvedBy, &t.ResolvedAt,
		&t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		var a Anchor
		// An unreadable anchor must not break reading the thread: the comment
		// stays, just without a pin.
		if json.Unmarshal(raw, &a) == nil {
			t.Anchor = &a
		}
	}
	return t, nil
}

// CreateThread opens a conversation and writes its first message.
func (p *Postgres) CreateThread(ctx context.Context, artifactID, userID uuid.UUID, versionNumber int, pagePath, body string, anchor *Anchor, via string) (*Thread, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("empty comment")
	}
	if pagePath == "" {
		pagePath = "index.html"
	}
	if versionNumber <= 0 {
		versionNumber = 1
	}
	var raw []byte
	if anchor != nil {
		raw, _ = json.Marshal(anchor)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO comment_threads (artifact_id, version_number, page_path, anchor, created_by)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		artifactID, versionNumber, NormalizePath(pagePath), raw, userID).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO artifact_comments (artifact_id, thread_id, user_id, body, via, agent)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		artifactID, id, userID, body, sanitizeVia(via), agents.From(ctx)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p.GetThread(ctx, id)
}

// Reply adds a message to an existing thread.
//
// Replying implicitly reopens a resolved thread: if someone writes again about
// something that was closed, evidently it was not done.
func (p *Postgres) Reply(ctx context.Context, threadID, userID uuid.UUID, body, via string) (*Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("empty comment")
	}
	var artifactID uuid.UUID
	if err := p.pool.QueryRow(ctx,
		`SELECT artifact_id FROM comment_threads WHERE id=$1`, threadID).Scan(&artifactID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c := &Comment{ThreadID: threadID, UserID: userID, Body: body, Via: sanitizeVia(via), Agent: agents.From(ctx)}
	if err := p.pool.QueryRow(ctx, `
		INSERT INTO artifact_comments (artifact_id, thread_id, user_id, body, via, agent)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at, updated_at`,
		artifactID, threadID, userID, body, c.Via, c.Agent,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	if _, err := p.pool.Exec(ctx,
		`UPDATE comment_threads
		    SET updated_at=NOW(), status='open', resolved_by=NULL, resolved_at=NULL
		  WHERE id=$1`, threadID); err != nil {
		return nil, err
	}
	return c, nil
}

// SetThreadStatus resolves or reopens a thread.
func (p *Postgres) SetThreadStatus(ctx context.Context, threadID, userID uuid.UUID, resolved bool) error {
	var err error
	if resolved {
		_, err = p.pool.Exec(ctx,
			`UPDATE comment_threads SET status='resolved', resolved_by=$2, resolved_at=NOW(), updated_at=NOW()
			  WHERE id=$1`, threadID, userID)
	} else {
		_, err = p.pool.Exec(ctx,
			`UPDATE comment_threads SET status='open', resolved_by=NULL, resolved_at=NULL, updated_at=NOW()
			  WHERE id=$1`, threadID)
	}
	return err
}

func (p *Postgres) GetThread(ctx context.Context, id uuid.UUID) (*Thread, error) {
	t, err := scanThread(p.pool.QueryRow(ctx, `SELECT `+selectThreadCols+fromThreads+` WHERE t.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if t.Comments, err = p.listComments(ctx, []uuid.UUID{t.ID}); err != nil {
		return nil, err
	}
	return t, nil
}

// ThreadFilter narrows the list of an artifact's threads.
type ThreadFilter struct {
	OnlyOpen bool
	PagePath string
	Limit    int
}

// ListThreads returns an artifact's threads with their messages inside.
//
// Comments are read with a single second query for all threads, not one per
// thread: a heavily commented page would otherwise take fifty queries to draw
// one side panel.
func (p *Postgres) ListThreads(ctx context.Context, artifactID uuid.UUID, f ThreadFilter) ([]Thread, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	args := []any{artifactID, limit}
	where := `t.artifact_id = $1`
	if f.OnlyOpen {
		where += ` AND t.status = 'open'`
	}
	if f.PagePath != "" {
		args = append(args, NormalizePath(f.PagePath))
		where += ` AND t.page_path = $3`
	}
	rows, err := p.pool.Query(ctx,
		`SELECT `+selectThreadCols+fromThreads+`
		  WHERE `+where+` ORDER BY t.created_at ASC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	threads := []Thread{}
	ids := []uuid.UUID{}
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		threads = append(threads, *t)
		ids = append(ids, t.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return threads, nil
	}
	comments, err := p.listComments(ctx, ids)
	if err != nil {
		return nil, err
	}
	byThread := map[uuid.UUID][]Comment{}
	for _, c := range comments {
		byThread[c.ThreadID] = append(byThread[c.ThreadID], c)
	}
	for i := range threads {
		threads[i].Comments = byThread[threads[i].ID]
	}
	return threads, nil
}

func (p *Postgres) listComments(ctx context.Context, threadIDs []uuid.UUID) ([]Comment, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT c.id, c.thread_id, c.user_id, COALESCE(u.display_name,''), c.body, c.via, c.agent,
		       c.edited, c.created_at, c.updated_at
		  FROM artifact_comments c LEFT JOIN users u ON u.id = c.user_id
		 WHERE c.thread_id = ANY($1) ORDER BY c.created_at ASC`, threadIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.ThreadID, &c.UserID, &c.UserName, &c.Body, &c.Via, &c.Agent,
			&c.Edited, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EditComment and DeleteComment only work on one's own message: moderating
// others is the artifact owner's job, and it goes through deleting the thread.
func (p *Postgres) EditComment(ctx context.Context, commentID, userID uuid.UUID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("empty comment")
	}
	tag, err := p.pool.Exec(ctx,
		`UPDATE artifact_comments SET body=$3, edited=TRUE, updated_at=NOW()
		  WHERE id=$1 AND user_id=$2`, commentID, userID, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteComment(ctx context.Context, commentID, userID uuid.UUID) error {
	var threadID uuid.UUID
	err := p.pool.QueryRow(ctx,
		`DELETE FROM artifact_comments WHERE id=$1 AND user_id=$2 RETURNING thread_id`,
		commentID, userID).Scan(&threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// A thread left with no messages has no reason to exist: without this it
	// would remain as a pin that opens an empty conversation. Only the touched
	// thread is checked, not all of them: this runs on a request path, not in a
	// maintenance job.
	_, _ = p.pool.Exec(ctx, `
		DELETE FROM comment_threads t
		 WHERE t.id = $1
		   AND NOT EXISTS (SELECT 1 FROM artifact_comments c WHERE c.thread_id = t.id)`, threadID)
	return nil
}

func (p *Postgres) DeleteThread(ctx context.Context, threadID uuid.UUID) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM comment_threads WHERE id=$1`, threadID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func sanitizeVia(v string) string {
	switch v {
	case "web", "cli", "ci", "mcp", "api", "link":
		return v
	default:
		return "web"
	}
}

// ---------------- Likes ----------------

func (p *Postgres) LikeArtifact(ctx context.Context, artifactID, userID uuid.UUID) (int, error) {
	if _, err := p.pool.Exec(ctx,
		`INSERT INTO artifact_likes (artifact_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		artifactID, userID); err != nil {
		return 0, err
	}
	return p.CountLikes(ctx, artifactID)
}

func (p *Postgres) UnlikeArtifact(ctx context.Context, artifactID, userID uuid.UUID) (int, error) {
	if _, err := p.pool.Exec(ctx,
		`DELETE FROM artifact_likes WHERE artifact_id=$1 AND user_id=$2`, artifactID, userID); err != nil {
		return 0, err
	}
	return p.CountLikes(ctx, artifactID)
}

func (p *Postgres) CountLikes(ctx context.Context, artifactID uuid.UUID) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM artifact_likes WHERE artifact_id=$1`, artifactID).Scan(&n)
	return n, err
}

func (p *Postgres) HasLiked(ctx context.Context, artifactID, userID uuid.UUID) (bool, error) {
	if userID == uuid.Nil {
		return false, nil
	}
	var ok bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM artifact_likes WHERE artifact_id=$1 AND user_id=$2)`,
		artifactID, userID).Scan(&ok)
	return ok, err
}

// LikeCountsBatch and MyLikedSet avoid an N+1 when drawing a grid.
func (p *Postgres) LikeCountsBatch(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx,
		`SELECT artifact_id, COUNT(*) FROM artifact_likes WHERE artifact_id = ANY($1) GROUP BY artifact_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (p *Postgres) MyLikedSet(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if userID == uuid.Nil || len(ids) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx,
		`SELECT artifact_id FROM artifact_likes WHERE user_id=$1 AND artifact_id = ANY($2)`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// OpenThreadCountsBatch feeds the "3 to fix" badge on cards.
func (p *Postgres) OpenThreadCountsBatch(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx,
		`SELECT artifact_id, COUNT(*) FROM comment_threads
		  WHERE artifact_id = ANY($1) AND status='open' GROUP BY artifact_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
