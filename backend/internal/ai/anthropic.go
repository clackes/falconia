package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RunAnthropic runs a chat loop on the Anthropic Messages API with tool use,
// publishing events through emit(). The frontend reads the stream over SSE.
//
// The caller's message format is {role: "user"|"assistant", content: string}.
// Internally tool_use/tool_result are added as structured blocks.
func RunAnthropic(ctx context.Context, apiKey, model string, messages []Message, sys string, tools []ToolDef, exec Executor, emit func(Event)) error {
	httpc := &http.Client{}

	// Internal conversation: messages whose content is an array of blocks
	// (text|tool_use|tool_result), starting from the plain text turns.
	convo := make([]anthropicMsg, 0, len(messages))
	for _, m := range messages {
		convo = append(convo, anthropicMsg{Role: m.Role, Content: []anthropicBlock{{Type: "text", Text: m.Content}}})
	}

	// Anthropic tool schema.
	toolSchemas := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		toolSchemas = append(toolSchemas, map[string]any{
			"name": t.Name, "description": t.Description, "input_schema": t.InputSchema,
		})
	}

	maxIter := 8
	for iter := 0; iter < maxIter; iter++ {
		body := map[string]any{
			"model":      model,
			"max_tokens": 16384,
			"system":     sys,
			"messages":   convo,
			"tools":      toolSchemas,
			"stream":     true,
		}
		buf, _ := json.Marshal(body)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := httpc.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			return fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(b))
		}

		// Rebuild the assistant blocks (text + tool_use) while streaming.
		assistantBlocks := []anthropicBlock{}
		curType := ""
		curText := strings.Builder{}
		curToolName := ""
		curToolID := ""
		curToolJSON := strings.Builder{}

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		var currentEvent string
		var stopReason string
		for sc.Scan() {
			if ctx.Err() != nil {
				resp.Body.Close()
				return ctx.Err()
			}
			line := sc.Text()
			if line == "" {
				currentEvent = ""
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			switch currentEvent {
			case "content_block_start":
				var e struct {
					ContentBlock struct {
						Type, Text, Name, ID string
					} `json:"content_block"`
				}
				_ = json.Unmarshal([]byte(data), &e)
				curType = e.ContentBlock.Type
				if curType == "text" {
					curText.Reset()
				} else if curType == "tool_use" {
					curToolName = e.ContentBlock.Name
					curToolID = e.ContentBlock.ID
					curToolJSON.Reset()
				}
			case "content_block_delta":
				var e struct {
					Delta struct {
						Type, Text, PartialJSON string
					} `json:"delta"`
				}
				_ = json.Unmarshal([]byte(data), &e)
				if e.Delta.Type == "text_delta" && e.Delta.Text != "" {
					curText.WriteString(e.Delta.Text)
					emit(Event{Kind: "text", Text: e.Delta.Text})
				} else if e.Delta.Type == "input_json_delta" {
					curToolJSON.WriteString(e.Delta.PartialJSON)
				}
			case "content_block_stop":
				if curType == "text" {
					assistantBlocks = append(assistantBlocks, anthropicBlock{Type: "text", Text: curText.String()})
				} else if curType == "tool_use" {
					inp := json.RawMessage(curToolJSON.String())
					if len(inp) == 0 {
						inp = json.RawMessage("{}")
					}
					assistantBlocks = append(assistantBlocks, anthropicBlock{
						Type: "tool_use", ID: curToolID, Name: curToolName, Input: inp,
					})
				}
				curType = ""
			case "message_delta":
				var e struct {
					Delta struct{ StopReason string } `json:"delta"`
				}
				_ = json.Unmarshal([]byte(data), &e)
				if e.Delta.StopReason != "" {
					stopReason = e.Delta.StopReason
				}
			case "message_stop":
				// end of one turn
			}
		}
		resp.Body.Close()
		if err := sc.Err(); err != nil {
			return err
		}

		// Record the assistant turn in the conversation.
		convo = append(convo, anthropicMsg{Role: "assistant", Content: assistantBlocks})

		// If the model asked for tools: run every tool_use and send again.
		toolUses := []anthropicBlock{}
		for _, b := range assistantBlocks {
			if b.Type == "tool_use" {
				toolUses = append(toolUses, b)
			}
		}
		if len(toolUses) == 0 || stopReason != "tool_use" {
			// no more tools requested: done
			emit(Event{Kind: "done"})
			return nil
		}

		// Run the tools and prepare the "user" turn with the tool_results.
		resultBlocks := []anthropicBlock{}
		for _, tu := range toolUses {
			emit(Event{Kind: "tool_call", ToolID: tu.ID, ToolName: tu.Name, ToolInput: tu.Input})
			output, err := exec(ctx, tu.Name, tu.Input)
			resBlock := anthropicBlock{Type: "tool_result", ToolUseID: tu.ID}
			if err != nil {
				resBlock.Content = "ERROR: " + err.Error()
				resBlock.IsError = true
				emit(Event{Kind: "tool_result", ToolID: tu.ID, ToolName: tu.Name, IsError: true, ResultText: err.Error()})
			} else {
				resBlock.Content = output
				emit(Event{Kind: "tool_result", ToolID: tu.ID, ToolName: tu.Name, ResultText: output})
			}
			resultBlocks = append(resultBlocks, resBlock)
		}
		convo = append(convo, anthropicMsg{Role: "user", Content: resultBlocks})
	}

	emit(Event{Kind: "error", Text: "tool loop limit reached"})
	return nil
}

type anthropicMsg struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}
type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}
