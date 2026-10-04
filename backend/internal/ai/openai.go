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

// RunOpenAICompat runs a tool-use chat against any OpenAI-compatible API:
// OpenAI itself, OpenRouter, Ollama, LM Studio, vLLM. Pass endpointBase.
func RunOpenAICompat(ctx context.Context, endpointBase, apiKey, model string, messages []Message, sys string, tools []ToolDef, exec Executor, emit func(Event), extraHeaders map[string]string) error {
	if endpointBase == "" {
		endpointBase = "https://api.openai.com/v1"
	}
	httpc := &http.Client{}

	// Build the initial message list.
	convo := []map[string]any{}
	if sys != "" {
		convo = append(convo, map[string]any{"role": "system", "content": sys})
	}
	for _, m := range messages {
		convo = append(convo, map[string]any{"role": m.Role, "content": m.Content})
	}

	// Tool schema.
	toolSchemas := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		toolSchemas = append(toolSchemas, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}

	maxIter := 8
	for iter := 0; iter < maxIter; iter++ {
		body := map[string]any{
			"model":    model,
			"messages": convo,
			"tools":    toolSchemas,
			"stream":   true,
		}
		buf, _ := json.Marshal(body)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointBase+"/chat/completions", bytes.NewReader(buf))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "text/event-stream")
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}

		resp, err := httpc.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			return fmt.Errorf("openai %d: %s", resp.StatusCode, string(b))
		}

		// Accumulate assistant content and tool_calls.
		var content strings.Builder
		toolCallsByIndex := map[int]*ocToolCall{}
		var finishReason string

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			if ctx.Err() != nil {
				resp.Body.Close()
				return ctx.Err()
			}
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var e struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Type     string `json:"type"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &e); err != nil {
				continue
			}
			for _, c := range e.Choices {
				if c.Delta.Content != "" {
					content.WriteString(c.Delta.Content)
					emit(Event{Kind: "text", Text: c.Delta.Content})
				}
				for _, tc := range c.Delta.ToolCalls {
					t, ok := toolCallsByIndex[tc.Index]
					if !ok {
						t = &ocToolCall{}
						toolCallsByIndex[tc.Index] = t
					}
					if tc.ID != "" {
						t.ID = tc.ID
					}
					if tc.Function.Name != "" {
						t.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						t.Args.WriteString(tc.Function.Arguments)
					}
				}
				if c.FinishReason != "" {
					finishReason = c.FinishReason
				}
			}
		}
		resp.Body.Close()
		if err := sc.Err(); err != nil {
			return err
		}

		// Add the assistant turn to the conversation.
		assistantMsg := map[string]any{"role": "assistant", "content": content.String()}
		if len(toolCallsByIndex) > 0 {
			tcSlice := make([]map[string]any, 0, len(toolCallsByIndex))
			for i := 0; i < len(toolCallsByIndex); i++ {
				t := toolCallsByIndex[i]
				if t == nil {
					continue
				}
				tcSlice = append(tcSlice, map[string]any{
					"id":       t.ID,
					"type":     "function",
					"function": map[string]any{"name": t.Name, "arguments": t.Args.String()},
				})
			}
			assistantMsg["tool_calls"] = tcSlice
		}
		convo = append(convo, assistantMsg)

		if finishReason != "tool_calls" || len(toolCallsByIndex) == 0 {
			emit(Event{Kind: "done"})
			return nil
		}

		// Run the tool calls and insert the results.
		for i := 0; i < len(toolCallsByIndex); i++ {
			t := toolCallsByIndex[i]
			if t == nil {
				continue
			}
			args := json.RawMessage(t.Args.String())
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			emit(Event{Kind: "tool_call", ToolID: t.ID, ToolName: t.Name, ToolInput: args})
			out, err := exec(ctx, t.Name, args)
			if err != nil {
				emit(Event{Kind: "tool_result", ToolID: t.ID, ToolName: t.Name, IsError: true, ResultText: err.Error()})
				convo = append(convo, map[string]any{"role": "tool", "tool_call_id": t.ID, "content": "ERROR: " + err.Error()})
			} else {
				emit(Event{Kind: "tool_result", ToolID: t.ID, ToolName: t.Name, ResultText: out})
				convo = append(convo, map[string]any{"role": "tool", "tool_call_id": t.ID, "content": out})
			}
		}
	}
	emit(Event{Kind: "error", Text: "tool loop limit reached"})
	return nil
}

type ocToolCall struct {
	ID   string
	Name string
	Args strings.Builder
}
