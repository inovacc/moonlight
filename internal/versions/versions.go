package versions

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/inovacc/dataprovider"
	"github.com/jmoiron/sqlx"
	"moonligth/pkg/versions"
	"time"
)

const (
	createQuery                 = `CREATE TABLE IF NOT EXISTS go_versions (id INTEGER PRIMARY KEY AUTOINCREMENT, version TEXT NOT NULL, stable BOOLEAN NOT NULL, filename TEXT NOT NULL, os TEXT NOT NULL, arch TEXT NOT NULL, sha256 TEXT NOT NULL, size TEXT NOT NULL, kind LONG NOT NULL);`
	findAllQuery                = `SELECT * FROM go_versions;`
	findByIDQuery               = `SELECT * FROM go_versions WHERE id = ?;`
	findByVerQuery              = `SELECT * FROM go_versions WHERE version = ?;`
	findByOSQuery               = `SELECT * FROM go_versions WHERE os = ?;`
	findByArchQuery             = `SELECT * FROM go_versions WHERE arch = ?;`
	findByKindQuery             = `SELECT * FROM go_versions WHERE kind = ?;`
	findByStableQuery           = `SELECT * FROM go_versions WHERE stable = ?;`
	findByOSArchQuery           = `SELECT * FROM go_versions WHERE os = ? AND arch = ?;`
	findByOSKindQuery           = `SELECT * FROM go_versions WHERE os = ? AND kind = ?;`
	findByArchKindQuery         = `SELECT * FROM go_versions WHERE arch = ? AND kind = ?;`
	findByOSArchKindQuery       = `SELECT * FROM go_versions WHERE os = ? AND arch = ? AND kind = ?;`
	findByOSArchStableQuery     = `SELECT * FROM go_versions WHERE os = ? AND arch = ? AND stable = ?;`
	findByOSArchKindStableQuery = `SELECT * FROM go_versions WHERE os = ? AND arch = ? AND kind = ? AND stable = ?;`
	findBySha256Query           = `SELECT * FROM go_versions WHERE sha256 = ?;`
	insertQuery                 = `INSERT INTO go_versions (version, stable, filename, os, arch, sha256, size, kind) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`
	updateQuery                 = `UPDATE go_versions SET version = ?, stable = ?, filename = ?, os = ?, arch = ?, sha256 = ?, size = ?, kind = ? WHERE id = ?;`
	deleteQuery                 = `DELETE FROM go_versions WHERE id = ?;`
	createLatestQuery           = `CREATE TABLE IF NOT EXISTS go_latest (id INTEGER PRIMARY KEY AUTOINCREMENT, version TEXT NOT NULL, next_release_candidate TEXT NOT NULL, stable BOOLEAN NOT NULL, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);`
	insertLatestQuery           = `INSERT INTO go_latest (version, stable, next_release_candidate) VALUES (?, ?, ?);`
	findLatestQuery             = `SELECT * FROM go_latest;`
)

type MapVersions struct {
	db *sqlx.DB
}

func NewMapVersions(goVer *versions.GoVersion) (*MapVersions, error) {
	opts := dataprovider.NewOptions(
		dataprovider.WithDriver(dataprovider.SQLiteDataProviderName),
		dataprovider.WithConnectionString("file:history.sqlite3?cache=shared"),
	)

	provider, err := dataprovider.NewDataProvider(opts)
	if err != nil {
		return nil, err
	}

	// Create the database table for the Go versions
	if err = provider.InitializeDatabase(createQuery); err != nil {
		return nil, err
	}

	// Create the database table for the latest Go version
	if err = provider.InitializeDatabase(createLatestQuery); err != nil {
		return nil, err
	}

	db := provider.GetConnection()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Hour)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func(tx *sql.Tx) {
		if err = tx.Rollback(); err != nil {
			fmt.Println(err)
		}
	}(tx)

	if _, err = tx.ExecContext(ctx, insertLatestQuery, goVer.Versions[0].Version, goVer.Versions[0].Stable, goVer.ReleaseCandidate); err != nil {
		return nil, err
	}

	for i := range goVer.Versions {
		for j := range goVer.Versions[i].Files {
			item := goVer.Versions[i]
			file := item.Files[j]

			if _, err = tx.ExecContext(ctx, insertQuery, item.Version, item.Stable, file.Filename, file.Os, file.Arch, file.Sha256, file.Size, file.Kind); err != nil {
				continue
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &MapVersions{db: db}, nil
}
