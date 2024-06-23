package component

import (
	"github.com/spf13/cobra"
	"log/slog"
	"moonlight/internal/cron"
	"moonlight/internal/database"
	"moonlight/internal/mapper"
	"moonlight/pkg/versions"
)

func MainComponent(cmd *cobra.Command, _ []string) error {
	if err := database.NewDatabase(); err != nil {
		return err
	}
	defer database.CloseConnection()

	c, err := cron.NewCron(cmd.Context())
	if err != nil {
		return err
	}

	job := func() {
		slog.Info("Running job")

		goVer, err := versions.NewGoVersion()
		if err != nil {
			slog.Error(err.Error())
		}

		mapVerse, err := mapper.NewMapVersions(goVer)
		if err != nil {
			slog.Error(err.Error())
		}

		latestVersion, err := mapVerse.GetLatest()
		if err != nil {
			slog.Error(err.Error())
		}

		slog.Info(latestVersion.StableVersion)
	}

	if _, err = c.AddFunc("0 0 0 * * *", job); err != nil {
		return err
	}

	c.Start()

	slog.Info("Cron started")

	for {
		select {
		case <-cmd.Context().Done():
			return nil
		}
	}
}
