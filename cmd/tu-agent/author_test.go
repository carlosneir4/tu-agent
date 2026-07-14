package main

import (
	"strings"
	"testing"
)

// TestRequireAuthor verifies requireAuthor's hard-error contract for
// team-chunk writes: an empty author must error with actionable remediation
// (an empty author collapses every teammate into chunk-local and silently
// overwrites shared notes), while a real author passes through unchanged.
func TestRequireAuthor(t *testing.T) {
	tests := []struct {
		name    string
		author  string
		want    string
		wantErr bool
	}{
		{name: "empty author errors", author: "", wantErr: true},
		{name: "real author passes through", author: "alice@x", want: "alice@x", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requireAuthor(tt.author)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("requireAuthor(%q): want error, got nil", tt.author)
				}
				if !strings.Contains(err.Error(), "user.email") {
					t.Fatalf("requireAuthor(%q) error = %q, want it to mention user.email", tt.author, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("requireAuthor(%q): unexpected error: %v", tt.author, err)
			}
			if got != tt.want {
				t.Fatalf("requireAuthor(%q) = %q, want %q", tt.author, got, tt.want)
			}
		})
	}
}
