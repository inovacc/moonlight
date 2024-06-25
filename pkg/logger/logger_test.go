package logger

import (
	"log/slog"
	"testing"
)

func TestNewJSONRotatorHandler(t *testing.T) {
	r := NewRotatorHandler("filename", 1, 2, 3, true, true)
	NewLoggerWithJSONRotator(r, nil)

	slog.Info("test 1")
	slog.Info("test 2")
	slog.Info("test 3")

	if err := r.Rotate(); err != nil {
		t.Errorf("Error rotating logs: %v", err)
	}

	slog.Info("test 4")
	slog.Info("test 5")
	slog.Info("test 6")
}
