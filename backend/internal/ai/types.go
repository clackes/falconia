package ai

import "encoding/json"

type Message struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
}

type Event struct {
	Kind       string          `json:"kind"` // text | tool_call | tool_result | done | error
	Text       string          `json:"text,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ResultText string          `json:"result_text,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
}
