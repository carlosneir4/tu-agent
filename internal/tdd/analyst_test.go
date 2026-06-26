package tdd

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptChatter returns canned replies in order; records inputs.
type scriptChatter struct {
	replies []string
	i       int
	inputs  []string
}

func (s *scriptChatter) Chat(_ context.Context, input string) (string, error) {
	s.inputs = append(s.inputs, input)
	r := s.replies[s.i]
	s.i++
	return r, nil
}

func TestRunAnalystCompletes(t *testing.T) {
	ch := &scriptChatter{replies: []string{
		"What output format do you want?", // a question -> needs a user answer
		"Thanks.\n```json\n{\"stage\":\"analyst\",\"status\":\"pass\",\"handoff\":\"spec ready\"}\n```",
	}}
	in := strings.NewReader("plain text\n") // user's answer to the one question
	var out strings.Builder

	c, err := RunAnalyst(context.Background(), ch, "build a count command", bufio.NewReader(in), &out)
	if err != nil {
		t.Fatalf("analyst: %v", err)
	}
	if c.Status != StatusPass {
		t.Fatalf("status = %q, want pass", c.Status)
	}
	if ch.inputs[0] != "build a count command" {
		t.Fatalf("first input = %q, want the task", ch.inputs[0])
	}
	if !strings.Contains(out.String(), "output format") {
		t.Fatalf("question not printed; out = %q", out.String())
	}
}

func TestRunAnalystEOFBeforeDone(t *testing.T) {
	ch := &scriptChatter{replies: []string{"A question?"}}
	_, err := RunAnalyst(context.Background(), ch, "task", bufio.NewReader(strings.NewReader("")), &strings.Builder{})
	if err == nil {
		t.Fatalf("expected error on EOF before completion")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestRunAnalystReadError(t *testing.T) {
	ch := &scriptChatter{replies: []string{"A question?"}}
	_, err := RunAnalyst(context.Background(), ch, "task", bufio.NewReader(errReader{}), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "reading input") {
		t.Fatalf("expected reading-input error, got %v", err)
	}
}
