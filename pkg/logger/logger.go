package logger

import (
	"github.com/robfig/cron/v3"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"log/slog"
	"os"
	"strings"
)

const (
	logExtension        = ".log"
	loggerSchedulerName = "log-rotation"
)

var (
	d         *logRotator
	scheduler *cron.Cron
)

func init() {
	d = &logRotator{tee: os.Stdout}
	slog.SetDefault(slog.New(slog.NewJSONHandler(d.tee, nil)))
}

type logRotator struct {
	filename     string
	maxSize      int
	maxAge       int
	maxBackups   int
	localTime    bool
	compress     bool
	rotationTime string
	stdout       bool
	tee          io.Writer
}

type RotatedLogger struct {
	*lumberjack.Logger
}

// NewRotatorHandler creates a new RotatedLogger that implements lumberjack.Logger compatible with slog.Handler
func NewRotatorHandler(filename string, maxSize int, maxAge int, maxBackups int, localTime bool, compress bool) *RotatedLogger {
	if !strings.Contains(filename, logExtension) {
		filename += logExtension
	}

	return &RotatedLogger{
		Logger: &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			MaxAge:     maxAge,
			Compress:   compress,
			LocalTime:  localTime,
		},
	}
}

// SetScheduler sets up a cron job to rotate logs
func (r *RotatedLogger) SetScheduler(spec string) {
	scheduler = cron.New(cron.WithSeconds())
	if _, err := scheduler.AddFunc(spec, func() {
		if err := r.Logger.Rotate(); err != nil {
			slog.Error("error rotating logs", slog.String(loggerSchedulerName, err.Error()))
		}
	}); err != nil {
		slog.Error("error setting up cron job", slog.String(loggerSchedulerName, err.Error()))
	}
	scheduler.Start()
}

// Rotate rotates the logs manually
func (r *RotatedLogger) Rotate() error {
	return r.Logger.Rotate()
}

// NewLoggerWithJSONRotator creates a new slog.Logger with a RotatedLogger handler and JSON formatter
func NewLoggerWithJSONRotator(rotator *RotatedLogger, opts *slog.HandlerOptions) {
	if !d.stdout {
		d.tee = io.MultiWriter(rotator, d.tee)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(d.tee, opts)))
}

// NewLoggerWithTextRotator creates a new slog.Logger with a RotatedLogger handler and Text formatter
func NewLoggerWithTextRotator(rotator *RotatedLogger, opts *slog.HandlerOptions) {
	if !d.stdout {
		d.tee = io.MultiWriter(rotator, d.tee)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(d.tee, opts)))
}

// EnableStdout enables logging to stdout and ignores writing to file
func EnableStdout() {
	d.stdout = true
}
