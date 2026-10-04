package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/agentbridge"
)

// Routes of the bridge to the local agent.
//
// Three parties talk: `falconia agent` on the person's computer long-polls
// /poll and sends output back to /events; the browser drops a request on /runs
// and streams it from /runs/{id}/stream.
//
// No model credential travels through here. The server does not know which
// subscription pays for the work — Claude, ChatGPT/Codex, Gemini — and must
// not know: that ignorance is what keeps this within every provider's terms,
// besides being safer.

// agentStatus tells the interface whether an agent is on the other end.
func (a *api) agentStatus(w http.ResponseWriter, r *http.Request) {
	b, ok := a.deps.Agents.Status(identityFrom(r).User.ID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": true,
		"runner":    b.Runner,
		"host":      b.Host,
		"dir":       b.Dir,
		"last_seen": b.LastSeen,
	})
}

// agentPoll is the call `falconia agent` repeats forever.
//
// It hangs for up to twenty-five seconds, on purpose: an outgoing connection
// that waits gets through any corporate proxy, while an incoming websocket to
// someone's laptop gets nowhere.
func (a *api) agentPoll(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r)
	q := r.URL.Query()

	wait := 25 * time.Second
	if n, err := strconv.Atoi(q.Get("wait")); err == nil && n > 0 && n <= 60 {
		wait = time.Duration(n) * time.Second
	}

	job, err := a.deps.Agents.Poll(r.Context(), id.User.ID,
		q.Get("runner"), q.Get("host"), q.Get("dir"), wait)
	if err != nil || job == nil {
		// 204 means "nothing to do, call again": a normal outcome, not an
		// error, and the CLI must not print anything about it.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": job.ID, "prompt": job.Prompt, "created_at": job.CreatedAt,
	})
}

func (a *api) agentDisconnect(w http.ResponseWriter, r *http.Request) {
	a.deps.Agents.Disconnect(identityFrom(r).User.ID)
	w.WriteHeader(http.StatusNoContent)
}

// agentEvents receives from the bridge what the agent printed.
func (a *api) agentEvents(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	job, err := a.deps.Agents.Job(identityFrom(r).User.ID, jobID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown request")
		return
	}
	var in struct {
		Events []agentbridge.Event `json:"events"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	for _, e := range in.Events {
		switch e.Type {
		case agentbridge.EventStatus, agentbridge.EventText,
			agentbridge.EventTool, agentbridge.EventError, agentbridge.EventDone:
			job.Publish(e)
		default:
			// An unknown type is shown as text instead of being dropped: better
			// to show something raw than to lose the answer because an agent
			// changed its output format.
			job.Publish(agentbridge.Event{Type: agentbridge.EventText, Text: e.Text})
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentRun is the browser asking its own agent for something.
func (a *api) agentRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt string `json:"prompt"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(in.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, "the request is empty")
		return
	}
	job, err := a.deps.Agents.Submit(identityFrom(r).User.ID, in.Prompt)
	if errors.Is(err, agentbridge.ErrNoBridge) {
		writeErr(w, http.StatusConflict,
			"no agent connected: open a terminal and run `falconia agent`")
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": job.ID})
}

// agentStream delivers a request's events to the browser, as SSE.
func (a *api) agentStream(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	job, err := a.deps.Agents.Job(identityFrom(r).User.ID, jobID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown request")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	// A reconnecting browser says where to resume from, and loses nothing of
	// what happened while it was away.
	from, _ := strconv.Atoi(r.URL.Query().Get("from"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		events, done, err := job.Since(r.Context(), from)
		if err != nil {
			return // the browser went away
		}
		for _, e := range events {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\nevent: chunk\ndata: %s\n\n", from, b)
			from++
		}
		flusher.Flush()
		if done && len(events) == 0 {
			return
		}
		if done {
			// Exit on the next round, so the last batch of events really goes out.
			continue
		}
	}
}
