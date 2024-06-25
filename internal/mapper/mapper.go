package mapper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/inovacc/moonlight/pkg/cron"
	"github.com/inovacc/moonlight/pkg/versions"
	"github.com/jmoiron/sqlx"
	"time"
)

var cronId int

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
	db  *sqlx.DB
	ctx context.Context
}

func NewMapVersions(ctx context.Context, db *sqlx.DB, goVer *versions.GoVersion) (*MapVersions, error) {
	m := &MapVersions{
		db:  db,
		ctx: ctx,
	}

	if _, err := m.db.ExecContext(ctx, createQuery); err != nil {
		return nil, err
	}

	if _, err := m.db.ExecContext(ctx, createLatestQuery); err != nil {
		return nil, err
	}

	if err := m.checkLatestVersion(goVer); err != nil {
		return nil, err
	}

	if err := m.compareExistingFiles(goVer); err != nil {
		return nil, err
	}

	if len(goVer.Versions) > 0 {
		if err := m.insertItems(goVer); err != nil {
			return nil, err
		}
	}

	return m, nil
}

func (m *MapVersions) CronJob(spec string, cron *cron.Cron) error {
	var err error
	cronId, err = cron.AddFunc(spec, func() {
		// Do something
	})
	if err != nil {
		return err
	}
	return nil
}

// insertItems inserts the items into the database
func (m *MapVersions) insertItems(goVer *versions.GoVersion) error {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func(tx *sql.Tx) {
		if err = tx.Rollback(); err != nil {
			fmt.Println(err)
		}
	}(tx)

	for i := range goVer.Versions {
		for _, file := range goVer.Versions[i].Files {
			if _, err = tx.ExecContext(ctx, insertQuery, goVer.Versions[i].Version, goVer.Versions[i].Stable, file.Filename, file.Os, file.Arch, file.Sha256, file.Size, file.Kind); err != nil {
				continue
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

// compareExistingFiles compares the existing files in the database with the new files
func (m *MapVersions) compareExistingFiles(goVer *versions.GoVersion) error {
	var hashes = make([]*File, 0)
	if err := m.db.Select(&hashes, findAllSha256Query); err != nil {
		return fmt.Errorf("error getting all hashes: %w", err)
	}

	hashMap := make(map[string]struct{})
	for _, hash := range hashes {
		hashMap[hash.Sha256] = struct{}{}
	}

	fixedVersions := make([]versions.Versions, len(goVer.Versions))

	for idx, item := range goVer.Versions {
		for _, file := range item.Files {

			fixedVersions[idx].Version = item.Version
			fixedVersions[idx].Stable = item.Stable
			if _, exists := hashMap[file.Sha256]; !exists {
				fixedVersions[idx].Files = append(fixedVersions[idx].Files, file)
			}
		}
	}

	goVer.Versions = fixedVersions
	return nil
}

// checkLatestVersion checks if the latest version is the same as the new version
func (m *MapVersions) checkLatestVersion(goVer *versions.GoVersion) error {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	latestVersion := &LatestVersion{}
	if err := m.db.Get(latestVersion, findLatestQuery); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}

	uVer := &GoVersion{
		ID:               latestVersion.ID,
		StableVersion:    goVer.StableVersion,
		ReleaseCandidate: goVer.ReleaseCandidate,
	}

	customQuery := updateLatestQuery
	if goVer.StableVersion != latestVersion.StableVersion {
		customQuery = updateLatestQuery
	}

	if latestVersion.StableVersion == "" {
		customQuery = insertLatestQuery
	}

	if _, err := m.db.ExecContext(ctx, customQuery, uVer.StableVersion, true, uVer.ReleaseCandidate, uVer.ID); err != nil {
		return err
	}

	return nil
}

// GetAll returns all the versions
func (m *MapVersions) GetAll() ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findAllQuery); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByID returns a version by its ID
func (m *MapVersions) GetByID(id int) (*File, error) {
	var v File
	if err := m.db.Get(&v, findByIDQuery, id); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetByVer returns a version by its version
func (m *MapVersions) GetByVer(version string) (*File, error) {
	var v File
	if err := m.db.Get(&v, findByVerQuery, version); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetByOS returns a version by its OS
func (m *MapVersions) GetByOS(os string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSQuery, os); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByArch returns a version by its architecture
func (m *MapVersions) GetByArch(arch string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByArchQuery, arch); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByKind returns a version by its kind
func (m *MapVersions) GetByKind(kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByKindQuery, kind); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByStable returns a version by its stability
func (m *MapVersions) GetByStable() ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByStableQuery); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByOSArch returns a version by its OS and architecture
func (m *MapVersions) GetByOSArch(os, arch string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchQuery, os, arch); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByOSKind returns a version by its OS and kind
func (m *MapVersions) GetByOSKind(os, kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSKindQuery, os, kind); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByArchKind returns a version by its architecture and kind
func (m *MapVersions) GetByArchKind(arch, kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByArchKindQuery, arch, kind); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByOSArchKind returns a version by its OS, architecture, and kind
func (m *MapVersions) GetByOSArchKind(os, arch, kind string) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchKindQuery, os, arch, kind); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByOSArchStable returns a version by its OS, architecture, and stability
func (m *MapVersions) GetByOSArchStable(os, arch string, stable bool) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchStableQuery, os, arch, stable); err != nil {
		return nil, err
	}
	return v, nil
}

// GetByOSArchKindStable returns a version by its OS, architecture, kind, and stability
func (m *MapVersions) GetByOSArchKindStable(os, arch, kind string, stable bool) ([]*File, error) {
	var v []*File
	if err := m.db.Select(&v, findByOSArchKindStableQuery, os, arch, kind, stable); err != nil {
		return nil, err
	}
	return v, nil
}

// GetBySha256 returns a version by its SHA256
func (m *MapVersions) GetBySha256(sha256 string) (*File, error) {
	var v File
	if err := m.db.Get(&v, findBySha256Query, sha256); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetLatest returns the latest version
func (m *MapVersions) GetLatest() (*LatestVersion, error) {
	var v LatestVersion
	if err := m.db.Get(&v, findLatestQuery); err != nil {
		return nil, err
	}
	return &v, nil
}

// Update updates a version
func (m *MapVersions) Update(v *versions.Versions) error {
	_, err := m.db.Exec(updateQuery, v.Version, v.Stable, v.Files[0].Filename, v.Files[0].Os, v.Files[0].Arch, v.Files[0].Sha256, v.Files[0].Size, v.Files[0].Kind, v.ID)
	return err
}

// Delete deletes a version
func (m *MapVersions) Delete(id int) error {
	_, err := m.db.Exec(deleteQuery, id)
	return err
}
