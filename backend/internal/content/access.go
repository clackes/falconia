package content

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/storage"
)

// accessTTL is how stale an access decision may be. An artifact page loads
// dozens of assets with the same grant; checking the database for each would
// cost more than the page, while thirty seconds is short enough that a
// revocation is effectively immediate for whoever made it.
const accessTTL = 30 * time.Second

type accessCache struct {
	mu sync.Mutex
	m  map[string]accessEntry
}

type accessEntry struct {
	ok bool
	at time.Time
}

func (c *accessCache) get(key string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.m[key]
	if !found || time.Since(e.at) > accessTTL {
		return false, false
	}
	return e.ok, true
}

func (c *accessCache) put(key string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]accessEntry{}
	}
	if len(c.m) > 20000 {
		for k, e := range c.m {
			if time.Since(e.at) > accessTTL {
				delete(c.m, k)
			}
		}
		if len(c.m) > 20000 {
			c.m = map[string]accessEntry{}
		}
	}
	c.m[key] = accessEntry{ok: ok, at: time.Now()}
}

// stillAllowed re-checks a verified grant against the current state.
//
// The content server still knows nothing about sessions: it holds no token
// and reads no cookie. It asks the same question the application asked when
// it minted the grant — may this user, or this link, see this artifact — and
// only the answer, not the identity of the browser, matters.
func (s *Server) stillAllowed(ctx context.Context, grantTok string, g *Grant) (bool, error) {
	if ok, found := s.access.get(grantTok); found {
		return ok, nil
	}
	var ok bool
	var err error
	if g.UserID == uuid.Nil {
		ok, err = s.Store.LinkGrantValid(ctx, g.ArtifactID, g.LinkTag)
	} else {
		ok, err = s.userAllowed(ctx, g)
	}
	if err != nil {
		return false, err
	}
	s.access.put(grantTok, ok)
	return ok, nil
}

func (s *Server) userAllowed(ctx context.Context, g *Grant) (bool, error) {
	u, err := s.Store.GetUser(ctx, g.UserID)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil // the account is gone: so is its access
	}
	if err != nil {
		return false, err
	}
	role, err := s.Store.ResolveAccess(ctx, g.ArtifactID, u.ID, s.Store.AccessEmail(u))
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrForbidden) {
		return false, nil
	}
	if err != nil {
		// A database hiccup is not a revocation: report it, do not cache a
		// "no" that would lock the reader out for the next thirty seconds.
		return false, err
	}
	return role.CanRead(), nil
}
