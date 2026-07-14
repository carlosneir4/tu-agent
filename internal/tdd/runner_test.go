package tdd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosneir4/tu-agent/internal/config"
)

func TestResolveTestRunner(t *testing.T) {
	tests := []struct {
		name        string
		testCommand string
		writeGoMod  bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "explicit config command",
			testCommand: "echo ok",
			writeGoMod:  false,
			wantErr:     false,
		},
		{
			name:        "go autodetect when go.mod present",
			testCommand: "",
			writeGoMod:  true,
			wantErr:     false,
		},
		{
			name:        "fail fast when no command and no go.mod",
			testCommand: "",
			writeGoMod:  false,
			wantErr:     true,
			errContains: "tdd.test_command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.writeGoMod {
				if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
					t.Fatalf("write go.mod: %v", err)
				}
			}
			cfg := config.Config{Tdd: config.TddConfig{TestCommand: tt.testCommand}}

			runner, err := ResolveTestRunner(cfg, root)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if runner == nil {
				t.Fatalf("expected non-nil runner")
			}
		})
	}
}

func TestRunVerify(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	passed, err := RunVerify(ctx, config.Config{Tdd: config.TddConfig{TestCommand: "true"}}, root)
	if err != nil {
		t.Fatalf("runVerify(true): %v", err)
	}
	if !passed {
		t.Fatalf("runVerify(true) should report passed")
	}

	passed, err = RunVerify(ctx, config.Config{Tdd: config.TddConfig{TestCommand: "false"}}, root)
	if err != nil {
		t.Fatalf("runVerify(false): %v", err)
	}
	if passed {
		t.Fatalf("runVerify(false) should report not passed")
	}
}
