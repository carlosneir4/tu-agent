package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/carlosneir4/tu-agent/internal/config"
	"github.com/spf13/cobra"
)

var (
	debug bool
	cfg   config.Config
)

var rootCmd = &cobra.Command{
	Use:   "tu-agent",
	Short: "tu-agent — multi-provider, multi-agent coding harness",
	Long: `tu-agent is a CLI for running multi-provider AI coding agents
with persistent memory and a shared skill registry.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		initLogger(debug)

		loader, err := config.DefaultLoader()
		if err != nil {
			return err
		}
		cfg, err = loader.Load()
		if err != nil {
			return err
		}
		slog.Debug("config loaded", "routing_default", cfg.Routing.Default)
		return nil
	},
}

func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging (text format to stderr)")

	// Register the help taxonomy groups. Commands reference these via their
	// GroupID field; cobra's checkCommandGroups requires the groups to exist
	// before Execute or it panics.
	rootCmd.AddGroup(
		&cobra.Group{ID: "setup", Title: "Setup"},
		&cobra.Group{ID: "graph", Title: "Grafo"},
		&cobra.Group{ID: "memory", Title: "Memoria"},
		&cobra.Group{ID: "feature", Title: "Feature"},
		&cobra.Group{ID: "diagnostics", Title: "Diagnóstico"},
	)

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(learnCmd)
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(benchCmd)
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(mapCmd)
	rootCmd.AddCommand(guardPathCmd)
	rootCmd.AddCommand(hookCmd)
	rootCmd.AddCommand(topStatusCmd)
}

func initLogger(debug bool) {
	var h slog.Handler
	if debug {
		h = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	slog.SetDefault(slog.New(h))
}
