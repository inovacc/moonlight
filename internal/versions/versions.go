package versions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/blang/semver"
	"github.com/jmoiron/sqlx"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/inovacc/dataprovider"
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

const (
	goUrl = "https://go.dev/dl/?mode=json&include=all"
)

type kindString string

const (
	kindSource    kindString = "source"
	kindBinary    kindString = "binary"
	kindInstaller kindString = "installer"
	kindArchive   kindString = "archive"
)

type archString string

const (
	archAmd64    archString = "amd64"
	arch386      archString = "386"
	archArm64    archString = "arm64"
	archArm      archString = "arm"
	archPpc64    archString = "ppc64"
	archMips64   archString = "mips64"
	archMips64le archString = "mips64le"
	archS390x    archString = "s390x"
	archWasm     archString = "wasm"
)

type osString string

const (
	osLinux   osString = "linux"
	osDarwin  osString = "darwin"
	osWindows osString = "windows"
)

type File struct {
	Filename string `json:"filename"`
	Os       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	Sha256   string `json:"sha256"`
	Size     int    `json:"size"`
	Kind     string `json:"kind"`
}

type Versions struct {
	ID               int64  `json:"-"`
	Version          string `json:"version"`
	Stable           bool   `json:"stable"`
	Files            []File `json:"files"`
	ReleaseCandidate string `json:"release_candidate"`
}

// GetWindows returns the Windows file and a boolean indicating if it was found.
func (g *Versions) GetWindows() (*File, bool) {
	for _, f := range g.Files {
		if f.Os == string(osWindows) && f.Arch == string(archAmd64) && f.Kind == string(kindArchive) {
			return &f, true
		}
	}
	return nil, false
}

// GetLinux returns the Linux file and a boolean indicating if it was found.
func (g *Versions) GetLinux() (*File, bool) {
	for _, f := range g.Files {
		if f.Os == string(osLinux) && f.Arch == string(archAmd64) && f.Kind == string(kindArchive) {
			return &f, true
		}
	}
	return nil, false
}

// GetDarwin returns the Darwin file and a boolean indicating if it was found.
func (g *Versions) GetDarwin() (*File, bool) {
	for _, f := range g.Files {
		if f.Os == string(osDarwin) && f.Arch == string(archAmd64) && f.Kind == string(kindArchive) {
			return &f, true
		}
	}
	return nil, false
}

type GoVersion struct {
	db       *sqlx.DB
	Versions []Versions `json:"versions"`
	Os       osString   `json:"os"`
	Arch     archString `json:"arch"`
	Kind     kindString `json:"kind"`
}

// NewGoVersion returns a new GoVersion.
func NewGoVersion() (*GoVersion, error) {
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

	return &GoVersion{
		db:   provider.GetConnection(),
		Arch: archString(os.Getenv("GOARCH")),
		Os:   osString(os.Getenv("GOOS")),
		Kind: kindArchive,
	}, nil
}

// GetGoVersion returns the latest stable Go versions from the Go website
func (g *GoVersion) GetGoVersion() (*Versions, error) {
	goVer, err := getJSON(goUrl)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Hour)
	defer cancel()

	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func(tx *sql.Tx) {
		if err = tx.Rollback(); err != nil {
			fmt.Println(err)
		}
	}(tx)

	releaseCandidate := goVer.Versions[0].Version

	for i := range goVer.Versions {
		for j := range goVer.Versions[i].Files {
			if goVer.Versions[i].Files[j].Kind == string(kindSource) {
				goVer.Versions[i].Files[j].Os = "any"
				goVer.Versions[i].Files[j].Arch = "any"
			}

			item := goVer.Versions[i]
			file := item.Files[j]

			if _, err = tx.ExecContext(ctx, insertQuery, item.Version, item.Stable, file.Filename, file.Os, file.Arch, file.Sha256, file.Size, file.Kind); err != nil {
				continue
			}
		}
	}

	sort.Slice(goVer.Versions, func(i, j int) bool {
		verI, _ := semver.Make(strings.TrimPrefix(goVer.Versions[i].Version, "go"))
		verJ, _ := semver.Make(strings.TrimPrefix(goVer.Versions[j].Version, "go"))
		return verI.GT(verJ)
	})

	result, err := tx.ExecContext(ctx, insertLatestQuery, goVer.Versions[0].Version, goVer.Versions[0].Stable, releaseCandidate)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Versions{
		ID:               id,
		Version:          goVer.Versions[0].Version,
		ReleaseCandidate: releaseCandidate,
		Stable:           goVer.Versions[0].Stable,
		Files:            goVer.Versions[0].Files,
	}, nil
}

// getJSON returns the GoVersion struct from the Go website
func getJSON(url string) (*GoVersion, error) {
	r, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		if err = Body.Close(); err != nil {
			fmt.Println(err)
		}
	}(r.Body)

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var goVer GoVersion
	if err = json.Unmarshal(data, &goVer.Versions); err != nil {
		return nil, err
	}
	return &goVer, nil
}
