// Package ai is the built-in chat: a proxy to the person's own model, with
// provider keys that stay theirs.
//
// Keys arrive with every request and are never persisted: whoever installs
// Falconia on their server does not become the keeper of their colleagues' API
// keys, and so is not responsible when one leaks.
//
// The tools are the same ones the instance exposes over MCP: this file only
// adapts the shape the provider loops expect to the shared Runner in
// internal/tools.
package ai

import (
	"context"
	"encoding/json"

	"github.com/clackes/falconia/backend/internal/publish"
	"github.com/clackes/falconia/backend/internal/storage"
	"github.com/clackes/falconia/backend/internal/tools"
)

// ToolDef is the shape the provider loops expect.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Executor runs a tool and returns the JSON to report back to the model.
type Executor func(ctx context.Context, name string, args json.RawMessage) (string, error)

// SystemPrompt is what the model reads before anything else.
const SystemPrompt = tools.Instructions

// BuildTools binds the shared tools to a user.
//
// The chat runs on a browser session, not an API key: whoever uses it is
// already in front of the screen, and "via" records it as a web action. The
// model's name travels in the context and ends up on every version it writes.
func BuildTools(store *storage.Postgres, pub *publish.Publisher, appBaseURL string, user *storage.User) ([]ToolDef, Executor) {
	runner := &tools.Runner{Store: store, Publisher: pub, AppBaseURL: appBaseURL, User: user, Via: "web"}

	defs := make([]ToolDef, 0, len(tools.All()))
	for _, d := range tools.All() {
		defs = append(defs, ToolDef{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema})
	}
	return defs, runner.Call
}
