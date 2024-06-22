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
	findByStableQuery           = `SELECT * FROM go_versions WHERE stable = true;`
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

	if _, err = tx.ExecContext(ctx, insertLatestQuery, goVer.StableVersion, true, goVer.ReleaseCandidate); err != nil {
		return nil, err
	}

	for i := range goVer.Versions {
		for _, file := range goVer.Versions[i].Files {
			if _, err = tx.ExecContext(ctx, insertQuery, goVer.Versions[i].Version, goVer.Versions[i].Stable, file.Filename, file.Os, file.Arch, file.Sha256, file.Size, file.Kind); err != nil {
				continue
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &MapVersions{db: db}, nil
}

func (m *MapVersions) GetAll() ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findAllQuery); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByID(id int) (*versions.Versions, error) {
	var v versions.Versions
	if err := m.db.Get(&v, findByIDQuery, id); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *MapVersions) GetByVer(version string) (*versions.Versions, error) {
	var v versions.Versions
	if err := m.db.Get(&v, findByVerQuery, version); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *MapVersions) GetByOS(os string) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByOSQuery, os); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByArch(arch string) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByArchQuery, arch); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByKind(kind string) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByKindQuery, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByStable() ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByStableQuery); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArch(os, arch string) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByOSArchQuery, os, arch); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSKind(os, kind string) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByOSKindQuery, os, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByArchKind(arch, kind string) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByArchKindQuery, arch, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArchKind(os, arch, kind string) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByOSArchKindQuery, os, arch, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArchStable(os, arch string, stable bool) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByOSArchStableQuery, os, arch, stable); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArchKindStable(os, arch, kind string, stable bool) ([]*versions.Versions, error) {
	var v []*versions.Versions
	if err := m.db.Select(&v, findByOSArchKindStableQuery, os, arch, kind, stable); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetBySha256(sha256 string) (*versions.Versions, error) {
	var v versions.Versions
	if err := m.db.Get(&v, findBySha256Query, sha256); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *MapVersions) GetLatest() (*versions.GoVersion, error) {
	var v versions.GoVersion
	if err := m.db.Get(&v, findLatestQuery); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *MapVersions) Update(v *versions.Versions) error {
	_, err := m.db.Exec(updateQuery, v.Version, v.Stable, v.Files[0].Filename, v.Files[0].Os, v.Files[0].Arch, v.Files[0].Sha256, v.Files[0].Size, v.Files[0].Kind, v.ID)
	return err
}

func (m *MapVersions) Delete(id int) error {
	_, err := m.db.Exec(deleteQuery, id)
	return err
}
