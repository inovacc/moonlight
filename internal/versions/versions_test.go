package versions

import (
	"testing"
)

func TestGetGoVersions(t *testing.T) {
	newGoVersions, err := NewGoVersion()
	if err != nil {
		t.Errorf("NewGoVersion() error = %v", err)
		return
	}

	goVer, err := newGoVersions.GetGoVersion()
	if err != nil {
		t.Errorf("GetGoVersions() error = %v", err)
		return
	}

	if len(goVer.Version) == 0 {
		t.Errorf("GetGoVersions() got = %v, want > 0", len(goVer.Version))
	}

	version, ok := goVer.GetWindows()
	if !ok {
		t.Errorf("GetWindows() got = %v, want true", ok)
	}

	if version == nil {
		t.Errorf("GetWindows() got = %v, want not nil", version)
	}

	t.Logf("Go version: %v", version)
}
