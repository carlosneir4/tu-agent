package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "0.3.0-dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the tu-agent version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(version)
		return nil
	},
}
