package cmd

import (
	"context"
	"github.com/inovacc/moonlight/internal/component"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "moonlight",
	Short: "A brief description of your application",
	RunE:  component.Run,
}

func Execute() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cobra.CheckErr(rootCmd.ExecuteContext(ctx))
}

func init() {
	rootCmd.Flags().StringP("config", "c", "config.yaml", "config file (default is config.yaml)")
}
