package module

import (
	"context"
	modload2 "github.com/inovacc/moonlight/internal/module/internal/modload"
)

// ListModules returns a description of the modules matching args, if known,
// along with any error preventing additional matches from being identified.
//
// The returned slice can be nonempty even if the error is non-nil.
func ListModules(ctx context.Context, req []string) ([]*modload2.ModulePublic, error) {
	mods, err := modload2.ListModules(ctx, req)
	if err != nil {
		return nil, err
	}
	return mods, nil
}
