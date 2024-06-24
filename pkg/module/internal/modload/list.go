// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modload

import (
	"context"
	"errors"
	"fmt"
	"github.com/inovacc/moonlight/pkg/module/internal/gover"
	"github.com/inovacc/moonlight/pkg/module/internal/modfetch/codehost"
	"github.com/inovacc/moonlight/pkg/module/internal/pkgpattern"
	"os"
	"strings"

	"golang.org/x/mod/module"
)

type ListMode int

const (
	ListU ListMode = 1 << iota
	ListRetracted
	ListDeprecated
	ListVersions
	ListRetractedVersions
)

// ListModules returns a description of the modules matching args, if known,
// along with any error preventing additional matches from being identified.
//
// The returned slice can be nonempty even if the error is non-nil.
func ListModules(ctx context.Context, args []string) ([]*ModulePublic, error) {
	var mods []*ModulePublic
	var reuse map[module.Version]*ModulePublic
	var mg *ModuleGraph
	var rs *Requirements
	var mgErr error

	var mode = ListU | ListRetracted | ListDeprecated | ListVersions
	matchedModule := map[module.Version]bool{}
	for _, arg := range args {
		if path, vers, found := strings.Cut(arg, "@"); found {
			var current string

			allowed := CheckAllowed
			if IsRevisionQuery(path, vers) || mode&ListRetracted != 0 {
				// Allow excluded and retracted versions if the user asked for a
				// specific revision or used 'go list -retracted'.
				allowed = nil
			}
			info, err := Query(ctx, path, vers, current, allowed)
			if err != nil {
				var origin *codehost.Origin
				if info != nil {
					origin = info.Origin
				}
				mods = append(mods, &ModulePublic{
					Path:    path,
					Version: vers,
					Error:   modinfoError(path, vers, err),
					Origin:  origin,
				})
				continue
			}

			// Indicate that m was resolved from outside of rs by passing a nil
			// *Requirements instead.
			var noRS *Requirements

			mod := moduleInfo(ctx, noRS, module.Version{Path: path, Version: info.Version}, mode, reuse)
			if vers != mod.Version {
				mod.Query = vers
			}
			mod.Origin = info.Origin
			mods = append(mods, mod)
			continue
		}

		// Module path or pattern.
		var match func(string) bool
		if arg == "all" {
			match = func(p string) bool { return !gover.IsToolchain(p) }
		} else if strings.Contains(arg, "...") {
			mp := pkgpattern.MatchPattern(arg)
			match = func(p string) bool { return mp(p) && !gover.IsToolchain(p) }
		} else {
			var v string
			if mg == nil {
				var ok bool
				v, ok = rs.rootSelected(arg)
				if !ok {
					// We checked rootSelected(arg) in the earlier args loop, so if there
					// is no such root we should have loaded a non-nil mg.
					panic(fmt.Sprintf("internal error: root requirement expected but not found for %v", arg))
				}
			} else {
				v = mg.Selected(arg)
			}
			if v == "none" && mgErr != nil {
				// mgErr is already set, so just skip this module.
				continue
			}
			if v != "none" {
				mods = append(mods, moduleInfo(ctx, rs, module.Version{Path: arg, Version: v}, mode, reuse))
				/*	} else if cfg.BuildMod == "vendor" {
					// In vendor mode, we can't determine whether a missing module is “a
					// known dependency” because the module graph is incomplete.
					// Give a more explicit error message.
					mods = append(mods, &modinfo.ModulePublic{
						Path:  arg,
						Error: modinfoError(arg, "", errors.New("can't resolve module using the vendor directory\n\t(Use -mod=mod or -mod=readonly to bypass.)")),
					})
				} */
			} else if mode&ListVersions != 0 {
				// Don't make the user provide an explicit '@latest' when they're
				// explicitly asking what the available versions are. Instead, return a
				// module with version "none", to which we can add the requested list.
				mods = append(mods, &ModulePublic{Path: arg})
			} else {
				mods = append(mods, &ModulePublic{
					Path:  arg,
					Error: modinfoError(arg, "", errors.New("not a known dependency")),
				})
			}
			continue
		}

		matched := false
		for _, m := range mg.BuildList() {
			if match(m.Path) {
				matched = true
				if !matchedModule[m] {
					matchedModule[m] = true
					mods = append(mods, moduleInfo(ctx, rs, m, mode, reuse))
				}
			}
		}
		if !matched {
			fmt.Fprintf(os.Stderr, "warning: pattern %q matched no module dependencies\n", arg)
		}

	}

	return mods, mgErr
	//var reuse map[module.Version]*ModulePublic
	////if reuseFile != "" {
	////	data, err := os.ReadFile(reuseFile)
	////	if err != nil {
	////		return nil, err
	////	}
	////	dec := json.NewDecoder(bytes.NewReader(data))
	////	reuse = make(map[module.Version]*modinfo.ModulePublic)
	////	for {
	////		var m modinfo.ModulePublic
	////		if err := dec.Decode(&m); err != nil {
	////			if err == io.EOF {
	////				break
	////			}
	////			return nil, fmt.Errorf("parsing %s: %v", reuseFile, err)
	////		}
	////		if m.Origin == nil {
	////			continue
	////		}
	////		m.Reuse = true
	////		reuse[module.Version{Path: m.Path, Version: m.Version}] = &m
	////		if m.Query != "" {
	////			reuse[module.Version{Path: m.Path, Version: m.Query}] = &m
	////		}
	////	}
	////}
	//
	//lmf := LoadModFile(ctx)
	//rs, mods, err := listModules(ctx, lmf, args, mode, reuse)
	//
	//type token struct{}
	//sem := make(chan token, runtime.GOMAXPROCS(0))
	//if mode != 0 {
	//	for _, m := range mods {
	//		if m.Reuse {
	//			continue
	//		}
	//		add := func(m *ModulePublic) {
	//			sem <- token{}
	//			go func() {
	//				if mode&ListU != 0 {
	//					addUpdate(ctx, m)
	//				}
	//				if mode&ListVersions != 0 {
	//					addVersions(ctx, m, mode&ListRetractedVersions != 0)
	//				}
	//				if mode&ListRetracted != 0 {
	//					addRetraction(ctx, m)
	//				}
	//				if mode&ListDeprecated != 0 {
	//					addDeprecation(ctx, m)
	//				}
	//				<-sem
	//			}()
	//		}
	//
	//		add(m)
	//		if m.Replace != nil {
	//			add(m.Replace)
	//		}
	//	}
	//}
	//// Fill semaphore channel to wait for all tasks to finish.
	//for n := cap(sem); n > 0; n-- {
	//	sem <- token{}
	//}
	//
	//if err == nil {
	//	requirements = rs
	//	//// TODO(#61605): The extra ListU clause fixes a problem with Go 1.21rc3
	//	//// where "go mod tidy" and "go list -m -u all" fight over whether the go.sum
	//	//// should be considered up-to-date. The fix for now is to always treat the
	//	//// go.sum as up-to-date during list -m -u. Probably the right fix is more targeted,
	//	//// but in general list -u is looking up other checksums in the checksum database
	//	//// that won't be necessary later, so it makes sense not to write the go.sum back out.
	//	//if !ExplicitWriteGoMod && mode&ListU == 0 {
	//	//	err = commitRequirements(ctx, WriteOpts{})
	//	//}
	//}
	//return mods, err
}

func modinfoError(path, vers string, err error) *ModuleError {
	var nerr *NoMatchingVersionError
	var merr *module.ModuleError
	if errors.As(err, &nerr) {
		// NoMatchingVersionError contains the query, so we don't mention the
		// query again in ModuleError.
		err = &module.ModuleError{Path: path, Err: err}
	} else if !errors.As(err, &merr) {
		// If the error does not contain path and version, wrap it in a
		// module.ModuleError.
		err = &module.ModuleError{Path: path, Version: vers, Err: err}
	}

	return &ModuleError{Err: err.Error()}
}
