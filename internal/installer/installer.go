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

const (
	commandPrefixGo      = "go"
	commandPrefixInstall = "install"

	createTableInstaller = `CREATE TABLE IF NOT EXISTS installer (
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

	insertInstaller = `INSERT INTO installer (version, command, dependencies) VALUES (?, ?, ?) RETURNING id`
	insertModule    = `INSERT INTO module (path, version, query, versions_history, time, dir, go_mod, go_version, installer_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
)

var cronId int

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

	if _, err := i.db.ExecContext(ctx, createTableInstaller); err != nil {
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

	parsedCommand, err := i.validateCommand(command)
	if err != nil {
		return err
	}

	tmpDir, err := afero.TempDir(afero.NewOsFs(), "", "go-list")
	if err != nil {
		return err
	}
	defer afero.NewOsFs().RemoveAll(tmpDir)

	urlList := strings.Split(parsedCommand[len(parsedCommand)-1], "@")
	if len(urlList) != 2 {
		return fmt.Errorf("invalid url format")
	}

	parsedCommand[2], err = getBaseURL(urlList[0])
	if err != nil {
		return err
	}

	module, err := i.getModuleInfo(ctxTimeout, parsedCommand[2], tmpDir)
	if err != nil {
		return err
	}

	module.Version = selectVersion(module)

	if err = i.executeInstallCommand(ctxTimeout, parsedCommand[0], urlList[0], module.Version); err != nil {
		return err
	}

	return i.saveModuleInfo(ctxTimeout, command, module)
}

func (i *Installer) validateCommand(command string) ([]string, error) {
	parsedCommand := strings.Split(command, " ")
	if !strings.HasPrefix(parsedCommand[0], commandPrefixGo) {
		return nil, fmt.Errorf("invalid command, must have prefix %s", commandPrefixGo)
	}

	if !strings.HasPrefix(parsedCommand[1], commandPrefixInstall) {
		return nil, fmt.Errorf("invalid command, must have prefix %s", commandPrefixInstall)
	}
	return parsedCommand, nil
}

func (i *Installer) getModuleInfo(ctx context.Context, url, dir string) (Module, error) {
	execCommand := []string{"go", "list", "-m", "-json", "-versions", url}
	cmd := exec.CommandContext(ctx, execCommand[0], execCommand[1:]...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return Module{}, err
	}

	var module Module
	if err = json.Unmarshal(out, &module); err != nil {
		return Module{}, err
	}

	return module, nil
}

func selectVersion(module Module) string {
	if module.Version == "" {
		stableVersions := filterAndSortStableVersions(module.Versions)
		if len(stableVersions) > 0 {
			return stableVersions[0]
		}
	}
	return module.Version
}

func (i *Installer) executeInstallCommand(ctx context.Context, goCommand, url, version string) error {
	execCommand := []string{goCommand, "install", fmt.Sprintf("%s@%s", url, version)}
	cmd := exec.CommandContext(ctx, execCommand[0], execCommand[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error executing install command: %w, output: %s", err, string(out))
	}
	return nil
}

func (i *Installer) saveModuleInfo(ctx context.Context, command string, module Module) error {
	tx, err := i.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	dependencies := extractDependencies(module.Versions)
	moduleStr := strings.Join(module.Versions, ",")

	var installerID int64
	if err := tx.QueryRowContext(ctx, insertInstaller, module.Version, command, dependencies).Scan(&installerID); err != nil {
		tx.Rollback()
		return err
	}

	_, err = tx.ExecContext(ctx, insertModule, module.Path, module.Version, module.Query, moduleStr, module.Time, module.Dir, module.GoMod, module.GoVersion, installerID)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func extractDependencies(output []string) string {
	joinedOutput := strings.Join(output, " ")
	joinedOutput = strings.ReplaceAll(joinedOutput, "go: downloading ", "")
	joinedOutput = strings.ReplaceAll(joinedOutput, "go: extracting ", "")
	joinedOutput = strings.ReplaceAll(joinedOutput, "go: finding ", "")
	joinedOutput = strings.ReplaceAll(joinedOutput, "go: found ", "")
	return joinedOutput
}

func getBaseURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	segments := strings.Split(parsedURL.Path, "/")
	for i, segment := range segments {
		if segment == "cmd" {
			parsedURL.Path = strings.Join(segments[:i], "/")
			break
		}
	}
	return parsedURL.String(), nil
}

func filterAndSortStableVersions(versions []string) []string {
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

	sort.Slice(stableVersions, func(i, j int) bool {
		return stableVersions[i].GreaterThan(stableVersions[j])
	})

	var result []string
	for _, v := range stableVersions {
		result = append(result, fmt.Sprintf("v%s", v.String()))
	}
	return result
}
