package agentbridge

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSinceNegativeIndex(t *testing.T) {
	j := newJob(uuid.New(), "p")
	j.Publish(Event{Type: EventText, Text: "a"})
	evs, _, err := j.Since(context.Background(), -5)
	if err != nil || len(evs) != 1 {
		t.Fatalf("got %v, %v", evs, err)
	}
}

func TestDisconnectFailsQueuedJobs(t *testing.T) {
	h := &Hub{bridges: map[uuid.UUID]*Bridge{}, jobs: map[uuid.UUID]*Job{}}
	uid := uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, _ = h.Poll(ctx, uid, "claude", "host", "/", time.Millisecond)
	job, err := h.Submit(uid, "do it")
	if err != nil {
		t.Fatal(err)
	}
	h.Disconnect(uid)
	if !job.Finished() {
		t.Fatal("a queued job survived its agent's disconnect")
	}
}
