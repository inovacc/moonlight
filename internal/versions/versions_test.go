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

	result, err := mapVerse.GetByOS("linux")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) == 0 {
		t.Fatal("No data found")
	}

	result, err = mapVerse.GetByArch("amd64")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) == 0 {
		t.Fatal("No data found")
	}

	result, err = mapVerse.GetByKind("source")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) == 0 {
		t.Fatal("No data found")
	}

	result, err = mapVerse.GetByStable()
	if err != nil {
		t.Fatal(err)
	}
}
