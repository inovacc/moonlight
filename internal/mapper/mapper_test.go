package mapper

import (
	"github.com/inovacc/moonlight/pkg/database"
	"github.com/inovacc/moonlight/pkg/versions"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewMapVersions(t *testing.T) {
	if err := database.NewDatabase(); err != nil {
		t.Error("Expected database to be initialized")
	}

	goVer, err := versions.NewGoVersion()
	if err != nil {
		t.Fatal(err)
	}

	mapVerse, err := NewMapVersions(database.GetConnection(), goVer)
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

	result, err = mapVerse.GetByOSArch("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) == 0 {
		t.Fatal("No data found")
	}

	result, err = mapVerse.GetByOSKind("windows", "installer")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) == 0 {
		t.Fatal("No data found")
	}

	result, err = mapVerse.GetByArchKind("amd64", "installer")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) == 0 {
		t.Fatal("No data found")
	}

	result, err = mapVerse.GetByOSArchKind("linux", "amd64", "archive")
	if err != nil {
		t.Fatal(err)
	}

	result, err = mapVerse.GetByOSArchKind("any", "any", "source")
	if err != nil {
		t.Fatal(err)
	}

	version, err := mapVerse.GetLatest()
	if err != nil {
		t.Fatal(err)
	}

	if version == nil {
		t.Fatal("No data found")
	}

	assert.Equal(t, "go1.22.4", goVer.StableVersion)
}
