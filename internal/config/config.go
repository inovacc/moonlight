package config

import (
	"fmt"
	"os"
	"strings"
)

var GetConfig *Config

func init() {
	GetConfig = &Config{
		Logger: Logger{
			LogLevel:  LevelInfo,
			LogFormat: JSONLogFormat,
		},
		Db: Db{
			Driver: SQLiteNamedDriver,
			Dbname: "store",
			DBPath: os.TempDir(),
		},
	}
}

const (
	SQLiteNamedDriver string = "sqlite"
)

type LogFormat string

const (
	JSONLogFormat LogFormat = "json"
	TextLogFormat LogFormat = "text"
)

type LevelName string

const (
	LevelDebug LevelName = "debug"
	LevelInfo  LevelName = "info"
	LevelError LevelName = "error"
)

type Config struct {
	Logger Logger `yaml:"logger" mapstructure:"logger" json:"logger"`
	Db     Db     `yaml:"db" mapstructure:"db" json:"db"`
}

type Logger struct {
	LogLevel  LevelName `yaml:"logLevel" mapstructure:"logLevel" json:"logLevel"`
	LogFormat LogFormat `yaml:"logFormat" mapstructure:"logFormat" json:"logFormat"`
}

type Db struct {
	Driver string `yaml:"driver" mapstructure:"driver" json:"driver"`
	Dbname string `yaml:"dbName" mapstructure:"dbName" json:"dbName"`
	DBPath string `yaml:"dbPath" mapstructure:"dbPath" json:"dbPath"`
}

type OptsFunc func(*Config)

// WithSqliteDB sets sqlite db path name
func WithSqliteDB(name, path string) OptsFunc {
	if !strings.HasSuffix(path, ".sqlite") {
		path = fmt.Sprintf("%s.sqlite", path)
	}
	return func(o *Config) {
		o.Db.Dbname = name
		o.Db.DBPath = path
	}
}

// WithLogLevel sets log level, e.g. info, debug, error
func WithLogLevel(logLevel LevelName) OptsFunc {
	return func(o *Config) {
		o.Logger.LogLevel = logLevel
	}
}

// WithLogFormat sets log format
func WithLogFormat(logFormat LogFormat) OptsFunc {
	return func(o *Config) {
		o.Logger.LogFormat = logFormat
	}
}

// NewConfig creates a new service configuration
func NewConfig(opts ...OptsFunc) {
	for _, fn := range opts {
		fn(GetConfig)
	}
}
