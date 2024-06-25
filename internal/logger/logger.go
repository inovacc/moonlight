package logger

import (
	"fmt"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/robfig/cron/v3"
)

var d *logRotator

func init() {
	d = &logRotator{
		filename:     "app.log",
		maxSize:      500,
		maxAge:       28,
		maxBackups:   3,
		rotationTime: "@daily", // Use cron format for rotation time
		localTime:    true,
		compress:     true,
		tee:          os.Stdout,
		scheduler:    cron.New(),
	}

	// Initialize the log rotator
	rotator := NewRotatorHandler(d.filename, d.maxSize, d.maxAge, d.maxBackups, d.localTime, d.compress)
	d.handler = slog.NewJSONHandler(io.MultiWriter(rotator, d.tee), nil)

	// Set up the cron job for log rotation
	d.setupCronJob()
}

type logRotator struct {
	filename     string
	maxSize      int
	maxAge       int
	maxBackups   int
	localTime    bool
	compress     bool
	rotationTime string
	handler      slog.Handler
	tee          io.Writer
	scheduler    *cron.Cron
}

func DisableStdout() {
	d.tee = nil
}

func NewRotatorHandler(filename string, maxSize int, maxAge int, maxBackups int, localTime bool, compress bool) *lumberjack.Logger {
	if !strings.Contains(filename, ".log") {
		filename = fmt.Sprintf("%s.log", filename)
	}

	return &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   compress,
		LocalTime:  localTime,
	}
}

func NewLoggerWithJSONRotator(rotator *lumberjack.Logger, opts *slog.HandlerOptions) {
	logger := slog.New(slog.NewJSONHandler(io.MultiWriter(rotator, d.tee), opts))
	slog.SetDefault(logger)
}

func NewLoggerWithTextRotator(rotator *lumberjack.Logger, opts *slog.HandlerOptions) {
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(rotator, d.tee), opts))
	slog.SetDefault(logger)
}

func (lr *logRotator) setupCronJob() {
	_, err := lr.scheduler.AddFunc(lr.rotationTime, func() {
		lr.rotateLogs()
	})
	if err != nil {
		fmt.Printf("Error setting up cron job: %v\n", err)
		return
	}
	lr.scheduler.Start()
}

func (lr *logRotator) rotateLogs() {
	lr.handler = slog.NewJSONHandler(io.MultiWriter(NewRotatorHandler(lr.filename, lr.maxSize, lr.maxAge, lr.maxBackups, lr.localTime, lr.compress), lr.tee), nil)
}
