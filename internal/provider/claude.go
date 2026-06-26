package provider

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ClaudeConfig holds configuration for the Claude adapter.
type ClaudeConfig struct {
	APIKey          string
	BaseURL         string // optional; override for testing
	Model           string
	MaxOutputTokens int // max tokens to generate; 0 = default (defaultClaudeMaxTokens)
}

// defaultClaudeMaxTokens is the output-token ceiling used when the config sets none.
const defaultClaudeMaxTokens = 8192

// ClaudeAdapter implements Provider using the Anthropic Messages API.
type ClaudeAdapter struct {
	client          anthropic.Client
	model           string
	maxOutputTokens int
}

// NewClaudeAdapter creates a new ClaudeAdapter.
func NewClaudeAdapter(cfg ClaudeConfig) *ClaudeAdapter {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	maxTokens := cfg.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultClaudeMaxTokens
	}
	return &ClaudeAdapter{
		client:          anthropic.NewClient(opts...),
		model:           model,
		maxOutputTokens: maxTokens,
	}
}

// Name returns the provider identifier.
func (a *ClaudeAdapter) Name() string { return "claude" }

// Model returns the configured model identifier.
func (a *ClaudeAdapter) Model() string { return a.model }

// NativeContextWindow returns the intrinsic context window for Claude models.
func (a *ClaudeAdapter) NativeContextWindow() int { return 200000 }

// Send converts our types to SDK types, calls the API, and converts back.
func (a *ClaudeAdapter) Send(
	ctx context.Context,
	system string,
	messages []Message,
	tools []ToolDef,
) (Response, error) {
	// Build system prompt.
	systemBlocks := []anthropic.TextBlockParam{}
	if system != "" {
		systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: system})
	}

	// Convert messages.
	sdkMessages := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Blocks))
		for _, b := range msg.Blocks {
			switch b.Type {
			case "text":
				blocks = append(blocks, anthropic.NewTextBlock(b.Text))
			case "tool_use":
				var input any
				if len(b.Input) > 0 {
					if err := json.Unmarshal(b.Input, &input); err != nil {
						return Response{}, fmt.Errorf("claude: unmarshal tool_use input: %w", err)
					}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(b.ID, input, b.Name))
			case "tool_result":
				blocks = append(blocks, anthropic.NewToolResultBlock(b.ID, b.Content, b.IsError))
			default:
				// Unknown block type: skip
			}
		}
		switch msg.Role {
		case "assistant":
			sdkMessages = append(sdkMessages, anthropic.NewAssistantMessage(blocks...))
		default: // "user"
			sdkMessages = append(sdkMessages, anthropic.NewUserMessage(blocks...))
		}
	}

	// Convert tools.
	sdkTools := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		// Parse the input schema to extract properties and required fields.
		// The SDK's ToolInputSchemaParam uses ExtraFields for arbitrary JSON.
		var schemaMap map[string]any
		if err := json.Unmarshal(t.InputSchema, &schemaMap); err != nil {
			return Response{}, fmt.Errorf("claude: unmarshal tool schema for %q: %w", t.Name, err)
		}

		inputSchema := anthropic.ToolInputSchemaParam{}
		if props, ok := schemaMap["properties"]; ok {
			inputSchema.Properties = props
		}
		if req, ok := schemaMap["required"]; ok {
			if reqSlice, ok := req.([]any); ok {
				required := make([]string, 0, len(reqSlice))
				for _, r := range reqSlice {
					if s, ok := r.(string); ok {
						required = append(required, s)
					}
				}
				inputSchema.Required = required
			}
		}

		tool := anthropic.ToolUnionParamOfTool(inputSchema, t.Name)
		if tool.OfTool != nil && t.Description != "" {
			tool.OfTool.Description = anthropic.String(t.Description)
		}
		sdkTools = append(sdkTools, tool)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: int64(a.maxOutputTokens),
		System:    systemBlocks,
		Messages:  sdkMessages,
	}
	if len(sdkTools) > 0 {
		params.Tools = sdkTools
	}

	msg, err := a.client.Messages.New(ctx, params)
	if err != nil {
		if isPromptTooLargeMessage(err.Error()) {
			return Response{}, fmt.Errorf("claude: messages.new: %v: %w", err, ErrPromptTooLarge)
		}
		return Response{}, fmt.Errorf("claude: messages.new: %w", err)
	}

	// Convert response blocks.
	respBlocks := make([]Block, 0, len(msg.Content))
	for _, cb := range msg.Content {
		switch cb.Type {
		case "text":
			respBlocks = append(respBlocks, Block{
				Type: "text",
				Text: cb.Text,
			})
		case "tool_use":
			inputBytes, err := json.Marshal(cb.Input)
			if err != nil {
				return Response{}, fmt.Errorf("claude: marshal tool_use input: %w", err)
			}
			respBlocks = append(respBlocks, Block{
				Type:  "tool_use",
				ID:    cb.ID,
				Name:  cb.Name,
				Input: json.RawMessage(inputBytes),
			})
		default:
			// Skip unknown block types (thinking, etc.)
		}
	}

	return Response{
		Blocks:     respBlocks,
		StopReason: string(msg.StopReason),
		Usage: Usage{
			InputTokens:  int(msg.Usage.InputTokens),
			OutputTokens: int(msg.Usage.OutputTokens),
		},
	}, nil
}
