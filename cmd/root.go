package cmd

import (
	"context"
	"github.com/inovacc/moonlight/internal/component"
	"log/slog"
	"os"
)

var rootCmd = &cobra.Command{
	Use:   "moonlight",
	Short: "A brief description of your application",
	RunE: func(cmd *cobra.Command, args []string) error {
		configFile, err := cmd.Flags().GetString("config")
		if err != nil {
			return err
		}

		return component.Run(configFile)
	},
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
