package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/tu/tu-agent/internal/memory"
)

var (
	sessionProject string
	sessionSummary string
	sessionListN   int
)

func openSessionMem() (*memory.Store, error) { return memory.Open(memoryDBPath(repoRoot())) }

func closeSessionMem(s *memory.Store) {
	if cerr := s.Close(); cerr != nil {
		slog.Warn("session: memory store close failed", "err", cerr)
	}
}

// runSessionStart opens a session and writes the previous summary (if any) to out.
func runSessionStart(project string, out io.Writer) error {
	s, err := openSessionMem()
	if err != nil {
		return err
	}
	defer closeSessionMem(s)
	sess, prev, err := s.SessionStart(project)
	if err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "session started: %s\n", sess.ID)
		if prev != "" {
			fmt.Fprintf(out, "\n## Last session\n%s\n", prev)
		}
	}
	return nil
}

func runSessionEnd(project, summary string, out io.Writer) error {
	s, err := openSessionMem()
	if err != nil {
		return err
	}
	defer closeSessionMem(s)
	sess, err := s.SessionEnd(project, summary)
	if err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "session ended: %s\nsummary: %s\n", sess.ID, sess.Summary)
	}
	return nil
}

func runSessionList(project string, n int, out io.Writer) error {
	s, err := openSessionMem()
	if err != nil {
		return err
	}
	defer closeSessionMem(s)
	list, err := s.SessionList(project, n)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(out, "no sessions")
		return nil
	}
	for _, ss := range list {
		state := "active"
		if !ss.EndedAt.IsZero() {
			state = ss.EndedAt.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(out, "%s  %-19s  %s\n", ss.StartedAt.Format("2006-01-02 15:04"), state, ss.Summary)
	}
	return nil
}

var sessionCmd = &cobra.Command{Use: "session", Short: "Explicit work sessions with carried-over continuity"}

var sessionStartCmd = &cobra.Command{
	Use: "start", Short: "Start a session (prints the previous session's summary)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error { return runSessionStart(sessionProject, cmd.OutOrStdout()) },
}
var sessionEndCmd = &cobra.Command{
	Use: "end", Short: "End the active session (composes a summary if --summary omitted)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runSessionEnd(sessionProject, sessionSummary, cmd.OutOrStdout())
	},
}
var sessionListCmd = &cobra.Command{
	Use: "list", Short: "List recent sessions", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runSessionList(sessionProject, sessionListN, cmd.OutOrStdout())
	},
}

func init() {
	sessionCmd.PersistentFlags().StringVar(&sessionProject, "project", "", "session project label (optional; memory DB is already repo-local)")
	sessionEndCmd.Flags().StringVar(&sessionSummary, "summary", "", "explicit session summary (composed from observations if omitted)")
	sessionListCmd.Flags().IntVar(&sessionListN, "n", 10, "number of sessions to list")
	sessionCmd.AddCommand(sessionStartCmd, sessionEndCmd, sessionListCmd)
	rootCmd.AddCommand(sessionCmd)
}
