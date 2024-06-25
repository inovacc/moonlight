package config

import (
	"testing"
)

func TestConfig(t *testing.T) {
	SetConfig("../../config.yaml")

	if err := DefaultConfig(); err != nil {
		t.Errorf("Error rotating logs: %v", err)
	}
}
