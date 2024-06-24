package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/inovacc/moonlight/internal/cron"
	"github.com/jmoiron/sqlx"
	"os/exec"
	"strings"
	"time"
)

const commandPrefixGo = "go"
const commandPrefixInstall = "install"

const (
	createTable = `CREATE TABLE IF NOT EXISTS installer (id INTEGER PRIMARY KEY AUTOINCREMENT, version TEXT NOT NULL, command TEXT NOT NULL,  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);`
	insert      = `INSERT INTO installer (version, command) VALUES (?, ?);`
	selectAll   = `SELECT * FROM installer;`
	selectOne   = `SELECT * FROM installer WHERE version = ?;`
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
	Path      string    `json:"Path"`
	Version   string    `json:"Version"`
	Query     string    `json:"Query"`
	Versions  []string  `json:"Versions"`
	Time      time.Time `json:"Time"`
	Dir       string    `json:"Dir"`
	GoMod     string    `json:"GoMod"`
	GoVersion string    `json:"GoVersion"`
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
	parsedCommand := strings.Split(command, " ")

	if !strings.HasPrefix(parsedCommand[0], commandPrefixGo) {
		return fmt.Errorf("invalid command, must have prefix %s", commandPrefixGo)
	}

	if !strings.HasPrefix(parsedCommand[1], commandPrefixInstall) {
		return fmt.Errorf("invalid command, must have prefix %s", commandPrefixInstall)
	}

	//TODO implement do module to catch version
	execCommand := strings.Split(fmt.Sprintf("go list -m %s", parsedCommand[2]), " ")

	cmd := exec.CommandContext(i.ctx, execCommand[0], execCommand[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	var module Module
	if err = json.Unmarshal(out, &module); err != nil {
		return err
	}

	return nil
}
