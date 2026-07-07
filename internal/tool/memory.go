package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tu/tu-agent/internal/memory"
)

// MemSaveTool saves an observation to the memory store.
type MemSaveTool struct {
	store  *memory.Store
	author string
}

// NewMemSaveTool returns a MemSaveTool backed by store.
func NewMemSaveTool(store *memory.Store, author string) *MemSaveTool {
	return &MemSaveTool{store: store, author: author}
}

func (t *MemSaveTool) Name() string { return "mem_save" }
func (t *MemSaveTool) Description() string {
	return "Save an observation to project memory. Use to record decisions, discoveries, or patterns for future sessions."
}

const memSaveSchema = `{
    "type": "object",
    "properties": {
        "topic":   {"type": "string", "description": "Topic key (e.g. 'architecture/auth'). Saving the same topic again refines it and bumps its revision."},
        "content": {"type": "string", "description": "The observation to save."}
    },
    "required": ["topic", "content"]
}`

func (t *MemSaveTool) InputSchema() json.RawMessage { return json.RawMessage(memSaveSchema) }

type memSaveInput struct {
	Topic   string `json:"topic"`
	Content string `json:"content"`
}

// Run decodes the input and upserts the observation by topic key.
func (t *MemSaveTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var in memSaveInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("mem_save: decode input: %w", err)
	}
	if in.Topic == "" || in.Content == "" {
		return "", fmt.Errorf("mem_save: topic and content cannot be empty")
	}
	obs, err := t.store.Upsert(in.Topic, in.Content, memory.UpsertOpts{Source: "agent", Author: t.author})
	if err != nil {
		return "", fmt.Errorf("mem_save: persist: %w", err)
	}
	return fmt.Sprintf("Saved observation %s (topic: %q, rev:%d)", obs.ID, obs.TopicKey, obs.Revision), nil
}

// MemSearchTool searches observations by keyword.
type MemSearchTool struct{ store *memory.Store }

// NewMemSearchTool returns a MemSearchTool backed by store.
func NewMemSearchTool(store *memory.Store) *MemSearchTool { return &MemSearchTool{store: store} }

func (t *MemSearchTool) Name() string { return "mem_search" }
func (t *MemSearchTool) Description() string {
	return "Search project memory for observations matching a keyword query."
}

const memSearchSchema = `{
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "Keyword to search in topic and content."},
        "type": {"type": "string", "description": "Optional: restrict to one observation type (bug-pattern|decision|architecture|testing|reference|gotcha). With an empty query, lists all of that type."}
    },
    "required": ["query"]
}`

func (t *MemSearchTool) InputSchema() json.RawMessage { return json.RawMessage(memSearchSchema) }

type memSearchInput struct {
	Query string `json:"query"`
	Type  string `json:"type"`
}

// Run decodes the input and returns observations matching the query, optionally
// restricted to a single observation type. An empty query is allowed when a type
// is given (lists all of that type); both empty is an error.
func (t *MemSearchTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var in memSearchInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("mem_search: decode input: %w", err)
	}
	if in.Query == "" && in.Type == "" {
		return "", fmt.Errorf("mem_search: query cannot be empty")
	}
	results, _, err := t.store.Search(in.Query, in.Type, 0)
	if err != nil {
		return "", fmt.Errorf("mem_search: %w", err)
	}
	if len(results) == 0 {
		return "No observations found for query: " + in.Query, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d observation(s) found:\n", len(results))
	for _, obs := range results {
		fmt.Fprintf(&sb, "\n[%s] %s\n%s\n", obs.Title, obs.UpdatedAt.Format("2006-01-02"), obs.Content)
	}
	return sb.String(), nil
}

// MemRecentTool returns the most recent N observations.
type MemRecentTool struct{ store *memory.Store }

// NewMemRecentTool returns a MemRecentTool backed by store.
func NewMemRecentTool(store *memory.Store) *MemRecentTool { return &MemRecentTool{store: store} }

func (t *MemRecentTool) Name() string { return "mem_recent" }
func (t *MemRecentTool) Description() string {
	return "Return the most recent N observations from project memory (default 5)."
}

const memRecentSchema = `{
    "type": "object",
    "properties": {
        "n": {"type": "integer", "description": "Number of recent observations to return (default 5)."}
    },
    "required": []
}`

func (t *MemRecentTool) InputSchema() json.RawMessage { return json.RawMessage(memRecentSchema) }

type memRecentInput struct {
	N int `json:"n"`
}

// Run decodes the input and returns the most recent N observations.
func (t *MemRecentTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var in memRecentInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("mem_recent: decode input: %w", err)
	}
	n := in.N
	if n <= 0 {
		n = 5
	}
	results, err := t.store.Recent(n)
	if err != nil {
		return "", fmt.Errorf("mem_recent: %w", err)
	}
	if len(results) == 0 {
		return "No observations in memory.", nil
	}
	var sb strings.Builder
	for _, obs := range results {
		fmt.Fprintf(&sb, "[%s] %s\n%s\n\n", obs.Title, obs.UpdatedAt.Format("2006-01-02"), obs.Content)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
