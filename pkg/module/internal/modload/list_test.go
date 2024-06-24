package modload

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestListModules(t *testing.T) {
	moduleName := []string{"golang.org/x/tools/gopls@latest"}

	ctx := context.Background()
	mods, err := ListModules(ctx, moduleName)
	if err != nil {
		t.Fatalf("go: %v", err)
	}

	for _, mod := range mods {
		t.Log("mod:", mod)
		t.Log("versions:", mod.Versions)
		assert.Equal(t, "golang.org/x/tools/gopls v0.16.0", mod.String())
	}
}
