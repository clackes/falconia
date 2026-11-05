package handlers

import (
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// dummyHash is compared against when a login names an account that does not
// exist, so that both outcomes cost one bcrypt.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("falconia-timing-equalizer"), bcrypt.DefaultCost)

// failureLimiter counts recent failures per key and refuses once there are too
// many. It lives in memory: with several replicas the limit is per replica,
// which still turns an online guessing attack from thousands of tries a minute
// into a handful.
type failureLimiter struct {
	max    int
	window time.Duration

	mu      sync.Mutex
	entries map[string]*failures
}

type failures struct {
	count int
	first time.Time
}

func newFailureLimiter(max int, window time.Duration) *failureLimiter {
	return &failureLimiter{max: max, window: window, entries: map[string]*failures{}}
}

var loginLimiter = newFailureLimiter(10, 15*time.Minute)

func (l *failureLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return true
	}
	if time.Since(e.first) > l.window {
		delete(l.entries, key)
		return true
	}
	return e.count < l.max
}

func (l *failureLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.first) > l.window {
		// Sweep on insert rather than on a timer: the map only grows when
		// someone fails, and that is when it is worth cleaning.
		if len(l.entries) > 10000 {
			for k, v := range l.entries {
				if now.Sub(v.first) > l.window {
					delete(l.entries, k)
				}
			}
			// Still huge means someone is spraying made-up keys: forget an
			// arbitrary half rather than let memory grow without bound.
			if len(l.entries) > 50000 {
				n := 0
				for k := range l.entries {
					delete(l.entries, k)
					if n++; n >= 25000 {
						break
					}
				}
			}
		}
		l.entries[key] = &failures{count: 1, first: now}
		return
	}
	e.count++
}

func (l *failureLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}
