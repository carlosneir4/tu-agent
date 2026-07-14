package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

func TestTddOverlayCmd(t *testing.T) {
	var buf bytes.Buffer
	tddOverlayCmd.SetOut(&buf)
	if err := tddOverlayCmd.RunE(tddOverlayCmd, []string{"architect"}); err != nil {
		t.Fatalf("overlay architect: %v", err)
	}
	if !strings.Contains(buf.String(), "@s1") {
		t.Fatalf("architect overlay should contain @s1, got %q", buf.String())
	}
	if err := tddOverlayCmd.RunE(tddOverlayCmd, []string{"nope"}); err == nil {
		t.Fatal("unknown stage should error")
	}
}

func TestTddOverlayAllStages(t *testing.T) {
	for _, stage := range []string{"analyst", "architect", "craftsman", "judge", "scribe"} {
		var buf bytes.Buffer
		tddOverlayCmd.SetOut(&buf)
		if err := tddOverlayCmd.RunE(tddOverlayCmd, []string{stage}); err != nil {
			t.Fatalf("overlay %s: %v", stage, err)
		}
		if strings.TrimSpace(buf.String()) == "" {
			t.Fatalf("overlay %s produced empty output", stage)
		}
	}
}

func TestTddVerifyCmd(t *testing.T) {
	orig := cfg
	defer func() { cfg = orig }()

	var buf bytes.Buffer
	tddVerifyCmd.SetOut(&buf)
	tddVerifyCmd.SetContext(context.Background())

	cfg = config.Config{Tdd: config.TddConfig{TestCommand: "true"}}
	if err := tddVerifyCmd.RunE(tddVerifyCmd, nil); err != nil {
		t.Fatalf("tdd verify (true): %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != `{"ok":true}` {
		t.Fatalf("tdd verify (true) output = %q", got)
	}

	buf.Reset()
	cfg = config.Config{Tdd: config.TddConfig{TestCommand: "false"}}
	if err := tddVerifyCmd.RunE(tddVerifyCmd, nil); err != nil {
		t.Fatalf("tdd verify (false): %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != `{"ok":false}` {
		t.Fatalf("tdd verify (false) output = %q", got)
	}
}
