package versions

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetGoVersions(t *testing.T) {
	newGoVersions, err := NewGoVersion()
	if err != nil {
		t.Errorf("NewGoVersion() error = %v", err)
		return
	}

	version := newGoVersions.Stable
	if version == "" {
		t.Errorf("NewGoVersion() = %v, want a valid version", version)
	}

	assert.Equal(t, "go1.22.4", version)
}
