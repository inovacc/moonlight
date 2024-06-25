package logger

import (
	"log/slog"
	"testing"
)

func TestNewJSONRotatorHandler(t *testing.T) {
	r := NewRotatorHandler("filename", 1, 2, 3, true, true)
	NewLoggerWithJSONRotator(r, nil)

	slog.Info("test")
}
