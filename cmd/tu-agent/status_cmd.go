package main

import (
	"bytes"
	"fmt"

	"github.com/spf13/cobra"
)

// topStatusCmd is the root-level `status` command: a read-only, at-a-glance
// health report that composes the graph status and the knowledge-index status
// into two labelled sections. It never aborts on one half's failure — a failing
// half is reported inline and the command still exits 0.
var topStatusCmd = &cobra.Command{
	GroupID: "diagnostics",
	Use:     "status",
	Short:   "Report graph + knowledge-index health at a glance (read-only)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()

		// Graph section — reproduce runGraphStatus() verbatim, or its error inline.
		fmt.Fprintln(out, "Graph")
		if gs, err := runGraphStatus(); err != nil {
			fmt.Fprintln(out, err.Error())
		} else {
			fmt.Fprint(out, gs)
		}

		// Knowledge section — render runStatusTo() output plus any returned error inline.
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Knowledge")
		var buf bytes.Buffer
		if err := runStatusTo(&buf, "."); err != nil {
			// runStatusTo returns every error before writing to buf, so the
			// buffer is empty here — render the error inline as the section body.
			fmt.Fprintln(out, err.Error())
		} else {
			fmt.Fprint(out, buf.String())
		}

		return nil
	},
}
