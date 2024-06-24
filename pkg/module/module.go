package module

import (
	"context"
	"github.com/inovacc/moonlight/pkg/module/internal/modload"
)

// ListModules returns a description of the modules matching args, if known,
// along with any error preventing additional matches from being identified.
//
// The returned slice can be nonempty even if the error is non-nil.
func ListModules(ctx context.Context, req []string) ([]*modload.ModulePublic, error) {
	mods, err := modload.ListModules(ctx, req)
	if err != nil {
		return nil, err
	}
	return mods, nil
}
