package mapper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/inovacc/dataprovider"
	"github.com/jmoiron/sqlx"
	"moonligth/pkg/versions"
	"time"
)

const (
	createQuery                 = `CREATE TABLE IF NOT EXISTS go_versions (id INTEGER PRIMARY KEY AUTOINCREMENT, version TEXT NOT NULL, stable BOOLEAN NOT NULL, filename TEXT NOT NULL, os TEXT NOT NULL, arch TEXT NOT NULL, sha256 TEXT NOT NULL, size TEXT NOT NULL, kind LONG NOT NULL);`
	findAllQuery                = `SELECT * FROM go_versions;`
	findAllSha256Query          = `SELECT sha256 FROM go_versions;`
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
	updateLatestQuery           = `UPDATE go_latest SET version = ?, stable = ?, next_release_candidate = ? WHERE id = ?;`
	findLatestQuery             = `SELECT * FROM go_latest;`
)

type LatestVersion struct {
	ID                  int    `json:"id,omitempty" db:"id"`
	Version             string `json:"version,omitempty" db:"version"`
	NexReleaseCandidate string `json:"next_release_candidate,omitempty" db:"next_release_candidate"`
	StableVersion       string `json:"stable,omitempty" db:"stable"`
	Sha256              string `json:"sha256,omitempty" db:"sha256"`
	CreatedAt           string `json:"created_at,omitempty" db:"created_at"`
	UpdatedAt           string `json:"updated_at,omitempty" db:"updated_at"`
}

type GoVersion struct {
	ID               int        `json:"id,omitempty" db:"id"`
	StableVersion    string     `json:"stable,omitempty" db:"stable"`
	Versions         []Versions `json:"versions,omitempty" db:"versions"`
	ReleaseCandidate string     `json:"release_candidate,omitempty" db:"release_candidate"`
}

type Versions struct {
	ID      int    `json:"id,omitempty" db:"id"`
	Version string `json:"version,omitempty" db:"version"`
	Stable  bool   `json:"stable,omitempty" db:"stable"`
	Files   []File `json:"files,omitempty" db:"files"`
}

type File struct {
	ID       int    `json:"id,omitempty" db:"id"`
	Version  string `json:"version,omitempty" db:"version"`
	Stable   bool   `json:"stable,omitempty" db:"stable"`
	Filename string `json:"filename,omitempty" db:"filename"`
	Os       string `json:"os,omitempty" db:"os"`
	Arch     string `json:"arch,omitempty" db:"arch"`
	Sha256   string `json:"sha256,omitempty" db:"sha256"`
	Size     int    `json:"size,omitempty" db:"size"`
	Kind     string `json:"kind,omitempty" db:"kind"`
}

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

	var hashes = make([]*File, 0)
	if err = db.Select(&hashes, findAllSha256Query); err != nil {
		return nil, err
	}

	latestVersion := &LatestVersion{}
	if err = db.Get(latestVersion, findLatestQuery); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	customQuery := updateLatestQuery
	uVer := &GoVersion{
		ID:               latestVersion.ID,
		StableVersion:    goVer.StableVersion,
		ReleaseCandidate: goVer.ReleaseCandidate,
	}

	if goVer.StableVersion != latestVersion.Version {
		customQuery = updateLatestQuery
	}

	if latestVersion.Version == "" {
		customQuery = insertLatestQuery
	}

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

	if _, err = tx.ExecContext(ctx, customQuery, uVer.StableVersion, true, uVer.ReleaseCandidate, uVer.ID); err != nil {
		return nil, err
	}

	if len(hashes) == 0 {
		for i := range goVer.Versions {
			for _, file := range goVer.Versions[i].Files {
				if _, err = tx.ExecContext(ctx, insertQuery, goVer.Versions[i].Version, goVer.Versions[i].Stable, file.Filename, file.Os, file.Arch, file.Sha256, file.Size, file.Kind); err != nil {
					continue
				}
			}
		}
		goto doneCommit
	}

	for i := range goVer.Versions {
		for _, file := range goVer.Versions[i].Files {
			if compareHashes(hashes, file.Sha256) {
				continue
			}
			if _, err = tx.ExecContext(ctx, insertQuery, goVer.Versions[i].Version, goVer.Versions[i].Stable, file.Filename, file.Os, file.Arch, file.Sha256, file.Size, file.Kind); err != nil {
				continue
			}
		}
	}

doneCommit:

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &MapVersions{db: db}, nil
}

func (m *MapVersions) GetAll() ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findAllQuery); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByID(id int) (*File, error) {
	var v File
	if err := m.db.Get(&v, findByIDQuery, id); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *MapVersions) GetByVer(version string) (*File, error) {
	var v File
	if err := m.db.Get(&v, findByVerQuery, version); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *MapVersions) GetByOS(os string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSQuery, os); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByArch(arch string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByArchQuery, arch); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByKind(kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByKindQuery, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByStable() ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByStableQuery); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArch(os, arch string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchQuery, os, arch); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSKind(os, kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSKindQuery, os, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByArchKind(arch, kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByArchKindQuery, arch, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArchKind(os, arch, kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchKindQuery, os, arch, kind); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArchStable(os, arch string, stable bool) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchStableQuery, os, arch, stable); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetByOSArchKindStable(os, arch, kind string, stable bool) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchKindStableQuery, os, arch, kind, stable); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *MapVersions) GetBySha256(sha256 string) (*File, error) {
	var v File
	if err := m.db.Get(&v, findBySha256Query, sha256); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *MapVersions) GetLatest() (*LatestVersion, error) {
	var v LatestVersion
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

func compareHashes(hashes []*File, hashFile string) bool {
	if hashes == nil {
		return false
	}

	for _, hash := range hashes {
		if hash.Sha256 == hashFile {
			return true
		}
	}
	return false
}
