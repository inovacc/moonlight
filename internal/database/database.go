package database

import (
	"github.com/inovacc/dataprovider"
	"github.com/jmoiron/sqlx"
	"moonlight/internal/config"
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
func CloseConnection() error {
	return d.db.Close()
}
