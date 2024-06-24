// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modload

import (
	"context"
	"errors"
	"github.com/inovacc/moonlight/internal/module/internal/gover"
	"github.com/inovacc/moonlight/internal/module/internal/modfetch"
	"github.com/inovacc/moonlight/internal/module/internal/modfetch/codehost"
	"golang.org/x/mod/module"
	"io/fs"
	"os"
	"path/filepath"
)

// mergeOrigin returns the union of data from two origins,
// returning either a new origin or one of its unmodified arguments.
// If the two origins conflict including if either is nil,
// mergeOrigin returns nil.
func mergeOrigin(m1, m2 *codehost.Origin) *codehost.Origin {
	if m1 == nil || m2 == nil {
		return nil
	}

	if m2.VCS != m1.VCS ||
		m2.URL != m1.URL ||
		m2.Subdir != m1.Subdir {
		return nil
	}

	merged := *m1
	if m2.Hash != "" {
		if m1.Hash != "" && m1.Hash != m2.Hash {
			return nil
		}
		merged.Hash = m2.Hash
	}
	if m2.TagSum != "" {
		if m1.TagSum != "" && (m1.TagSum != m2.TagSum || m1.TagPrefix != m2.TagPrefix) {
			return nil
		}
		merged.TagSum = m2.TagSum
		merged.TagPrefix = m2.TagPrefix
	}
	if m2.Ref != "" {
		if m1.Ref != "" && m1.Ref != m2.Ref {
			return nil
		}
		merged.Ref = m2.Ref
	}

	switch {
	case merged == *m1:
		return m1
	case merged == *m2:
		return m2
	default:
		// Clone the result to avoid an alloc for merged
		// if the result is equal to one of the arguments.
		clone := merged
		return &clone
	}
}

// addRetraction fills in m.Retracted if the module was retracted by its author.
// m.Error is set if there's an error loading retraction information.
func addRetraction(ctx context.Context, m *ModulePublic) {
	if m.Version == "" {
		return
	}

	err := CheckRetractions(ctx, module.Version{Path: m.Path, Version: m.Version})
	var noVersionErr *NoMatchingVersionError
	var retractErr *ModuleRetractedError
	if err == nil || errors.Is(err, fs.ErrNotExist) || errors.As(err, &noVersionErr) {
		// Ignore "not found" and "no matching version" errors.
		// This means the proxy has no matching version or no versions at all.
		//
		// We should report other errors though. An attacker that controls the
		// network shouldn't be able to hide versions by interfering with
		// the HTTPS connection. An attacker that controls the proxy may still
		// hide versions, since the "list" and "latest" endpoints are not
		// authenticated.
		return
	} else if errors.As(err, &retractErr) {
		if len(retractErr.Rationale) == 0 {
			m.Retracted = []string{"retracted by module author"}
		} else {
			m.Retracted = retractErr.Rationale
		}
	} else if m.Error == nil {
		m.Error = &ModuleError{Err: err.Error()}
	}
}

// moduleInfo returns information about module m, loaded from the requirements
// in rs (which may be nil to indicate that m was not loaded from a requirement
// graph).
func moduleInfo(ctx context.Context, rs *Requirements, m module.Version, mode ListMode, reuse map[module.Version]*ModulePublic) *ModulePublic {
	if m.Version == "" && MainModules.Contains(m.Path) {
		info := &ModulePublic{
			Path:    m.Path,
			Version: m.Version,
			Main:    true,
		}
		if v, ok := rawGoVersion.Load(m); ok {
			info.GoVersion = v.(string)
		} else {
			panic("internal error: GoVersion not set for main module")
		}
		if modRoot := MainModules.ModRoot(m); modRoot != "" {
			info.Dir = modRoot
			info.GoMod = modFilePath(modRoot)
		}
		return info
	}

	info := &ModulePublic{
		Path:     m.Path,
		Version:  m.Version,
		Indirect: rs != nil && !rs.direct[m.Path],
	}
	if v, ok := rawGoVersion.Load(m); ok {
		info.GoVersion = v.(string)
	}

	// completeFromModCache fills in the extra fields in m using the module cache.
	completeFromModCache := func(m *ModulePublic) {
		if gover.IsToolchain(m.Path) {
			return
		}

		checksumOk := func(suffix string) bool {
			return rs == nil || m.Version == "" || !mustHaveSums() ||
				modfetch.HaveSum(module.Version{Path: m.Path, Version: m.Version + suffix})
		}

		mod := module.Version{Path: m.Path, Version: m.Version}

		if m.Version != "" {
			if old := reuse[mod]; old != nil {
				if err := checkReuse(ctx, mod, old.Origin); err == nil {
					*m = *old
					m.Query = ""
					m.Dir = ""
					return
				}
			}

			if q, err := Query(ctx, m.Path, m.Version, "", nil); err != nil {
				m.Error = &ModuleError{Err: err.Error()}
			} else {
				m.Version = q.Version
				m.Time = &q.Time
			}
		}

		if m.GoVersion == "" && checksumOk("/go.mod") {
			// Load the go.mod file to determine the Go version, since it hasn't
			// already been populated from rawGoVersion.
			if summary, err := rawGoModSummary(mod); err == nil && summary.goVersion != "" {
				m.GoVersion = summary.goVersion
			}
		}

		if m.Version != "" {
			if checksumOk("/go.mod") {
				gomod, err := modfetch.CachePath(ctx, mod, "mod")
				if err == nil {
					if info, err := os.Stat(gomod); err == nil && info.Mode().IsRegular() {
						m.GoMod = gomod
					}
				}
			}
			if checksumOk("") {
				dir, err := modfetch.DownloadDir(ctx, mod)
				if err == nil {
					m.Dir = dir
				}
			}

			if mode&ListRetracted != 0 {
				addRetraction(ctx, m)
			}
		}
	}

	if rs == nil {
		// If this was an explicitly-versioned argument to 'go mod download' or
		// 'go list -m', report the actual requested version, not its replacement.
		completeFromModCache(info) // Will set m.Error in vendor mode.
		return info
	}

	r := Replacement(m)
	if r.Path == "" {
		//if cfg.BuildMod == "vendor" {
		//	// It's tempting to fill in the "Dir" field to point within the vendor
		//	// directory, but that would be misleading: the vendor directory contains
		//	// a flattened package tree, not complete modules, and it can even
		//	// interleave packages from different modules if one module path is a
		//	// prefix of the other.
		//} else {
		completeFromModCache(info)
		//}
		return info
	}

	// Don't hit the network to fill in extra data for replaced modules.
	// The original resolved Version and Time don't matter enough to be
	// worth the cost, and we're going to overwrite the GoMod and Dir from the
	// replacement anyway. See https://golang.org/issue/27859.
	info.Replace = &ModulePublic{
		Path:    r.Path,
		Version: r.Version,
	}
	if v, ok := rawGoVersion.Load(m); ok {
		info.Replace.GoVersion = v.(string)
	}
	if r.Version == "" {
		if filepath.IsAbs(r.Path) {
			info.Replace.Dir = r.Path
		} else {
			info.Replace.Dir = filepath.Join(replaceRelativeTo(), r.Path)
		}
		info.Replace.GoMod = filepath.Join(info.Replace.Dir, "go.mod")
	}
	//if cfg.BuildMod != "vendor" {
	//	completeFromModCache(info.Replace)
	//	info.Dir = info.Replace.Dir
	//	info.GoMod = info.Replace.GoMod
	//	info.Retracted = info.Replace.Retracted
	//}
	info.GoVersion = info.Replace.GoVersion
	return info
}
