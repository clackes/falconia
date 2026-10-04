package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/clackes/falconia/backend/internal/agents"
	"github.com/clackes/falconia/backend/internal/ai"
)

// aiChat is the built-in chat: the person brings their own provider key, we
// bring the tools.
//
// The key travels in the request body and is never written anywhere. The
// stream is SSE with one Flush per chunk, and cancellation follows
// r.Context(): if the viewer closes the tab, the provider request dies with it
// instead of going on spending their credit.
func (a *api) aiChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Provider string       `json:"provider"`
		Model    string       `json:"model"`
		APIKey   string       `json:"api_key"`
		System   string       `json:"system"`
		BaseURL  string       `json:"base_url"`
		Messages []ai.Message `json:"messages"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	provider := strings.ToLower(in.Provider)
	// A local model needs no key: Ollama and LM Studio accept anything.
	if (strings.TrimSpace(in.APIKey) == "" && provider != "local" && provider != "custom") ||
		strings.TrimSpace(in.Model) == "" || len(in.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "provider, model, api_key and messages are required")
		return
	}

	f, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	sys := ai.SystemPrompt
	if s := strings.TrimSpace(in.System); s != "" {
		sys = s + "\n\n" + sys
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Without this nginx buffers the stream and the text arrives all at once
	// at the end, the opposite of what is needed.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()

	// Whatever the chat publishes is attributed to the model that wrote it.
	ctx := agents.With(r.Context(), agents.Normalize("chat:"+in.Model))
	toolDefs, exec := ai.BuildTools(a.deps.Store, a.publisher(), a.deps.AppBaseURL, identityFrom(r).User)
	emit := func(ev ai.Event) {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: chunk\ndata: %s\n\n", b)
		f.Flush()
	}

	var err error
	switch provider {
	case "anthropic":
		err = ai.RunAnthropic(ctx, in.APIKey, in.Model, in.Messages, sys, toolDefs, exec, emit)
	case "openai":
		err = ai.RunOpenAICompat(ctx, "https://api.openai.com/v1", in.APIKey, in.Model, in.Messages, sys, toolDefs, exec, emit, nil)
	case "openrouter":
		err = ai.RunOpenAICompat(ctx, "https://openrouter.ai/api/v1", in.APIKey, in.Model, in.Messages, sys, toolDefs, exec, emit,
			map[string]string{"HTTP-Referer": a.deps.AppBaseURL, "X-Title": "Falconia"})
	case "gemini":
		// Google exposes an OpenAI-compatible endpoint, tool calls included.
		err = ai.RunOpenAICompat(ctx, "https://generativelanguage.googleapis.com/v1beta/openai", in.APIKey, in.Model, in.Messages, sys, toolDefs, exec, emit, nil)
	case "local", "custom":
		if in.BaseURL == "" {
			err = errors.New("this provider needs a base_url")
		} else {
			err = ai.RunOpenAICompat(ctx, in.BaseURL, orDefault(in.APIKey, "none"), in.Model, in.Messages, sys, toolDefs, exec, emit, nil)
		}
	default:
		err = fmt.Errorf("unknown provider: %s", in.Provider)
	}

	// A finished context means the viewer cancelled the request: not an error
	// worth reporting to anyone.
	if err != nil && ctx.Err() == nil {
		emit(ai.Event{Kind: "error", Text: err.Error()})
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
