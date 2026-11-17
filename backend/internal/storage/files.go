package storage

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// FileSpec describes a file joining a version. The bytes are already in the
// object store: only their hash travels here.
type FileSpec struct {
	Path        string
	ContentType string
	Size        int64
	SHA256      string
}

// ArtifactFile is a file that belongs to a version.
type ArtifactFile struct {
	ID          int64
	VersionID   int64
	Path        string
	ContentType string
	SizeBytes   int64
	SHA256      string
}

// NormalizePath reduces a deploy path to the bundle's canonical form.
//
// A deploy comes from a CLI or an agent on any machine, and what it calls a
// "relative path" may contain Windows backslashes, a stray "./", a leading
// slash or a "..". The last one is the truly dangerous one: without this
// function a file named "../../etc/passwd" would become a bundle key and then
// a served path. It returns "" for anything that must be rejected.
func NormalizePath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	// path.Clean resolves "." and ".." internally; if the path still tries to
	// climb out after cleaning, it is not a valid bundle path.
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ""
		}
	}
	if len(p) > 512 {
		return ""
	}
	return p
}

func insertFilesTx(ctx context.Context, tx pgx.Tx, versionID int64, files []FileSpec) error {
	for _, f := range files {
		clean := NormalizePath(f.Path)
		if clean == "" {
			continue
		}
		ct := f.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO artifact_files (version_id, path, content_type, size_bytes, sha256)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (version_id, path) DO NOTHING`,
			versionID, clean, ct, f.Size, f.SHA256); err != nil {
			return err
		}
	}
	return nil
}

// blobLockClass namespaces the per-blob advisory locks (two-key form: class,
// hash of the sha).
const blobLockClass = 0x53464102

// StoreBlob writes a blob's bytes through write and records its row, under a
// per-blob lock shared with CollectBlob.
//
// Order matters: bytes first, then the row. A blob without a row is garbage
// the collector picks up; a row without bytes is a broken artifact that serves
// a 500 to whoever opens it. The lock is what makes that order hold against
// the collector: without it, the collector could delete the bytes right after
// write decided they were already there.
func (p *Postgres) StoreBlob(ctx context.Context, sha string, size int64, write func() error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, blobLockClass, sha); err != nil {
		return err
	}
	if err := write(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO blobs (sha256, size_bytes) VALUES ($1,$2)
		 ON CONFLICT (sha256) DO UPDATE SET last_used_at = NOW()`, sha, size); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// HasBlobRow reports whether the database knows a blob. It is the source of
// truth for "do we have it": bytes without a row may be about to be collected.
func (p *Postgres) HasBlobRow(ctx context.Context, sha string) (bool, error) {
	var ok bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM blobs WHERE sha256=$1)`, sha).Scan(&ok)
	return ok, err
}

// BlobSizes returns the stored size of each known hash.
//
// A deploy manifest declares sizes, but the declaration comes from the client:
// the size recorded when the bytes were hashed is the one that is true, and it
// is the one the content server will put in Content-Length.
func (p *Postgres) BlobSizes(ctx context.Context, shas []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(shas) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx,
		`SELECT sha256, size_bytes FROM blobs WHERE sha256 = ANY($1::text[])`, shas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sha string
		var n int64
		if err := rows.Scan(&sha, &n); err != nil {
			return nil, err
		}
		out[sha] = n
	}
	return out, rows.Err()
}

// MissingBlobs filters the hashes the instance does not have yet.
//
// This is what makes a redeploy instant: the CLI sends the list of the
// bundle's hashes, gets back only the unknown ones, and uploads those. A fifty
// megabyte bundle where only index.html changed transfers a few kilobytes.
func (p *Postgres) MissingBlobs(ctx context.Context, shas []string) ([]string, error) {
	if len(shas) == 0 {
		return []string{}, nil
	}
	// Asking about a blob is a sign someone is about to use it: refresh it, so
	// the collector does not delete it between this answer and the deploy.
	if _, err := p.pool.Exec(ctx, `
		UPDATE blobs SET last_used_at = NOW()
		 WHERE sha256 = ANY($1::text[]) AND last_used_at < NOW() - INTERVAL '1 minute'`, shas); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT s FROM UNNEST($1::text[]) AS s
		 WHERE NOT EXISTS (SELECT 1 FROM blobs b WHERE b.sha256 = s)`, shas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) ListFiles(ctx context.Context, versionID int64) ([]ArtifactFile, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, version_id, path, content_type, size_bytes, sha256
		   FROM artifact_files WHERE version_id=$1 ORDER BY path`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArtifactFile{}
	for rows.Next() {
		var f ArtifactFile
		if err := rows.Scan(&f.ID, &f.VersionID, &f.Path, &f.ContentType, &f.SizeBytes, &f.SHA256); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFile resolves a path inside a version.
func (p *Postgres) GetFile(ctx context.Context, versionID int64, filePath string) (*ArtifactFile, error) {
	clean := NormalizePath(filePath)
	if clean == "" {
		return nil, ErrNotFound
	}
	var f ArtifactFile
	err := p.pool.QueryRow(ctx,
		`SELECT id, version_id, path, content_type, size_bytes, sha256
		   FROM artifact_files WHERE version_id=$1 AND path=$2`, versionID, clean,
	).Scan(&f.ID, &f.VersionID, &f.Path, &f.ContentType, &f.SizeBytes, &f.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &f, err
}

// ---------------- Garbage collection ----------------

// OrphanBlobs lists blobs no file references any more.
//
// The age margin is not pedantry: a deploy uploads bytes before writing the
// artifact_files rows, so there is a window in which a freshly uploaded blob
// looks orphaned. Deleting it in that window would break the deploy in
// progress.
func (p *Postgres) OrphanBlobs(ctx context.Context, olderThanHours int, limit int) ([]string, error) {
	if olderThanHours <= 0 {
		olderThanHours = 24
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := p.pool.Query(ctx, `
		SELECT b.sha256 FROM blobs b
		 WHERE b.last_used_at < NOW() - make_interval(hours => $1)
		   AND NOT EXISTS (SELECT 1 FROM artifact_files f WHERE f.sha256 = b.sha256)
		 LIMIT $2`, olderThanHours, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CollectBlob deletes one orphan blob, row and bytes, if it is still
// collectable, and reports whether it did.
//
// The row goes first, conditionally, and the bytes only once that succeeded,
// all under the lock StoreBlob takes. A version that started using the blob in
// the meantime makes the delete match nothing (or fail on the RESTRICT
// foreign key) and the bytes stay. If deleting the bytes fails, the row is
// kept too, so the next run tries again instead of forgetting about them.
func (p *Postgres) CollectBlob(ctx context.Context, sha string, olderThanHours int, deleteBytes func() error) (bool, error) {
	if olderThanHours <= 0 {
		olderThanHours = 24
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, blobLockClass, sha); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM blobs b
		 WHERE b.sha256 = $1
		   AND b.last_used_at < NOW() - make_interval(hours => $2)
		   AND NOT EXISTS (SELECT 1 FROM artifact_files f WHERE f.sha256 = b.sha256)`, sha, olderThanHours)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return false, nil // referenced after all: keep it
		}
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if err := deleteBytes(); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// StorageStats feeds the admin panel.
type StorageStats struct {
	Blobs      int64
	Bytes      int64
	Files      int64
	Artifacts  int64
	Versions   int64
	Users      int64
	Orgs       int64
	OpenThread int64
}

func (p *Postgres) Stats(ctx context.Context) (*StorageStats, error) {
	s := &StorageStats{}
	err := p.pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM blobs),
		       (SELECT COALESCE(SUM(size_bytes),0) FROM blobs),
		       (SELECT COUNT(*) FROM artifact_files),
		       (SELECT COUNT(*) FROM artifacts),
		       (SELECT COUNT(*) FROM artifact_versions),
		       (SELECT COUNT(*) FROM users),
		       (SELECT COUNT(*) FROM orgs),
		       (SELECT COUNT(*) FROM comment_threads WHERE status='open')`,
	).Scan(&s.Blobs, &s.Bytes, &s.Files, &s.Artifacts, &s.Versions, &s.Users, &s.Orgs, &s.OpenThread)
	return s, err
}
