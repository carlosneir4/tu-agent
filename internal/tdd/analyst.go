package tdd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// Chatter is one conversational turn: send input, get the assistant's reply.
// Satisfied by *orchestrator.Orchestrator.
type Chatter interface {
	Chat(ctx context.Context, input string) (string, error)
}

// RunAnalyst runs the interactive interrogation in the foreground. It seeds the
// conversation with task, then alternates: print the assistant's question, read
// one user line from in, send it back — until the assistant emits a contract
// with status "pass" (the spec is written). EOF before completion is an error.
func RunAnalyst(ctx context.Context, c Chatter, task string, reader *bufio.Reader, out io.Writer) (Contract, error) {
	reply, err := c.Chat(ctx, task)
	if err != nil {
		return Contract{}, fmt.Errorf("tdd.RunAnalyst: %w", err)
	}
	for {
		if contract, perr := ParseContract(reply); perr == nil && contract.Status == StatusPass {
			return contract, nil
		}
		fmt.Fprintln(out, strings.TrimSpace(stripContract(reply)))
		fmt.Fprint(out, "> ")
		line, rerr := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if rerr != nil && rerr != io.EOF {
			return Contract{}, fmt.Errorf("tdd.RunAnalyst: reading input: %w", rerr)
		}
		if line == "" && rerr == io.EOF {
			return Contract{}, fmt.Errorf("tdd.RunAnalyst: input ended before the spec was complete")
		}
		reply, err = c.Chat(ctx, line)
		if err != nil {
			return Contract{}, fmt.Errorf("tdd.RunAnalyst: %w", err)
		}
	}
}

// stripContract removes a trailing ```json block so only the prose question is shown.
func stripContract(text string) string {
	if i := strings.LastIndex(text, "```json"); i >= 0 {
		return text[:i]
	}
	return text
}
