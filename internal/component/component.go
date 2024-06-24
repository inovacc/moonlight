package component

import (
	"github.com/inovacc/moonlight/internal/cron"
	"github.com/inovacc/moonlight/internal/database"
	"github.com/inovacc/moonlight/internal/mapper"
	"github.com/inovacc/moonlight/pkg/versions"
	"log/slog"
)

func Run(cmd *cobra.Command, _ []string) error {
	if err := database.NewDatabase(); err != nil {
		return err
	}
	defer database.CloseConnection()

	c, err := cron.NewCronScheduler(cmd.Context())
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

	if _, err = c.AddFunc("*/1 * * * *", job); err != nil {
		return err
	}

	c.Start()

	slog.Info("Main component started")

	for {
		select {
		case <-cmd.Context().Done():
			return nil
		}
	}
}
