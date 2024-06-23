package cmd

import (
	"context"
	"log/slog"
	"moonlight/internal/component"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "moonlight",
	Short: "A brief description of your application",
	RunE:  component.MainComponent,
}

func Execute() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cobra.CheckErr(rootCmd.ExecuteContext(ctx))
}

func init() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	rootCmd.Flags().StringP("config", "c", "config.yaml", "config file (default is config.yaml)")
}
