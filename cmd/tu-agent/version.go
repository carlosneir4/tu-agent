package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is the reported tu-agent version. It defaults to a parseable dev
// sentinel and is overridden at release build time via
// -ldflags "-X main.version=<tag>" (see .github/workflows/release.yml), so a
// released binary reports its real tag (e.g. v1.0.5) instead of the dev value.
var version = "0.0.0-dev"

var versionCmd = &cobra.Command{
	GroupID: "diagnostics",
	Use:     "version",
	Short:   "Print the tu-agent version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(version)
		return nil
	},
}
