package versions

import (
	"moonligth/pkg/versions"
	"testing"
)

func TestNewMapVersions(t *testing.T) {
	goVer, err := versions.NewGoVersion()
	if err != nil {
		t.Fatal(err)
	}

	mapVerse, err := NewMapVersions(goVer)
	if err != nil {
		t.Fatal(err)
	}
	defer mapVerse.db.Close()
}
