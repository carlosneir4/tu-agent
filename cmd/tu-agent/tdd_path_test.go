package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

func TestTddPathCmdParity(t *testing.T) {
	var buf bytes.Buffer
	cmd := tddPathCmd
	cmd.SetOut(&buf)
	tddPathTicket = "ABC-9"
	if err := cmd.RunE(cmd, []string{"user", "login"}); err != nil {
		t.Fatalf("path cmd: %v", err)
	}
	tddPathTicket = ""
	got := strings.TrimSpace(buf.String())
	want := tdd.TddRelBase("ABC-9", tdd.Slugify("user login"))
	if got != want {
		t.Errorf("tdd path output = %q, want %q", got, want)
	}
}
