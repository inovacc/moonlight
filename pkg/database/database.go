package database

import (
	"github.com/inovacc/dataprovider"
	"github.com/inovacc/moonlight/internal/config"
	"github.com/jmoiron/sqlx"
	"log/slog"
)

var d *Database

type Database struct {
	db *sqlx.DB
}

// NewDatabase creates a new database connection
func NewDatabase() error {
	opts := dataprovider.NewOptions(
		dataprovider.WithSqliteDB(config.GetConfig.Db.Dbname, config.GetConfig.Db.DBPath),
	)

	provider, err := dataprovider.NewDataProvider(opts)
	if err != nil {
		return err
	}

	d = &Database{
		db: provider.GetConnection(),
	}
	return nil
}

func GetConnection() *sqlx.DB {
	return d.db
}

// CloseConnection closes the database connection
func CloseConnection() {
	if err := d.db.Close(); err != nil {
		slog.Error(err.Error())
	}
}
