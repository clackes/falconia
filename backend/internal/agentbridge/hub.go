// Package agentbridge connects the web interface to the agent running on the
// person's own computer.
//
// The reason it exists is economic before it is technical. Calling a model
// with an API key is pay-per-use; people who code all day already pay for a
// Claude, ChatGPT/Codex or Gemini subscription, and that subscription covers
// running the official binary on their own machine. What it does NOT cover is
// passing those credentials through someone else's server: the providers'
// terms forbid it explicitly, and third-party clients that tried were blocked.
//
// So credentials never pass through here. We do not see them, store them or
// forward them. The binary stays the official one, installed and signed in by
// the person, on their computer. This server only queues a request and relays
// back what the agent prints. That is how one Falconia instance works with
// everyone's subscription at once, whichever vendor each person pays.
//
// The transport is long-polling, not a websocket. The agent sits on someone's
// laptop, often behind a corporate VPN: an outgoing connection that waits
// twenty-five seconds gets through any proxy, an incoming websocket does not.
//
// State lives in memory, not in the database: a work session does not survive
// a backend restart, and it should not. If the server restarts, the agent
// notices on its next poll and reconnects.
package agentbridge

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNoBridge = errors.New("no agent connected")
	ErrNoJob    = errors.New("unknown request")
)

// Event types the agent sends back.
const (
	EventStatus = "status" // the bridge picked up the request
	EventText   = "text"   // text produced by the model
	EventTool   = "tool"   // the agent used a tool
	EventError  = "error"
	EventDone   = "done"
)

type Event struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Tool     string `json:"tool,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	// URL appears when the agent publishes something: the bridge spots it in
	// the output and isolates it, so the interface can offer a button instead
	// of making the person hunt for the link in the text.
	URL string `json:"url,omitempty"`
}

// Job is a request in progress.
type Job struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Prompt    string
	CreatedAt time.Time

	mu       sync.Mutex
	history  []Event
	finished bool
	// changed is closed and replaced on every event: the simplest way to wake
	// several readers at once while honoring their contexts.
	changed chan struct{}
	taken   bool
}

func newJob(userID uuid.UUID, prompt string) *Job {
	return &Job{
		ID: uuid.New(), UserID: userID, Prompt: prompt,
		CreatedAt: time.Now(), changed: make(chan struct{}),
	}
}

// Publish records an event and wakes up the readers.
func (j *Job) Publish(e Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.finished {
		return
	}
	j.history = append(j.history, e)
	if e.Type == EventDone || e.Type == EventError {
		j.finished = true
	}
	close(j.changed)
	j.changed = make(chan struct{})
}

// Since returns the events from the given index on, waiting for some to
// arrive if there are none yet.
//
// No event is lost: a browser reconnecting mid-request resumes from the index
// it has and gets everything it missed.
func (j *Job) Since(ctx context.Context, from int) ([]Event, bool, error) {
	for {
		j.mu.Lock()
		if from < len(j.history) {
			out := append([]Event(nil), j.history[from:]...)
			done := j.finished
			j.mu.Unlock()
			return out, done, nil
		}
		if j.finished {
			j.mu.Unlock()
			return nil, true, nil
		}
		wait := j.changed
		j.mu.Unlock()

		select {
		case <-wait:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
}

func (j *Job) Finished() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.finished
}

// Bridge is a connected agent: one per person.
type Bridge struct {
	UserID   uuid.UUID
	Runner   string // "claude" | "codex" | "gemini" | a custom command name
	Host     string
	Dir      string
	LastSeen time.Time

	queue chan *Job
}

type Hub struct {
	mu      sync.Mutex
	bridges map[uuid.UUID]*Bridge
	jobs    map[uuid.UUID]*Job
}

func NewHub() *Hub {
	h := &Hub{bridges: map[uuid.UUID]*Bridge{}, jobs: map[uuid.UUID]*Job{}}
	go h.sweep()
	return h
}

// staleAfter is how long an agent may stay silent before it counts as
// disconnected. It must exceed the long-poll wait, or a perfectly alive agent
// that is just waiting would look absent.
const staleAfter = 90 * time.Second

// Poll is the call the agent repeats forever: it announces itself and waits
// for something to do.
func (h *Hub) Poll(ctx context.Context, userID uuid.UUID, runner, host, dir string, wait time.Duration) (*Job, error) {
	h.mu.Lock()
	b, ok := h.bridges[userID]
	if !ok {
		// A buffered queue: if the person sends two requests in a row while the
		// agent is still working, the second waits instead of vanishing.
		b = &Bridge{UserID: userID, queue: make(chan *Job, 8)}
		h.bridges[userID] = b
	}
	b.Runner, b.Host, b.Dir, b.LastSeen = runner, host, dir, time.Now()
	queue := b.queue
	h.mu.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case job := <-queue:
		job.mu.Lock()
		job.taken = true
		job.mu.Unlock()
		return job, nil
	case <-timer.C:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Disconnect removes the agent when it shuts down gracefully.
func (h *Hub) Disconnect(userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.bridges, userID)
}

// Status tells the interface whether an agent is on the other end.
func (h *Hub) Status(userID uuid.UUID) (*Bridge, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.bridges[userID]
	if !ok || time.Since(b.LastSeen) > staleAfter {
		return nil, false
	}
	copied := *b
	return &copied, true
}

// Submit queues a request for a person's agent.
func (h *Hub) Submit(userID uuid.UUID, prompt string) (*Job, error) {
	h.mu.Lock()
	b, ok := h.bridges[userID]
	if !ok || time.Since(b.LastSeen) > staleAfter {
		h.mu.Unlock()
		return nil, ErrNoBridge
	}
	job := newJob(userID, prompt)
	h.jobs[job.ID] = job
	queue := b.queue
	h.mu.Unlock()

	select {
	case queue <- job:
		return job, nil
	default:
		// The queue is full: the agent is eight requests behind, and queuing a
		// ninth would only make it wait longer without saying so.
		h.mu.Lock()
		delete(h.jobs, job.ID)
		h.mu.Unlock()
		return nil, errors.New("the agent has too many queued requests")
	}
}

// Job finds a request, checking that it belongs to whoever asks.
func (h *Hub) Job(userID, jobID uuid.UUID) (*Job, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	j, ok := h.jobs[jobID]
	if !ok || j.UserID != userID {
		return nil, ErrNoJob
	}
	return j, nil
}

// sweep drops old requests and agents that stopped checking in.
func (h *Hub) sweep() {
	tick := time.NewTicker(time.Minute)
	for range tick.C {
		cutoff := time.Now().Add(-30 * time.Minute)
		h.mu.Lock()
		for id, j := range h.jobs {
			if j.CreatedAt.Before(cutoff) {
				delete(h.jobs, id)
			}
		}
		for id, b := range h.bridges {
			if time.Since(b.LastSeen) > staleAfter {
				delete(h.bridges, id)
			}
		}
		h.mu.Unlock()
	}
}
