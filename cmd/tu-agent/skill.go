package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tu/tu-agent/internal/codegen"
)

// runSkillPrune removes empty skill directories under root's generated skills
// dir. It is a test seam; skillPruneCmd's RunE calls it.
func runSkillPrune(root string) error {
	removed, err := codegen.PruneEmptySkillDirs(generatedSkillsDir(root))
	if err != nil {
		return err
	}
	if len(removed) == 0 {
		fmt.Println("No empty skill directories found.")
		return nil
	}
	fmt.Printf("Removed %d empty skill dir(s): %v\n", len(removed), removed)
	return nil
}

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Operate on generated skills",
}

var skillPruneCmd = &cobra.Command{
	Use:   "prune [path]",
	Short: "Remove empty skill directories left by an interrupted or failed run",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := "."
		if len(args) == 1 {
			root = args[0]
		}
		return runSkillPrune(root)
	},
}

func init() {
	skillCmd.AddCommand(skillPruneCmd)
	rootCmd.AddCommand(skillCmd)
}
