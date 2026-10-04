package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/clackes/falconia/backend/internal/agents"
)

// Tracked event kinds.
const (
	EventView    = "view"
	EventDeploy  = "deploy"
	EventComment = "comment"
	EventResolve = "resolve"
	EventShare   = "share"
	EventFork    = "fork"
	EventDelete  = "delete"
	EventCreate  = "create"
	EventExport  = "export"
)

type Event struct {
	ID            int64
	ArtifactID    *uuid.UUID
	ArtifactTitle string
	ActorID       *uuid.UUID
	ActorName     string
	Kind          string
	VersionNumber *int
	Via           string
	Agent         string
	Meta          map[string]any
	CreatedAt     time.Time
}

type RecordEventInput struct {
	ArtifactID    uuid.UUID
	ActorID       uuid.UUID
	Kind          string
	VersionNumber int
	Via           string
	Meta          map[string]any
	VisitorHash   string
}

// RecordEvent writes one audit row. The agent, if any, comes from the context.
//
// Views are deduplicated per visitor and per hour by a partial unique index:
// an artifact that reloads itself every thirty seconds must not produce two
// thousand rows a day. A conflict is therefore an expected outcome, not an
// error, and is swallowed.
func (p *Postgres) RecordEvent(ctx context.Context, in RecordEventInput) error {
	var meta []byte = []byte("{}")
	if len(in.Meta) > 0 {
		if b, err := json.Marshal(in.Meta); err == nil {
			meta = b
		}
	}
	var artifactID, actorID any
	if in.ArtifactID != uuid.Nil {
		artifactID = in.ArtifactID
	}
	if in.ActorID != uuid.Nil {
		actorID = in.ActorID
	}
	var version any
	if in.VersionNumber > 0 {
		version = in.VersionNumber
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO artifact_events (artifact_id, actor_id, kind, version_number, via, agent, meta, visitor_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT DO NOTHING`,
		artifactID, actorID, in.Kind, version, sanitizeVia(in.Via), agents.From(ctx), meta, in.VisitorHash)
	return err
}

// ListEvents is an artifact's activity log: who opened it, who changed it,
// who commented, newest first.
func (p *Postgres) ListEvents(ctx context.Context, artifactID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `
		SELECT e.id, e.artifact_id, COALESCE(a.title,''), e.actor_id, COALESCE(u.display_name,''),
		       e.kind, e.version_number, e.via, e.agent, e.meta, e.created_at
		  FROM artifact_events e
		  LEFT JOIN users     u ON u.id = e.actor_id
		  LEFT JOIN artifacts a ON a.id = e.artifact_id
		 WHERE e.artifact_id = $1
		 ORDER BY e.created_at DESC LIMIT $2`, artifactID, limit)
	if err != nil {
		return nil, err
	}
	return collectEvents(rows)
}

// ListInstanceEvents feeds the admin panel.
func (p *Postgres) ListInstanceEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `
		SELECT e.id, e.artifact_id, COALESCE(a.title,''), e.actor_id, COALESCE(u.display_name,''),
		       e.kind, e.version_number, e.via, e.agent, e.meta, e.created_at
		  FROM artifact_events e
		  LEFT JOIN users     u ON u.id = e.actor_id
		  LEFT JOIN artifacts a ON a.id = e.artifact_id
		 ORDER BY e.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return collectEvents(rows)
}

func collectEvents(rows pgx.Rows) ([]Event, error) {
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var raw []byte
		if err := rows.Scan(&e.ID, &e.ArtifactID, &e.ArtifactTitle, &e.ActorID, &e.ActorName,
			&e.Kind, &e.VersionNumber, &e.Via, &e.Agent, &raw, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &e.Meta)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ArtifactStats are the numbers shown at the top of an artifact.
type ArtifactStats struct {
	Views        int `json:"views"`
	UniqueViews  int `json:"unique_views"`
	OpenThreads  int `json:"open_threads"`
	TotalThreads int `json:"total_threads"`
	Likes        int `json:"likes"`
	Versions     int `json:"versions"`
}

func (p *Postgres) ArtifactStats(ctx context.Context, artifactID uuid.UUID) (*ArtifactStats, error) {
	s := &ArtifactStats{}
	err := p.pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*)                   FROM artifact_events   WHERE artifact_id=$1 AND kind='view'),
		       (SELECT COUNT(DISTINCT visitor_hash) FROM artifact_events WHERE artifact_id=$1 AND kind='view'),
		       (SELECT COUNT(*) FROM comment_threads  WHERE artifact_id=$1 AND status='open'),
		       (SELECT COUNT(*) FROM comment_threads  WHERE artifact_id=$1),
		       (SELECT COUNT(*) FROM artifact_likes   WHERE artifact_id=$1),
		       (SELECT COUNT(*) FROM artifact_versions WHERE artifact_id=$1)`, artifactID,
	).Scan(&s.Views, &s.UniqueViews, &s.OpenThreads, &s.TotalThreads, &s.Likes, &s.Versions)
	return s, err
}
