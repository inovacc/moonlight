package config

import (
	"bytes"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

func init() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
}

var GetConfig *Config

// SupportedExts are universally supported extensions.
var SupportedExts = []string{"json", "yaml", "yml"}

func init() {
	GetConfig = &Config{
		fs:       afero.NewOsFs(),
		initWG:   sync.WaitGroup{},
		eventsWG: sync.WaitGroup{},
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

type Config struct {
	fs             afero.Fs
	initWG         sync.WaitGroup
	eventsWG       sync.WaitGroup
	configName     string
	configFile     string
	configPaths    []string
	configType     string
	onConfigChange func(fsnotify.Event)
	Logger         Logger `yaml:"logger" mapstructure:"logger" json:"logger"`
	Db             Db     `yaml:"db" mapstructure:"db" json:"db"`
}

func (c *Config) defaultValues() error {
	if c.Logger.LogLevel == "" {
		c.Logger.LogLevel = LevelInfo
	}

	if c.Logger.LogFormat == "" {
		c.Logger.LogFormat = JSONLogFormat
	}

	if c.Db.Driver == "" {
		c.Db.Driver = SQLiteNamedDriver
	}

	if c.Db.Dbname == "" {
		c.Db.Dbname = "store"
	}

	if c.Db.DBPath == "" {
		c.Db.DBPath = os.TempDir()
	}

	return nil
}

func (c *Config) getConfigFile() (string, error) {
	if c.configFile == "" {
		cf, err := c.findConfigFile()
		if err != nil {
			return "", err
		}
		c.configFile = filepath.Clean(cf)
	}
	return c.configFile, nil
}

func (c *Config) findConfigFile() (string, error) {
	slog.Info("searching for config in paths", "paths", c.configPaths)

	for _, cp := range c.configPaths {
		file := c.searchInPath(cp)
		if file != "" {
			return file, nil
		}
	}
	return "", fmt.Errorf("filename: %s %s", c.configName, c.configPaths)
}

func (c *Config) searchInPath(in string) (filename string) {
	slog.Debug("searching for config in path", "path", in)
	for _, ext := range SupportedExts {
		slog.Debug("checking if file exists", "file", filepath.Join(in, c.configName+"."+ext))
		if b, _ := exists(c.fs, filepath.Join(in, c.configName+"."+ext)); b {
			slog.Debug("found file", "file", filepath.Join(in, c.configName+"."+ext))
			return filepath.Join(in, c.configName+"."+ext)
		}
	}

	if c.configType != "" {
		if b, _ := exists(c.fs, filepath.Join(in, c.configName)); b {
			return filepath.Join(in, c.configName)
		}
	}

	return ""
}

// OnConfigChange sets the event handler that is called when a config file changes.
func (c *Config) OnConfigChange(run func(in fsnotify.Event)) {
	c.onConfigChange = run
}

// OnConfigChange sets the event handler that is called when a config file changes.
func OnConfigChange(run func(in fsnotify.Event)) {
	GetConfig.OnConfigChange(run)
}

func (c *Config) watchTask() {
	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			slog.Error(fmt.Sprintf("failed to create watcher: %s", err))
			os.Exit(1)
		}
		defer watcher.Close()
		// we have to watch the entire directory to pick up renames/atomic saves in a cross-platform way
		filename, err := c.getConfigFile()
		if err != nil {
			slog.Error(fmt.Sprintf("get config file: %s", err))
			c.initWG.Done()
			return
		}

		configDir, _ := filepath.Split(filename)
		realConfigFile, _ := filepath.EvalSymlinks(filename)

		c.eventsWG.Add(1)
		go func() {
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok { // 'Events' channel is closed
						c.eventsWG.Done()
						return
					}
					currentConfigFile, _ := filepath.EvalSymlinks(filename)
					// we only care about the config file with the following cases:
					// 1 - if the config file was modified or created
					// 2 - if the real path to the config file changed (eg: k8s ConfigMap replacement)
					if (filepath.Clean(event.Name) == filename &&
						(event.Has(fsnotify.Write) || event.Has(fsnotify.Create))) ||
						(currentConfigFile != "" && currentConfigFile != realConfigFile) {
						realConfigFile = currentConfigFile
						if err := c.ReadInConfig(); err != nil {
							slog.Error(fmt.Sprintf("read config file: %s", err))
						}
						if c.onConfigChange != nil {
							c.onConfigChange(event)
						}
					} else if filepath.Clean(event.Name) == filename && event.Has(fsnotify.Remove) {
						c.eventsWG.Done()
						return
					}

				case err, ok := <-watcher.Errors:
					if ok { // 'Errors' channel is not closed
						slog.Error(fmt.Sprintf("watcher error: %s", err))
					}
					c.eventsWG.Done()
					return
				}
			}
		}()
		watcher.Add(configDir)
		c.initWG.Done()   // done initializing the watch in this go routine, so the parent routine can move on...
		c.eventsWG.Wait() // now, wait for event loop to end in this go-routine...
	}()
}

func (c *Config) WatchConfig() {
	c.initWG.Add(1)
	go c.watchTask()
	c.initWG.Wait() // make sure that the go routine above fully ended before returning
}

func (c *Config) ReadInConfig() error {
	slog.Info("attempting to read in config file")
	filename, err := c.getConfigFile()
	if err != nil {
		return err
	}

	slog.Debug("reading file", "file", filename)
	file, err := afero.ReadFile(c.fs, filename)
	if err != nil {
		return err
	}

	viper.SetConfigFile(filename)
	viper.AutomaticEnv()

	if err = viper.ReadConfig(bytes.NewReader(file)); err != nil {
		return fmt.Errorf("fatal error config file: %s", err)
	}

	if err = viper.Unmarshal(GetConfig); err != nil {
		return fmt.Errorf("fatal error config file: %s", err)
	}

	return nil
}

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

func SetConfig(cfgFile string) {
	if cfgFile == "" {
		cfgFile = os.Getenv("CONFIG_FILE")
	}

	var err error
	GetConfig.configFile, err = filepath.Abs(cfgFile)
	if err != nil {
		slog.Error(fmt.Sprintf("config file: %s", err))
		os.Exit(1)
	}

	if err = GetConfig.ReadInConfig(); err != nil {
		slog.Error(fmt.Sprintf("read in config: %s", err))
		os.Exit(1)
	}

	GetConfig.WatchConfig()
}

func writeToFile(cfgFile string) error {
	file, err := os.Create(cfgFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	return encoder.Encode(GetConfig)
}

func DefaultConfig() error {
	if err := GetConfig.defaultValues(); err != nil {
		return err
	}

	return writeToFile("config.yaml")
}

func exists(fs afero.Fs, path string) (bool, error) {
	stat, err := fs.Stat(path)
	if err == nil {
		return !stat.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
