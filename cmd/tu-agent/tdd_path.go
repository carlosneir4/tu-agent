package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/carlosneir4/tu-agent/internal/tdd"
)

var tddPathTicket string

var tddPathCmd = &cobra.Command{
	Use:   "path [feature description...]",
	Short: "Print the repo-relative per-feature artifact dir for a tdd run",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := tdd.Slugify(strings.Join(args, " "))
		fmt.Fprintln(cmd.OutOrStdout(), tdd.TddRelBase(tddPathTicket, slug))
		return nil
	},
}

func init() {
	tddPathCmd.Flags().StringVar(&tddPathTicket, "ticket", "", "ticket id; groups artifacts under <ticket>-<slug>")
	tddCmd.AddCommand(tddPathCmd)
}
