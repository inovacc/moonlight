package installer

import (
	"context"
	"github.com/inovacc/moonlight/internal/database"
	"testing"
)

func TestNewInstaller(t *testing.T) {
	if err := database.NewDatabase(); err != nil {
		t.Error("Expected database to be initialized")
	}

	installer, err := NewInstaller(context.Background(), database.GetConnection())
	if err != nil {
		t.Fatal(err)
	}

	if installer == nil {
		t.Fatal("Expected installer to be initialized")
	}

	if err = installer.Command("go install github.com/google/gops@latest"); err != nil {
		t.Fatal(err)
	}
}
