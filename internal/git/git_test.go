package git

import "testing"

func TestNewRepoGit(t *testing.T) {
	repo, err := NewRepoGit("golang.org/x/tools/gopls")
	if err != nil {
		t.Fatalf("NewRepoGit() failed: %v", err)
	}

	if repo == nil {
		t.Fatalf("NewRepoGit() failed: expected repo to be non-nil")
	}

	repo.SetDepth(1)

	if err = repo.Memory(); err != nil {
		t.Fatalf("Memory() failed: %v", err)
	}
}
