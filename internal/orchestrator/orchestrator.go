package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/carlosneir4/tu-agent/internal/provider"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
	"github.com/carlosneir4/tu-agent/internal/tool"
)

const maxToolIterations = 50

// Orchestrator runs the conversation loop between the user, the model, and tools.
type Orchestrator struct {
	provider  provider.Provider
	tools     *tool.Registry
	telemetry *telemetry.Logger
	system    string
	subAgent  string // set when this orchestrator runs as a sub-agent; empty for main session
	messages  []provider.Message
}

// New creates an Orchestrator.
func New(p provider.Provider, tools *tool.Registry, tel *telemetry.Logger, system, subAgent string) *Orchestrator {
	return &Orchestrator{
		provider:  p,
		tools:     tools,
		telemetry: tel,
		system:    system,
		subAgent:  subAgent,
	}
}

// Chat handles one user turn: appends the user message, runs the tool loop,
// and returns the final assistant text.
func (o *Orchestrator) Chat(ctx context.Context, userInput string) (string, error) {
	o.messages = append(o.messages, provider.Message{
		Role:   "user",
		Blocks: []provider.Block{{Type: "text", Text: userInput}},
	})

	for i := 0; i < maxToolIterations; i++ {
		start := time.Now()
		resp, err := o.provider.Send(ctx, o.system, o.messages, o.tools.Defs())
		latency := time.Since(start).Milliseconds()
		if err != nil {
			return "", fmt.Errorf("orchestrator.Chat: send: %w", err)
		}

		toolCalls := countToolUse(resp.Blocks)
		if o.telemetry != nil {
			_ = o.telemetry.Log(telemetry.Entry{
				Timestamp:      time.Now(),
				Provider:       o.provider.Name(),
				Model:          o.provider.Model(),
				InputTokens:    resp.Usage.InputTokens,
				OutputTokens:   resp.Usage.OutputTokens,
				LatencyMS:      latency,
				ToolCallsCount: toolCalls,
				SubAgent:       o.subAgent,
				CostUSD:        telemetry.EstimateCost(o.provider.Name(), o.provider.Model(), resp.Usage.InputTokens, resp.Usage.OutputTokens),
			})
		}

		o.messages = append(o.messages, provider.Message{
			Role:   "assistant",
			Blocks: resp.Blocks,
		})

		if resp.StopReason == "end_turn" || resp.StopReason == "" {
			return textFromBlocks(resp.Blocks), nil
		}

		if resp.StopReason == "tool_use" {
			resultBlocks := o.executeToolCalls(ctx, resp.Blocks)
			o.messages = append(o.messages, provider.Message{
				Role:   "user",
				Blocks: resultBlocks,
			})
			continue
		}

		return textFromBlocks(resp.Blocks), nil
	}

	return "", fmt.Errorf("orchestrator.Chat: exceeded max tool iterations (%d)", maxToolIterations)
}

// Run starts the interactive chat loop, reading from stdin and writing to stdout.
func (o *Orchestrator) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			fmt.Println()
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "/exit" || line == "/quit" {
			break
		}

		response, err := o.Chat(ctx, line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		fmt.Println(response)
	}
	return scanner.Err()
}

func (o *Orchestrator) executeToolCalls(ctx context.Context, blocks []provider.Block) []provider.Block {
	results := make([]provider.Block, 0)
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		t, ok := o.tools.Get(b.Name)
		if !ok {
			results = append(results, provider.Block{
				Type:    "tool_result",
				ID:      b.ID,
				Content: fmt.Sprintf("unknown tool: %s", b.Name),
				IsError: true,
			})
			continue
		}

		output, err := t.Run(ctx, b.Input)
		if err != nil {
			results = append(results, provider.Block{
				Type:    "tool_result",
				ID:      b.ID,
				Content: fmt.Sprintf("tool error: %v", err),
				IsError: true,
			})
			continue
		}

		results = append(results, provider.Block{
			Type:    "tool_result",
			ID:      b.ID,
			Content: output,
			IsError: false,
		})
	}
	return results
}

func textFromBlocks(blocks []provider.Block) string {
	parts := make([]string, 0)
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func countToolUse(blocks []provider.Block) int {
	n := 0
	for _, b := range blocks {
		if b.Type == "tool_use" {
			n++
		}
	}
	return n
}
