package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Masterminds/semver/v3"
	"github.com/inovacc/moonlight/internal/cron"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/afero"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const commandPrefixGo = "go"
const commandPrefixInstall = "install"

const (
	createTable = `CREATE TABLE IF NOT EXISTS installer (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version TEXT NOT NULL,
    command TEXT NOT NULL,
    dependencies TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`

	createTableModule = `CREATE TABLE IF NOT EXISTS module (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL,
    version TEXT NOT NULL,
    query TEXT NOT NULL,
    versions_history TEXT NOT NULL,
    time TIMESTAMP NOT NULL,
    dir TEXT NOT NULL,
    go_mod TEXT NOT NULL,
    go_version TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    installer_id INTEGER,
    FOREIGN KEY (installer_id) REFERENCES installer(id)
)`

	insertQuery  = `INSERT INTO installer (version, command, dependencies) VALUES (?, ?, ?) RETURNING id`
	insertModule = `INSERT INTO module (path, version, query, versions_history, time, dir, go_mod, go_version, installer_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	selectAll    = `SELECT * FROM installer`
	selectOne    = `SELECT * FROM installer WHERE version = ?`
)

var cronId int

type File struct {
	ID       int    `json:"id,omitempty" db:"id"`
	Version  string `json:"version,omitempty" db:"version"`
	Stable   bool   `json:"stable,omitempty" db:"stable"`
	Filename string `json:"filename,omitempty" db:"filename"`
	Os       string `json:"os,omitempty" db:"os"`
	Arch     string `json:"arch,omitempty" db:"arch"`
	Sha256   string `json:"sha256,omitempty" db:"sha256"`
	Size     int    `json:"size,omitempty" db:"size"`
	URL      string `json:"url,omitempty" db:"url"`
}

type Install struct {
	ID      int    `json:"id,omitempty" db:"id"`
	Version string `json:"version,omitempty" db:"version"`
	Command string `json:"command,omitempty" db:"command"`
}

type Module struct {
	Path      string    `json:"path,omitempty"`
	Version   string    `json:"version,omitempty"`
	Query     string    `json:"query,omitempty"`
	Versions  []string  `json:"versions,omitempty"`
	Time      time.Time `json:"time,omitempty"`
	Dir       string    `json:"dir,omitempty"`
	GoMod     string    `json:"go_mod,omitempty"`
	GoVersion string    `json:"go_version,omitempty"`
}

type Installer struct {
	db  *sqlx.DB
	ctx context.Context
}

func NewInstaller(ctx context.Context, db *sqlx.DB) (*Installer, error) {
	i := &Installer{
		db:  db,
		ctx: ctx,
	}

	if _, err := i.db.ExecContext(ctx, createTable); err != nil {
		return nil, err
	}

	if _, err := i.db.ExecContext(ctx, createTableModule); err != nil {
		return nil, err
	}

	return i, nil
}

func (i *Installer) CronJob(spec string, cron *cron.Cron) error {
	var err error
	cronId, err = cron.AddFunc(spec, func() {
		// Do something
	})
	if err != nil {
		return err
	}
	return nil
}

func (i *Installer) Command(command string) error {
	ctxTimeout, cancel := context.WithTimeout(i.ctx, 5*time.Minute)
	defer cancel()

	parsedCommand := strings.Split(command, " ")

	if !strings.HasPrefix(parsedCommand[0], commandPrefixGo) {
		return fmt.Errorf("invalid command, must have prefix %s", commandPrefixGo)
	}

	if !strings.HasPrefix(parsedCommand[1], commandPrefixInstall) {
		return fmt.Errorf("invalid command, must have prefix %s", commandPrefixInstall)
	}

	afs := afero.NewOsFs()
	tmpDir, err := afero.TempDir(afs, "", "go-list")
	if err != nil {
		return err
	}
	defer func(afs afero.Fs, path string) {
		if err = afs.RemoveAll(path); err != nil {
			fmt.Println(err)
		}
	}(afs, tmpDir)

	urlList := strings.Split(parsedCommand[2], "@")
	if len(urlList) != 2 {
		return fmt.Errorf("invalid url")
	}

	parsedCommand[2], err = getBaseURL(urlList[0])
	if err != nil {
		return err
	}

	execCommand := strings.Split(fmt.Sprintf("go list -m -json -versions %s", parsedCommand[2]), " ")
	cmd := exec.CommandContext(ctxTimeout, execCommand[0], execCommand[1:]...)
	cmd.Dir = tmpDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	var module Module
	if err = json.Unmarshal(out, &module); err != nil {
		return err
	}

	if module.Version == "" {
		stableVersions := filterAndSortStableVersions(module.Versions)
		if len(stableVersions) > 0 {
			module.Version = stableVersions[0]
		}
	}

	execCommand = strings.Split(fmt.Sprintf("go install %s@%s", urlList[0], module.Version), " ")

	if module.Version == "" {
		execCommand = strings.Split(command, " ")
	}
	cmd = exec.CommandContext(ctxTimeout, execCommand[0], execCommand[1:]...)

	out, err = cmd.CombinedOutput()
	if err != nil {
		return err
	}

	dependencies := strings.ReplaceAll(string(out), "go: downloading ", "")
	dependencies = strings.ReplaceAll(dependencies, "go: extracting ", "")
	dependencies = strings.ReplaceAll(dependencies, "go: finding ", "")
	dependencies = strings.ReplaceAll(dependencies, "go: found ", "")

	moduleStr := strings.Join(module.Versions, ",")

	tx, err := i.db.BeginTxx(ctxTimeout, nil)
	if err != nil {
		return err
	}

	var installerID int64
	if err = tx.QueryRowContext(ctxTimeout, insertQuery, module.Version, command, dependencies).Scan(&installerID); err != nil {
		tx.Rollback()
		return err
	}

	_, err = tx.ExecContext(ctxTimeout, insertModule, module.Path, module.Version, module.Query, moduleStr, module.Time, module.Dir, module.GoMod, module.GoVersion, installerID)
	if err != nil {
		tx.Rollback()
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}

func getBaseURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	// Split the path into segments
	segments := strings.Split(parsedURL.Path, "/")

	// Find the index of "cmd"
	cmdIndex := -1
	for i, segment := range segments {
		if segment == "cmd" {
			cmdIndex = i
			break
		}
	}

	// If "cmd" is found, create the base path without "cmd" and the following segment
	if cmdIndex != -1 && cmdIndex+1 < len(segments) {
		basePath := strings.Join(segments[:cmdIndex], "/")
		parsedURL.Path = basePath
	}

	return parsedURL.String(), nil
}

func filterAndSortStableVersions(versions []string) []string {
	// Filter out pre-release versions
	var stableVersions []*semver.Version
	for _, v := range versions {
		ver, err := semver.NewVersion(strings.TrimPrefix(v, "v"))
		if err != nil {
			continue
		}
		if ver.Prerelease() == "" {
			stableVersions = append(stableVersions, ver)
		}
	}

	// Sort the stable versions
	sort.Slice(stableVersions, func(i, j int) bool {
		return stableVersions[i].GreaterThan(stableVersions[j])
	})

	// Convert back to string slice
	var result = make([]string, 0)
	for _, v := range stableVersions {
		result = append(result, fmt.Sprintf("v%s", v.String()))
	}
	return result
}
