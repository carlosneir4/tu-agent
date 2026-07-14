package main

import (
	"testing"
)

func TestTddCmdRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "tdd" {
			found = true
		}
	}
	if !found {
		t.Fatal("tdd command not registered on rootCmd")
	}
}
