package provider

import (
	"context"
	"encoding/json"
)

// Block is one content element within a message.
// Type is one of "text", "tool_use", "tool_result".
type Block struct {
	Type    string
	Text    string          // when Type == "text"
	ID      string          // when Type == "tool_use" or "tool_result"
	Name    string          // when Type == "tool_use"
	Input   json.RawMessage // when Type == "tool_use"
	Content string          // when Type == "tool_result"
	IsError bool            // when Type == "tool_result"
}

// Message is one turn in a conversation.
type Message struct {
	Role   string // "user" | "assistant"
	Blocks []Block
}

// ToolDef describes a callable tool available to the model.
type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage // valid JSON Schema object
}

// Response is what the provider returns from a Send call.
type Response struct {
	Blocks     []Block
	StopReason string // "end_turn" | "tool_use"
	Usage      Usage
}

// Usage records token consumption for one API call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Provider abstracts an LLM backend.
type Provider interface {
	Send(ctx context.Context, system string, messages []Message, tools []ToolDef) (Response, error)
	Name() string
	Model() string
	// NativeContextWindow returns the model's intrinsic token context window.
	// Used as the effective window when config sets no context_size. Local
	// providers return a conservative floor since the real window is the
	// server's load-time setting (overridden via config context_size).
	NativeContextWindow() int
}
