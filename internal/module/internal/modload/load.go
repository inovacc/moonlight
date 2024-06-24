// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modload

// This file contains the module-mode package loader, as well as some accessory
// functions pertaining to the package import graph.
//
// There are two exported entry points into package loading — LoadPackages and
// ImportFromFiles — both implemented in terms of loadFromRoots, which itself
// manipulates an instance of the loader struct.
//
// Although most of the loading state is maintained in the loader struct,
// one key piece - the build list - is a global, so that it can be modified
// separate from the loading operation, such as during "go get"
// upgrades/downgrades or in "go mod" operations.
// TODO(#40775): It might be nice to make the loader take and return
// a buildList rather than hard-coding use of the global.
//
// Loading is an iterative process. On each iteration, we try to load the
// requested packages and their transitive imports, then try to resolve modules
// for any imported packages that are still missing.
//
// The first step of each iteration identifies a set of “root” packages.
// Normally the root packages are exactly those matching the named pattern
// arguments. However, for the "all" meta-pattern, the final set of packages is
// computed from the package import graph, and therefore cannot be an initial
// input to loading that graph. Instead, the root packages for the "all" pattern
// are those contained in the main module, and allPatternIsRoot parameter to the
// loader instructs it to dynamically expand those roots to the full "all"
// pattern as loading progresses.
//
// The pkgInAll flag on each loadPkg instance tracks whether that
// package is known to match the "all" meta-pattern.
// A package matches the "all" pattern if:
// 	- it is in the main module, or
// 	- it is imported by any test in the main module, or
// 	- it is imported by another package in "all", or
// 	- the main module specifies a go version ≤ 1.15, and the package is imported
// 	  by a *test of* another package in "all".
//
// When graph pruning is in effect, we want to spot-check the graph-pruning
// invariants — which depend on which packages are known to be in "all" — even
// when we are only loading individual packages, so we set the pkgInAll flag
// regardless of the whether the "all" pattern is a root.
// (This is necessary to maintain the “import invariant” described in
// https://golang.org/design/36460-lazy-module-loading.)
//
// Because "go mod vendor" prunes out the tests of vendored packages, the
// behavior of the "all" pattern with -mod=vendor in Go 1.11–1.15 is the same
// as the "all" pattern (regardless of the -mod flag) in 1.16+.
// The loader uses the GoVersion parameter to determine whether the "all"
// pattern should close over tests (as in Go 1.11–1.15) or stop at only those
// packages transitively imported by the packages and tests in the main module
// ("all" in Go 1.16+ and "go mod vendor" in Go 1.11+).
//
// Note that it is possible for a loaded package NOT to be in "all" even when we
// are loading the "all" pattern. For example, packages that are transitive
// dependencies of other roots named on the command line must be loaded, but are
// not in "all". (The mod_notall test illustrates this behavior.)
// Similarly, if the LoadTests flag is set but the "all" pattern does not close
// over test dependencies, then when we load the test of a package that is in
// "all" but outside the main module, the dependencies of that test will not
// necessarily themselves be in "all". (That configuration does not arise in Go
// 1.11–1.15, but it will be possible in Go 1.16+.)
//
// Loading proceeds from the roots, using a parallel work-queue with a limit on
// the amount of active work (to avoid saturating disks, CPU cores, and/or
// network connections). Each package is added to the queue the first time it is
// imported by another package. When we have finished identifying the imports of
// a package, we add the test for that package if it is needed. A test may be
// needed if:
// 	- the package matches a root pattern and tests of the roots were requested, or
// 	- the package is in the main module and the "all" pattern is requested
// 	  (because the "all" pattern includes the dependencies of tests in the main
// 	  module), or
// 	- the package is in "all" and the definition of "all" we are using includes
// 	  dependencies of tests (as is the case in Go ≤1.15).
//
// After all available packages have been loaded, we examine the results to
// identify any requested or imported packages that are still missing, and if
// so, which modules we could add to the module graph in order to make the
// missing packages available. We add those to the module graph and iterate,
// until either all packages resolve successfully or we cannot identify any
// module that would resolve any remaining missing package.
//
// If the main module is “tidy” (that is, if "go mod tidy" is a no-op for it)
// and all requested packages are in "all", then loading completes in a single
// iteration.
// TODO(bcmills): We should also be able to load in a single iteration if the
// requested packages all come from modules that are themselves tidy, regardless
// of whether those packages are in "all". Today, that requires two iterations
// if those packages are not found in existing dependencies of the main module.

import (
	"context"
	"errors"
	"fmt"
	"github.com/rogpeppe/go-internal/imports"
	"log"
	"moduleList/sup/internal/base"
	"moduleList/sup/internal/gover"
	"moduleList/sup/internal/modindex"
	"moduleList/sup/internal/par"
	"moduleList/sup/internal/search"
	"moduleList/sup/internal/util"

	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/mod/module"
)

// loaded is the most recently-used package loader.
// It holds details about individual packages.
//
// This variable should only be accessed directly in top-level exported
// functions. All other functions that require or produce a *loader should pass
// or return it as an explicit parameter.
var loaded *loader

// PackageOpts control the behavior of the LoadPackages function.
type PackageOpts struct {
	// TidyGoVersion is the Go version to which the go.mod file should be updated
	// after packages have been loaded.
	//
	// An empty TidyGoVersion means to use the Go version already specified in the
	// main module's go.mod file, or the latest Go version if there is no main
	// module.
	TidyGoVersion string

	// Tags are the build tags in effect (as interpreted by the
	// github.com/dyammarcano/go-project/cmd/go/internal/imports package).
	// If nil, treated as equivalent to imports.Tags().
	Tags map[string]bool

	// Tidy, if true, requests that the build list and go.sum file be reduced to
	// the minimal dependencies needed to reproducibly reload the requested
	// packages.
	Tidy bool

	// TidyCompatibleVersion is the oldest Go version that must be able to
	// reproducibly reload the requested packages.
	//
	// If empty, the compatible version is the Go version immediately prior to the
	// 'go' version listed in the go.mod file.
	TidyCompatibleVersion string

	// VendorModulesInGOROOTSrc indicates that if we are within a module in
	// GOROOT/src, packages in the module's vendor directory should be resolved as
	// actual module dependencies (instead of standard-library packages).
	VendorModulesInGOROOTSrc bool

	// ResolveMissingImports indicates that we should attempt to add module
	// dependencies as needed to resolve imports of packages that are not found.
	//
	// For commands that support the -mod flag, resolving imports may still fail
	// if the flag is set to "readonly" (the default) or "vendor".
	ResolveMissingImports bool

	// AssumeRootsImported indicates that the transitive dependencies of the root
	// packages should be treated as if those roots will be imported by the main
	// module.
	AssumeRootsImported bool

	// AllowPackage, if non-nil, is called after identifying the module providing
	// each package. If AllowPackage returns a non-nil error, that error is set
	// for the package, and the imports and test of that package will not be
	// loaded.
	//
	// AllowPackage may be invoked concurrently by multiple goroutines,
	// and may be invoked multiple times for a given package path.
	AllowPackage func(ctx context.Context, path string, mod module.Version) error

	// LoadTests loads the test dependencies of each package matching a requested
	// pattern. If ResolveMissingImports is also true, test dependencies will be
	// resolved if missing.
	LoadTests bool

	// UseVendorAll causes the "all" package pattern to be interpreted as if
	// running "go mod vendor" (or building with "-mod=vendor").
	//
	// This is a no-op for modules that declare 'go 1.16' or higher, for which this
	// is the default (and only) interpretation of the "all" pattern in module mode.
	UseVendorAll bool

	// AllowErrors indicates that LoadPackages should not terminate the process if
	// an error occurs.
	AllowErrors bool

	// SilencePackageErrors indicates that LoadPackages should not print errors
	// that occur while matching or loading packages, and should not terminate the
	// process if such an error occurs.
	//
	// Errors encountered in the module graph will still be reported.
	//
	// The caller may retrieve the silenced package errors using the Lookup
	// function, and matching errors are still populated in the Errs field of the
	// associated search.Match.)
	SilencePackageErrors bool

	// SilenceMissingStdImports indicates that LoadPackages should not print
	// errors or terminate the process if an imported package is missing, and the
	// import path looks like it might be in the standard library (perhaps in a
	// future version).
	SilenceMissingStdImports bool

	// SilenceNoGoErrors indicates that LoadPackages should not print
	// imports.ErrNoGo errors.
	// This allows the caller to invoke LoadPackages (and report other errors)
	// without knowing whether the requested packages exist for the given tags.
	//
	// Note that if a requested package does not exist *at all*, it will fail
	// during module resolution and the error will not be suppressed.
	SilenceNoGoErrors bool

	// SilenceUnmatchedWarnings suppresses the warnings normally emitted for
	// patterns that did not match any packages.
	SilenceUnmatchedWarnings bool

	// Resolve the query against this module.
	MainModule module.Version

	// If Switcher is non-nil, then LoadPackages passes all encountered errors
	// to Switcher.Error and tries Switcher.Switch before base.ExitIfErrors.
	Switcher gover.Switcher
}

// DirImportPath returns the effective import path for dir,
// provided it is within a main module, or else returns ".".
func (mms *MainModuleSet) DirImportPath(ctx context.Context, dir string) (path string, m module.Version) {
	if !HasModRoot() {
		return ".", module.Version{}
	}
	LoadModFile(ctx) // Sets targetPrefix.

	if !filepath.IsAbs(dir) {
		dir = filepath.Join(base.Cwd(), dir)
	} else {
		dir = filepath.Clean(dir)
	}

	var longestPrefix string
	var longestPrefixPath string
	var longestPrefixVersion module.Version
	for _, v := range mms.Versions() {
		modRoot := mms.ModRoot(v)
		if dir == modRoot {
			return mms.PathPrefix(v), v
		}
		if util.HasFilePathPrefix(dir, modRoot) {
			pathPrefix := MainModules.PathPrefix(v)
			if pathPrefix > longestPrefix {
				longestPrefix = pathPrefix
				longestPrefixVersion = v
				suffix := filepath.ToSlash(util.TrimFilePathPrefix(dir, modRoot))
				if strings.HasPrefix(suffix, "vendor/") {
					longestPrefixPath = suffix[len("vendor/"):]
					continue
				}
				longestPrefixPath = filepath.Join(mms.PathPrefix(v), suffix)
			}
		}
	}
	if len(longestPrefix) > 0 {
		return longestPrefixPath, longestPrefixVersion
	}

	return ".", module.Version{}
}

// A loader manages the process of loading information about
// the required packages for a particular build,
// checking that the packages are available in the module set,
// and updating the module set if needed.
type loader struct {
	loaderParams

	// allClosesOverTests indicates whether the "all" pattern includes
	// dependencies of tests outside the main module (as in Go 1.11–1.15).
	// (Otherwise — as in Go 1.16+ — the "all" pattern includes only the packages
	// transitively *imported by* the packages and tests in the main module.)
	allClosesOverTests bool

	// skipImportModFiles indicates whether we may skip loading go.mod files
	// for imported packages (as in 'go mod tidy' in Go 1.17–1.20).
	skipImportModFiles bool

	work *par.Queue

	// reset on each iteration
	roots    []*loadPkg
	pkgCache *par.Cache[string, *loadPkg]
	pkgs     []*loadPkg // transitive closure of loaded packages and tests; populated in buildStacks
}

// loaderParams configure the packages loaded by, and the properties reported
// by, a loader instance.
type loaderParams struct {
	PackageOpts
	requirements *Requirements

	allPatternIsRoot bool // Is the "all" pattern an additional root?

	listRoots func(rs *Requirements) []string
}

func (ld *loader) reset() {
	select {
	case <-ld.work.Idle():
	default:
		panic("loader.reset when not idle")
	}

	ld.roots = nil
	ld.pkgCache = new(par.Cache[string, *loadPkg])
	ld.pkgs = nil
}

// error reports an error via either os.Stderr or base.Error,
// according to whether ld.AllowErrors is set.
func (ld *loader) error(err error) {
	if ld.AllowErrors {
		fmt.Fprintf(os.Stderr, "go: %v\n", err)
	} else if ld.Switcher != nil {
		ld.Switcher.Error(err)
	} else {
		log.Fatal(err)
	}
}

// switchIfErrors switches toolchains if a switch is needed.
func (ld *loader) switchIfErrors(ctx context.Context) {
	if ld.Switcher != nil {
		ld.Switcher.Switch(ctx)
	}
}

// exitIfErrors switches toolchains if a switch is needed
// or else exits if any errors have been reported.
func (ld *loader) exitIfErrors(ctx context.Context) {
	ld.switchIfErrors(ctx)
	base.ExitIfErrors()
}

// goVersion reports the Go version that should be used for the loader's
// requirements: ld.TidyGoVersion if set, or ld.requirements.GoVersion()
// otherwise.
func (ld *loader) goVersion() string {
	if ld.TidyGoVersion != "" {
		return ld.TidyGoVersion
	}
	return ld.requirements.GoVersion()
}

// A loadPkg records information about a single loaded package.
type loadPkg struct {
	// Populated at construction time:
	path   string // import path
	testOf *loadPkg

	// Populated at construction time and updated by (*loader).applyPkgFlags:
	flags atomicLoadPkgFlags

	// Populated by (*loader).load:
	mod         module.Version // module providing package
	dir         string         // directory containing source code
	err         error          // error loading package
	imports     []*loadPkg     // packages imported by this one
	testImports []string       // test-only imports, saved for use by pkg.test.
	inStd       bool
	altMods     []module.Version // modules that could have contained the package but did not

	// Populated by (*loader).pkgTest:
	testOnce sync.Once
	test     *loadPkg

	// Populated by postprocessing in (*loader).buildStacks:
	stack *loadPkg // package importing this one in minimal import stack for this pkg
}

// loadPkgFlags is a set of flags tracking metadata about a package.
type loadPkgFlags int8

const (
	// pkgInAll indicates that the package is in the "all" package pattern,
	// regardless of whether we are loading the "all" package pattern.
	//
	// When the pkgInAll flag and pkgImportsLoaded flags are both set, the caller
	// who set the last of those flags must propagate the pkgInAll marking to all
	// of the imports of the marked package.
	//
	// A test is marked with pkgInAll if that test would promote the packages it
	// imports to be in "all" (such as when the test is itself within the main
	// module, or when ld.allClosesOverTests is true).
	pkgInAll loadPkgFlags = 1 << iota

	// pkgIsRoot indicates that the package matches one of the root package
	// patterns requested by the caller.
	//
	// If LoadTests is set, then when pkgIsRoot and pkgImportsLoaded are both set,
	// the caller who set the last of those flags must populate a test for the
	// package (in the pkg.test field).
	//
	// If the "all" pattern is included as a root, then non-test packages in "all"
	// are also roots (and must be marked pkgIsRoot).
	pkgIsRoot

	// pkgFromRoot indicates that the package is in the transitive closure of
	// imports starting at the roots. (Note that every package marked as pkgIsRoot
	// is also trivially marked pkgFromRoot.)
	pkgFromRoot

	// pkgImportsLoaded indicates that the imports and testImports fields of a
	// loadPkg have been populated.
	pkgImportsLoaded
)

// has reports whether all of the flags in cond are set in f.
func (f loadPkgFlags) has(cond loadPkgFlags) bool {
	return f&cond == cond
}

// An atomicLoadPkgFlags stores a loadPkgFlags for which individual flags can be
// added atomically.
type atomicLoadPkgFlags struct {
	bits atomic.Int32
}

// update sets the given flags in af (in addition to any flags already set).
//
// update returns the previous flag state so that the caller may determine which
// flags were newly-set.
func (af *atomicLoadPkgFlags) update(flags loadPkgFlags) (old loadPkgFlags) {
	for {
		old := af.bits.Load()
		new := old | int32(flags)
		if new == old || af.bits.CompareAndSwap(old, new) {
			return loadPkgFlags(old)
		}
	}
}

// has reports whether all of the flags in cond are set in af.
func (af *atomicLoadPkgFlags) has(cond loadPkgFlags) bool {
	return loadPkgFlags(af.bits.Load())&cond == cond
}

// isTest reports whether pkg is a test of another package.
func (pkg *loadPkg) isTest() bool {
	return pkg.testOf != nil
}

// fromExternalModule reports whether pkg was loaded from a module other than
// the main module.
func (pkg *loadPkg) fromExternalModule() bool {
	if pkg.mod.Path == "" {
		return false // loaded from the standard library, not a module
	}
	return !MainModules.Contains(pkg.mod.Path)
}

//// updateRequirements ensures that ld.requirements is consistent with the
//// information gained from ld.pkgs.
////
//// In particular:
////
////   - Modules that provide packages directly imported from the main module are
////     marked as direct, and are promoted to explicit roots. If a needed root
////     cannot be promoted due to -mod=readonly or -mod=vendor, the importing
////     package is marked with an error.
////
////   - If ld scanned the "all" pattern independent of build constraints, it is
////     guaranteed to have seen every direct import. Module dependencies that did
////     not provide any directly-imported package are then marked as indirect.
////
////   - Root dependencies are updated to their selected versions.
////
//// The "changed" return value reports whether the update changed the selected
//// version of any module that either provided a loaded package or may now
//// provide a package that was previously unresolved.
//func (ld *loader) updateRequirements(ctx context.Context) (changed bool, err error) {
//	rs := ld.requirements
//
//	// direct contains the set of modules believed to provide packages directly
//	// imported by the main module.
//	var direct map[string]bool
//
//	// If we didn't scan all of the imports from the main module, or didn't use
//	// imports.AnyTags, then we didn't necessarily load every package that
//	// contributes “direct” imports — so we can't safely mark existing direct
//	// dependencies in ld.requirements as indirect-only. Propagate them as direct.
//	loadedDirect := ld.allPatternIsRoot && reflect.DeepEqual(ld.Tags, imports.AnyTags())
//	if loadedDirect {
//		direct = make(map[string]bool)
//	} else {
//		// TODO(bcmills): It seems like a shame to allocate and copy a map here when
//		// it will only rarely actually vary from rs.direct. Measure this cost and
//		// maybe avoid the copy.
//		direct = make(map[string]bool, len(rs.direct))
//		for mPath := range rs.direct {
//			direct[mPath] = true
//		}
//	}
//
//	var maxTooNew *gover.TooNewError
//	for _, pkg := range ld.pkgs {
//		if pkg.err != nil {
//			if tooNew := (*gover.TooNewError)(nil); errors.As(pkg.err, &tooNew) {
//				if maxTooNew == nil || gover.Compare(tooNew.GoVersion, maxTooNew.GoVersion) > 0 {
//					maxTooNew = tooNew
//				}
//			}
//		}
//		if pkg.mod.Version != "" || !MainModules.Contains(pkg.mod.Path) {
//			continue
//		}
//
//		for _, dep := range pkg.imports {
//			if !dep.fromExternalModule() {
//				continue
//			}
//
//			if inWorkspaceMode() {
//				// In workspace mode / workspace pruning mode, the roots are the main modules
//				// rather than the main module's direct dependencies. The check below on the selected
//				// roots does not apply.
//				//if cfg.BuildMod == "vendor" {
//				//	// In workspace vendor mode, we don't need to load the requirements of the workspace
//				//	// modules' dependencies so the check below doesn't work. But that's okay, because
//				//	// checking whether modules are required directly for the purposes of pruning is
//				//	// less important in vendor mode: if we were able to load the package, we have
//				//	// everything we need  to build the package, and dependencies' tests are pruned out
//				//	// of the vendor directory anyway.
//				//	continue
//				//}
//				if mg, err := rs.Graph(ctx); err != nil {
//					return false, err
//				} else if _, ok := mg.RequiredBy(dep.mod); !ok {
//					// dep.mod is not an explicit dependency, but needs to be.
//					// See comment on error returned below.
//					pkg.err = &DirectImportFromImplicitDependencyError{
//						ImporterPath: pkg.path,
//						ImportedPath: dep.path,
//						Module:       dep.mod,
//					}
//				}
//				continue
//			}
//
//			if pkg.err == nil /*&& cfg.BuildMod != "mod"*/ {
//				if v, ok := rs.rootSelected(dep.mod.Path); !ok || v != dep.mod.Version {
//					// dep.mod is not an explicit dependency, but needs to be.
//					// Because we are not in "mod" mode, we will not be able to update it.
//					// Instead, mark the importing package with an error.
//					//
//					// TODO(#41688): The resulting error message fails to include the file
//					// position of the import statement (because that information is not
//					// tracked by the module loader). Figure out how to plumb the import
//					// position through.
//					pkg.err = &DirectImportFromImplicitDependencyError{
//						ImporterPath: pkg.path,
//						ImportedPath: dep.path,
//						Module:       dep.mod,
//					}
//					// cfg.BuildMod does not allow us to change dep.mod to be a direct
//					// dependency, so don't mark it as such.
//					continue
//				}
//			}
//
//			// dep is a package directly imported by a package or test in the main
//			// module and loaded from some other module (not the standard library).
//			// Mark its module as a direct dependency.
//			direct[dep.mod.Path] = true
//		}
//	}
//	if maxTooNew != nil {
//		return false, maxTooNew
//	}
//
//	var addRoots []module.Version
//	if ld.Tidy {
//		// When we are tidying a module with a pruned dependency graph, we may need
//		// to add roots to preserve the versions of indirect, test-only dependencies
//		// that are upgraded above or otherwise missing from the go.mod files of
//		// direct dependencies. (For example, the direct dependency might be a very
//		// stable codebase that predates modules and thus lacks a go.mod file, or
//		// the author of the direct dependency may have forgotten to commit a change
//		// to the go.mod file, or may have made an erroneous hand-edit that causes
//		// it to be untidy.)
//		//
//		// Promoting an indirect dependency to a root adds the next layer of its
//		// dependencies to the module graph, which may increase the selected
//		// versions of other modules from which we have already loaded packages.
//		// So after we promote an indirect dependency to a root, we need to reload
//		// packages, which means another iteration of loading.
//		//
//		// As an extra wrinkle, the upgrades due to promoting a root can cause
//		// previously-resolved packages to become unresolved. For example, the
//		// module providing an unstable package might be upgraded to a version
//		// that no longer contains that package. If we then resolve the missing
//		// package, we might add yet another root that upgrades away some other
//		// dependency. (The tests in mod_tidy_convergence*.txt illustrate some
//		// particularly worrisome cases.)
//		//
//		// To ensure that this process of promoting, adding, and upgrading roots
//		// eventually terminates, during iteration we only ever add modules to the
//		// root set — we only remove irrelevant roots at the very end of
//		// iteration, after we have already added every root that we plan to need
//		// in the (eventual) tidy root set.
//		//
//		// Since we do not remove any roots during iteration, even if they no
//		// longer provide any imported packages, the selected versions of the
//		// roots can only increase and the set of roots can only expand. The set
//		// of extant root paths is finite and the set of versions of each path is
//		// finite, so the iteration *must* reach a stable fixed-point.
//		tidy, err := tidyRoots(ctx, rs, ld.pkgs)
//		if err != nil {
//			return false, err
//		}
//		addRoots = tidy.rootModules
//	}
//
//	rs, err = updateRoots(ctx, direct, rs, ld.pkgs, addRoots, ld.AssumeRootsImported)
//	if err != nil {
//		// We don't actually know what even the root requirements are supposed to be,
//		// so we can't proceed with loading. Return the error to the caller
//		return false, err
//	}
//
//	if rs.GoVersion() != ld.requirements.GoVersion() {
//		// A change in the selected Go version may or may not affect the set of
//		// loaded packages, but in some cases it can change the meaning of the "all"
//		// pattern, the level of pruning in the module graph, and even the set of
//		// packages present in the standard library. If it has changed, it's best to
//		// reload packages once more to be sure everything is stable.
//		changed = true
//	} else if rs != ld.requirements && !reflect.DeepEqual(rs.rootModules, ld.requirements.rootModules) {
//		// The roots of the module graph have changed in some way (not just the
//		// "direct" markings). Check whether the changes affected any of the loaded
//		// packages.
//		mg, err := rs.Graph(ctx)
//		if err != nil {
//			return false, err
//		}
//		for _, pkg := range ld.pkgs {
//			if pkg.fromExternalModule() && mg.Selected(pkg.mod.Path) != pkg.mod.Version {
//				changed = true
//				break
//			}
//			if pkg.err != nil {
//				// Promoting a module to a root may resolve an import that was
//				// previously missing (by pulling in a previously-prune dependency that
//				// provides it) or ambiguous (by promoting exactly one of the
//				// alternatives to a root and ignoring the second-level alternatives) or
//				// otherwise errored out (by upgrading from a version that cannot be
//				// fetched to one that can be).
//				//
//				// Instead of enumerating all of the possible errors, we'll just check
//				// whether importFromModules returns nil for the package.
//				// False-positives are ok: if we have a false-positive here, we'll do an
//				// extra iteration of package loading this time, but we'll still
//				// converge when the root set stops changing.
//				//
//				// In some sense, we can think of this as ‘upgraded the module providing
//				// pkg.path from "none" to a version higher than "none"’.
//				if _, _, _, _, err = importFromModules(ctx, pkg.path, rs, nil, ld.skipImportModFiles); err == nil {
//					changed = true
//					break
//				}
//			}
//		}
//	}
//
//	ld.requirements = rs
//	return changed, nil
//}

// resolveMissingImports returns a set of modules that could be added as
// dependencies in order to resolve missing packages from pkgs.
//
// The newly-resolved packages are added to the addedModuleFor map, and
// resolveMissingImports returns a map from each new module version to
// the first missing package that module would resolve.
func (ld *loader) resolveMissingImports(ctx context.Context) (modAddedBy map[module.Version]*loadPkg, err error) {
	type pkgMod struct {
		pkg *loadPkg
		mod *module.Version
	}
	var pkgMods []pkgMod
	for _, pkg := range ld.pkgs {
		if pkg.err == nil {
			continue
		}
		if pkg.isTest() {
			// If we are missing a test, we are also missing its non-test version, and
			// we should only add the missing import once.
			continue
		}
		if !errors.As(pkg.err, new(*ImportMissingError)) {
			// Leave other errors for Import or load.Packages to report.
			continue
		}

		pkg := pkg
		var mod module.Version
		ld.work.Add(func() {
			var err error
			mod, err = queryImport(ctx, pkg.path, ld.requirements)
			if err != nil {
				var ime *ImportMissingError
				if errors.As(err, &ime) {
					for curstack := pkg.stack; curstack != nil; curstack = curstack.stack {
						if MainModules.Contains(curstack.mod.Path) {
							ime.ImportingMainModule = curstack.mod
							break
						}
					}
				}
				// pkg.err was already non-nil, so we can reasonably attribute the error
				// for pkg to either the original error or the one returned by
				// queryImport. The existing error indicates only that we couldn't find
				// the package, whereas the query error also explains why we didn't fix
				// the problem — so we prefer the latter.
				pkg.err = err
			}

			// err is nil, but we intentionally leave pkg.err non-nil and pkg.mod
			// unset: we still haven't satisfied other invariants of a
			// successfully-loaded package, such as scanning and loading the imports
			// of that package. If we succeed in resolving the new dependency graph,
			// the caller can reload pkg and update the error at that point.
			//
			// Even then, the package might not be loaded from the version we've
			// identified here. The module may be upgraded by some other dependency,
			// or by a transitive dependency of mod itself, or — less likely — the
			// package may be rejected by an AllowPackage hook or rendered ambiguous
			// by some other newly-added or newly-upgraded dependency.
		})

		pkgMods = append(pkgMods, pkgMod{pkg: pkg, mod: &mod})
	}
	<-ld.work.Idle()

	modAddedBy = map[module.Version]*loadPkg{}

	var (
		maxTooNew    *gover.TooNewError
		maxTooNewPkg *loadPkg
	)
	for _, pm := range pkgMods {
		if tooNew := (*gover.TooNewError)(nil); errors.As(pm.pkg.err, &tooNew) {
			if maxTooNew == nil || gover.Compare(tooNew.GoVersion, maxTooNew.GoVersion) > 0 {
				maxTooNew = tooNew
				maxTooNewPkg = pm.pkg
			}
		}
	}
	if maxTooNew != nil {
		fmt.Fprintf(os.Stderr, "go: toolchain upgrade needed to resolve %s\n", maxTooNewPkg.path)
		return nil, maxTooNew
	}

	for _, pm := range pkgMods {
		pkg, mod := pm.pkg, *pm.mod
		if mod.Path == "" {
			continue
		}

		fmt.Fprintf(os.Stderr, "go: found %s in %s %s\n", pkg.path, mod.Path, mod.Version)
		if modAddedBy[mod] == nil {
			modAddedBy[mod] = pkg
		}
	}

	return modAddedBy, nil
}

// pkg locates the *loadPkg for path, creating and queuing it for loading if
// needed, and updates its state to reflect the given flags.
//
// The imports of the returned *loadPkg will be loaded asynchronously in the
// ld.work queue, and its test (if requested) will also be populated once
// imports have been resolved. When ld.work goes idle, all transitive imports of
// the requested package (and its test, if requested) will have been loaded.
func (ld *loader) pkg(ctx context.Context, path string, flags loadPkgFlags) *loadPkg {
	if flags.has(pkgImportsLoaded) {
		panic("internal error: (*loader).pkg called with pkgImportsLoaded flag set")
	}

	pkg := ld.pkgCache.Do(path, func() *loadPkg {
		pkg := &loadPkg{
			path: path,
		}
		ld.applyPkgFlags(ctx, pkg, flags)

		ld.work.Add(func() { ld.load(ctx, pkg) })
		return pkg
	})

	ld.applyPkgFlags(ctx, pkg, flags)
	return pkg
}

// applyPkgFlags updates pkg.flags to set the given flags and propagate the
// (transitive) effects of those flags, possibly loading or enqueueing further
// packages as a result.
func (ld *loader) applyPkgFlags(ctx context.Context, pkg *loadPkg, flags loadPkgFlags) {
	if flags == 0 {
		return
	}

	if flags.has(pkgInAll) && ld.allPatternIsRoot && !pkg.isTest() {
		// This package matches a root pattern by virtue of being in "all".
		flags |= pkgIsRoot
	}
	if flags.has(pkgIsRoot) {
		flags |= pkgFromRoot
	}

	old := pkg.flags.update(flags)
	new := old | flags
	if new == old || !new.has(pkgImportsLoaded) {
		// We either didn't change the state of pkg, or we don't know anything about
		// its dependencies yet. Either way, we can't usefully load its test or
		// update its dependencies.
		return
	}

	if !pkg.isTest() {
		// Check whether we should add (or update the flags for) a test for pkg.
		// ld.pkgTest is idempotent and extra invocations are inexpensive,
		// so it's ok if we call it more than is strictly necessary.
		wantTest := false
		switch {
		case ld.allPatternIsRoot && MainModules.Contains(pkg.mod.Path):
			// We are loading the "all" pattern, which includes packages imported by
			// tests in the main module. This package is in the main module, so we
			// need to identify the imports of its test even if LoadTests is not set.
			//
			// (We will filter out the extra tests explicitly in computePatternAll.)
			wantTest = true

		case ld.allPatternIsRoot && ld.allClosesOverTests && new.has(pkgInAll):
			// This variant of the "all" pattern includes imports of tests of every
			// package that is itself in "all", and pkg is in "all", so its test is
			// also in "all" (as above).
			wantTest = true

		case ld.LoadTests && new.has(pkgIsRoot):
			// LoadTest explicitly requests tests of “the root packages”.
			wantTest = true
		}

		if wantTest {
			var testFlags loadPkgFlags
			if MainModules.Contains(pkg.mod.Path) || (ld.allClosesOverTests && new.has(pkgInAll)) {
				// Tests of packages in the main module are in "all", in the sense that
				// they cause the packages they import to also be in "all". So are tests
				// of packages in "all" if "all" closes over test dependencies.
				testFlags |= pkgInAll
			}
			ld.pkgTest(ctx, pkg, testFlags)
		}
	}

	if new.has(pkgInAll) && !old.has(pkgInAll|pkgImportsLoaded) {
		// We have just marked pkg with pkgInAll, or we have just loaded its
		// imports, or both. Now is the time to propagate pkgInAll to the imports.
		for _, dep := range pkg.imports {
			ld.applyPkgFlags(ctx, dep, pkgInAll)
		}
	}

	if new.has(pkgFromRoot) && !old.has(pkgFromRoot|pkgImportsLoaded) {
		for _, dep := range pkg.imports {
			ld.applyPkgFlags(ctx, dep, pkgFromRoot)
		}
	}
}

// preloadRootModules loads the module requirements needed to identify the
// selected version of each module providing a package in rootPkgs,
// adding new root modules to the module graph if needed.
func (ld *loader) preloadRootModules(ctx context.Context, rootPkgs []string) (changedBuildList bool) {
	needc := make(chan map[module.Version]bool, 1)
	needc <- map[module.Version]bool{}
	for _, path := range rootPkgs {
		path := path
		ld.work.Add(func() {
			// First, try to identify the module containing the package using only roots.
			//
			// If the main module is tidy and the package is in "all" — or if we're
			// lucky — we can identify all of its imports without actually loading the
			// full module graph.
			m, _, _, _, err := importFromModules(ctx, path, ld.requirements, nil, ld.skipImportModFiles)
			if err != nil {
				var missing *ImportMissingError
				if errors.As(err, &missing) && ld.ResolveMissingImports {
					// This package isn't provided by any selected module.
					// If we can find it, it will be a new root dependency.
					m, err = queryImport(ctx, path, ld.requirements)
				}
				if err != nil {
					// We couldn't identify the root module containing this package.
					// Leave it unresolved; we will report it during loading.
					return
				}
			}
			if m.Path == "" {
				// The package is in std or cmd. We don't need to change the root set.
				return
			}

			v, ok := ld.requirements.rootSelected(m.Path)
			if !ok || v != m.Version {
				// We found the requested package in m, but m is not a root, so
				// loadModGraph will not load its requirements. We need to promote the
				// module to a root to ensure that any other packages this package
				// imports are resolved from correct dependency versions.
				//
				// (This is the “argument invariant” from
				// https://golang.org/design/36460-lazy-module-loading.)
				need := <-needc
				need[m] = true
				needc <- need
			}
		})
	}
	<-ld.work.Idle()

	need := <-needc
	if len(need) == 0 {
		return false // No roots to add.
	}

	toAdd := make([]module.Version, 0, len(need))
	for m := range need {
		toAdd = append(toAdd, m)
	}
	gover.ModSort(toAdd)

	rs, err := updateRoots(ctx, ld.requirements.direct, ld.requirements, nil, toAdd, ld.AssumeRootsImported)
	if err != nil {
		// We are missing some root dependency, and for some reason we can't load
		// enough of the module dependency graph to add the missing root. Package
		// loading is doomed to fail, so fail quickly.
		ld.error(err)
		ld.exitIfErrors(ctx)
		return false
	}
	if reflect.DeepEqual(rs.rootModules, ld.requirements.rootModules) {
		// Something is deeply wrong. resolveMissingImports gave us a non-empty
		// set of modules to add to the graph, but adding those modules had no
		// effect — either they were already in the graph, or updateRoots did not
		// add them as requested.
		panic(fmt.Sprintf("internal error: adding %v to module graph had no effect on root requirements (%v)", toAdd, rs.rootModules))
	}

	ld.requirements = rs
	return true
}

// load loads an individual package.
func (ld *loader) load(ctx context.Context, pkg *loadPkg) {
	var mg *ModuleGraph
	if ld.requirements.pruning == unpruned {
		var err error
		mg, err = ld.requirements.Graph(ctx)
		if err != nil {
			// We already checked the error from Graph in loadFromRoots and/or
			// updateRequirements, so we ignored the error on purpose and we should
			// keep trying to push past it.
			//
			// However, because mg may be incomplete (and thus may select inaccurate
			// versions), we shouldn't use it to load packages. Instead, we pass a nil
			// *ModuleGraph, which will cause mg to first try loading from only the
			// main module and root dependencies.
			mg = nil
		}
	}

	var modroot string
	pkg.mod, modroot, pkg.dir, pkg.altMods, pkg.err = importFromModules(ctx, pkg.path, ld.requirements, mg, ld.skipImportModFiles)
	if pkg.dir == "" {
		return
	}
	if MainModules.Contains(pkg.mod.Path) {
		// Go ahead and mark pkg as in "all". This provides the invariant that a
		// package that is *only* imported by other packages in "all" is always
		// marked as such before loading its imports.
		//
		// We don't actually rely on that invariant at the moment, but it may
		// improve efficiency somewhat and makes the behavior a bit easier to reason
		// about (by reducing churn on the flag bits of dependencies), and costs
		// essentially nothing (these atomic flag ops are essentially free compared
		// to scanning source code for imports).
		ld.applyPkgFlags(ctx, pkg, pkgInAll)
	}
	if ld.AllowPackage != nil {
		if err := ld.AllowPackage(ctx, pkg.path, pkg.mod); err != nil {
			pkg.err = err
		}
	}

	//pkg.inStd = (search.IsStandardImportPath(pkg.path) && search.InDir(pkg.dir, cfg.GOROOTsrc) != "")

	var imports, testImports []string

	var err error
	imports, testImports, err = scanDir(modroot, pkg.dir, ld.Tags)
	if err != nil {
		pkg.err = err
		return
	}
	//}

	pkg.imports = make([]*loadPkg, 0, len(imports))
	var importFlags loadPkgFlags
	if pkg.flags.has(pkgInAll) {
		importFlags = pkgInAll
	}
	for _, path := range imports {
		if pkg.inStd {
			// Imports from packages in "std" and "cmd" should resolve using
			// GOROOT/src/vendor even when "std" is not the main module.
			path = ld.stdVendor(pkg.path, path)
		}
		pkg.imports = append(pkg.imports, ld.pkg(ctx, path, importFlags))
	}
	pkg.testImports = testImports

	ld.applyPkgFlags(ctx, pkg, pkgImportsLoaded)
}

// pkgTest locates the test of pkg, creating it if needed, and updates its state
// to reflect the given flags.
//
// pkgTest requires that the imports of pkg have already been loaded (flagged
// with pkgImportsLoaded).
func (ld *loader) pkgTest(ctx context.Context, pkg *loadPkg, testFlags loadPkgFlags) *loadPkg {
	if pkg.isTest() {
		panic("pkgTest called on a test package")
	}

	createdTest := false
	pkg.testOnce.Do(func() {
		pkg.test = &loadPkg{
			path:   pkg.path,
			testOf: pkg,
			mod:    pkg.mod,
			dir:    pkg.dir,
			err:    pkg.err,
			inStd:  pkg.inStd,
		}
		ld.applyPkgFlags(ctx, pkg.test, testFlags)
		createdTest = true
	})

	test := pkg.test
	if createdTest {
		test.imports = make([]*loadPkg, 0, len(pkg.testImports))
		var importFlags loadPkgFlags
		if test.flags.has(pkgInAll) {
			importFlags = pkgInAll
		}
		for _, path := range pkg.testImports {
			if pkg.inStd {
				path = ld.stdVendor(test.path, path)
			}
			test.imports = append(test.imports, ld.pkg(ctx, path, importFlags))
		}
		pkg.testImports = nil
		ld.applyPkgFlags(ctx, test, pkgImportsLoaded)
	} else {
		ld.applyPkgFlags(ctx, test, testFlags)
	}

	return test
}

// stdVendor returns the canonical import path for the package with the given
// path when imported from the standard-library package at parentPath.
func (ld *loader) stdVendor(parentPath, path string) string {
	if search.IsStandardImportPath(path) {
		return path
	}

	if util.HasPathPrefix(parentPath, "cmd") {
		if !ld.VendorModulesInGOROOTSrc || !MainModules.Contains("cmd") {
			//vendorPath := filepath.Join("cmd", "vendor", path)

			//if _, err := os.Stat(filepath.Join(cfg.GOROOTsrc, filepath.FromSlash(vendorPath))); err == nil {
			//	return vendorPath
			//}
		}
	} else if !ld.VendorModulesInGOROOTSrc || !MainModules.Contains("std") || util.HasPathPrefix(parentPath, "vendor") {
	}

	// Not vendored: resolve from modules.
	return path
}

// computePatternAll returns the list of packages matching pattern "all",
// starting with a list of the import paths for the packages in the main module.
func (ld *loader) computePatternAll() (all []string) {
	for _, pkg := range ld.pkgs {
		if pkg.flags.has(pkgInAll) && !pkg.isTest() {
			all = append(all, pkg.path)
		}
	}
	sort.Strings(all)
	return all
}

// checkMultiplePaths verifies that a given module path is used as itself
// or as a replacement for another module, but not both at the same time.
//
// (See https://golang.org/issue/26607 and https://golang.org/issue/34650.)
func (ld *loader) checkMultiplePaths() {
	mods := ld.requirements.rootModules
	if cached := ld.requirements.graph.Load(); cached != nil {
		if mg := cached.mg; mg != nil {
			mods = mg.BuildList()
		}
	}

	firstPath := map[module.Version]string{}
	for _, mod := range mods {
		src := resolveReplacement(mod)
		if prev, ok := firstPath[src]; !ok {
			firstPath[src] = mod.Path
		} else if prev != mod.Path {
			ld.error(fmt.Errorf("%s@%s used for two different module paths (%s and %s)", src.Path, src.Version, prev, mod.Path))
		}
	}
}

// checkTidyCompatibility emits an error if any package would be loaded from a
// different module under rs than under ld.requirements.
func (ld *loader) checkTidyCompatibility(ctx context.Context, rs *Requirements, compatVersion string) {
	goVersion := rs.GoVersion()
	suggestUpgrade := false
	suggestEFlag := false
	suggestFixes := func() {
		if ld.AllowErrors {
			// The user is explicitly ignoring these errors, so don't bother them with
			// other options.
			return
		}

		// We print directly to os.Stderr because this information is advice about
		// how to fix errors, not actually an error itself.
		// (The actual errors should have been logged already.)

		fmt.Fprintln(os.Stderr)

		goFlag := ""
		if goVersion != MainModules.GoVersion() {
			goFlag = " -go=" + goVersion
		}

		compatFlag := ""
		if compatVersion != gover.Prev(goVersion) {
			compatFlag = " -compat=" + compatVersion
		}
		if suggestUpgrade {
			eDesc := ""
			eFlag := ""
			if suggestEFlag {
				eDesc = ", leaving some packages unresolved"
				eFlag = " -e"
			}
			fmt.Fprintf(os.Stderr, "To upgrade to the versions selected by go %s%s:\n\tgo mod tidy%s -go=%s && go mod tidy%s -go=%s%s\n", compatVersion, eDesc, eFlag, compatVersion, eFlag, goVersion, compatFlag)
		} else if suggestEFlag {
			// If some packages are missing but no package is upgraded, then we
			// shouldn't suggest upgrading to the Go 1.16 versions explicitly — that
			// wouldn't actually fix anything for Go 1.16 users, and *would* break
			// something for Go 1.17 users.
			fmt.Fprintf(os.Stderr, "To proceed despite packages unresolved in go %s:\n\tgo mod tidy -e%s%s\n", compatVersion, goFlag, compatFlag)
		}

		fmt.Fprintf(os.Stderr, "If reproducibility with go %s is not needed:\n\tgo mod tidy%s -compat=%s\n", compatVersion, goFlag, goVersion)

		// TODO(#46141): Populate the linked wiki page.
		fmt.Fprintf(os.Stderr, "For other options, see:\n\thttps://golang.org/doc/modules/pruning\n")
	}

	mg, err := rs.Graph(ctx)
	if err != nil {
		ld.error(fmt.Errorf("error loading go %s module graph: %w", compatVersion, err))
		ld.switchIfErrors(ctx)
		suggestFixes()
		ld.exitIfErrors(ctx)
		return
	}

	// Re-resolve packages in parallel.
	//
	// We re-resolve each package — rather than just checking versions — to ensure
	// that we have fetched module source code (and, importantly, checksums for
	// that source code) for all modules that are necessary to ensure that imports
	// are unambiguous. That also produces clearer diagnostics, since we can say
	// exactly what happened to the package if it became ambiguous or disappeared
	// entirely.
	//
	// We re-resolve the packages in parallel because this process involves disk
	// I/O to check for package sources, and because the process of checking for
	// ambiguous imports may require us to download additional modules that are
	// otherwise pruned out in Go 1.17 — we don't want to block progress on other
	// packages while we wait for a single new download.
	type mismatch struct {
		mod module.Version
		err error
	}
	mismatchMu := make(chan map[*loadPkg]mismatch, 1)
	mismatchMu <- map[*loadPkg]mismatch{}
	for _, pkg := range ld.pkgs {
		if pkg.mod.Path == "" && pkg.err == nil {
			// This package is from the standard library (which does not vary based on
			// the module graph).
			continue
		}

		pkg := pkg
		ld.work.Add(func() {
			mod, _, _, _, err := importFromModules(ctx, pkg.path, rs, mg, ld.skipImportModFiles)
			if mod != pkg.mod {
				mismatches := <-mismatchMu
				mismatches[pkg] = mismatch{mod: mod, err: err}
				mismatchMu <- mismatches
			}
		})
	}
	<-ld.work.Idle()

	mismatches := <-mismatchMu
	if len(mismatches) == 0 {
		// Since we're running as part of 'go mod tidy', the roots of the module
		// graph should contain only modules that are relevant to some package in
		// the package graph. We checked every package in the package graph and
		// didn't find any mismatches, so that must mean that all of the roots of
		// the module graph are also consistent.
		//
		// If we're wrong, Go 1.16 in -mod=readonly mode will error out with
		// "updates to go.mod needed", which would be very confusing. So instead,
		// we'll double-check that our reasoning above actually holds — if it
		// doesn't, we'll emit an internal error and hopefully the user will report
		// it as a bug.
		for _, m := range ld.requirements.rootModules {
			if v := mg.Selected(m.Path); v != m.Version {
				fmt.Fprintln(os.Stderr)
				log.Fatalf("go: internal error: failed to diagnose selected-version mismatch for module %s: go %s selects %s, but go %s selects %s\n\tPlease report this at https://golang.org/issue.", m.Path, goVersion, m.Version, compatVersion, v)
			}
		}
		return
	}

	// Iterate over the packages (instead of the mismatches map) to emit errors in
	// deterministic order.
	for _, pkg := range ld.pkgs {
		mismatch, ok := mismatches[pkg]
		if !ok {
			continue
		}

		if pkg.isTest() {
			// We already did (or will) report an error for the package itself,
			// so don't report a duplicate (and more verbose) error for its test.
			if _, ok := mismatches[pkg.testOf]; !ok {
				log.Fatalf("go: internal error: mismatch recorded for test %s, but not its non-test package", pkg.path)
			}
			continue
		}

		switch {
		case mismatch.err != nil:
			// pkg resolved successfully, but errors out using the requirements in rs.
			//
			// This could occur because the import is provided by a single root (and
			// is thus unambiguous in a main module with a pruned module graph) and
			// also one or more transitive dependencies (and is ambiguous with an
			// unpruned graph).
			//
			// It could also occur because some transitive dependency upgrades the
			// module that previously provided the package to a version that no
			// longer does, or to a version for which the module source code (but
			// not the go.mod file in isolation) has a checksum error.
			if missing := (*ImportMissingError)(nil); errors.As(mismatch.err, &missing) {
				selected := module.Version{
					Path:    pkg.mod.Path,
					Version: mg.Selected(pkg.mod.Path),
				}
				ld.error(fmt.Errorf("%s loaded from %v,\n\tbut go %s would fail to locate it in %s", pkg.stackText(), pkg.mod, compatVersion, selected))
			} else {
				if ambiguous := (*AmbiguousImportError)(nil); errors.As(mismatch.err, &ambiguous) {
					// TODO: Is this check needed?
				}
				ld.error(fmt.Errorf("%s loaded from %v,\n\tbut go %s would fail to locate it:\n\t%v", pkg.stackText(), pkg.mod, compatVersion, mismatch.err))
			}

			suggestEFlag = true

			// Even if we press ahead with the '-e' flag, the older version will
			// error out in readonly mode if it thinks the go.mod file contains
			// any *explicit* dependency that is not at its selected version,
			// even if that dependency is not relevant to any package being loaded.
			//
			// We check for that condition here. If all of the roots are consistent
			// the '-e' flag suffices, but otherwise we need to suggest an upgrade.
			if !suggestUpgrade {
				for _, m := range ld.requirements.rootModules {
					if v := mg.Selected(m.Path); v != m.Version {
						suggestUpgrade = true
						break
					}
				}
			}

		case pkg.err != nil:
			// pkg had an error in with a pruned module graph (presumably suppressed
			// with the -e flag), but the error went away using an unpruned graph.
			//
			// This is possible, if, say, the import is unresolved in the pruned graph
			// (because the "latest" version of each candidate module either is
			// unavailable or does not contain the package), but is resolved in the
			// unpruned graph due to a newer-than-latest dependency that is normally
			// pruned out.
			//
			// This could also occur if the source code for the module providing the
			// package in the pruned graph has a checksum error, but the unpruned
			// graph upgrades that module to a version with a correct checksum.
			//
			// pkg.err should have already been logged elsewhere — along with a
			// stack trace — so log only the import path and non-error info here.
			suggestUpgrade = true
			ld.error(fmt.Errorf("%s failed to load from any module,\n\tbut go %s would load it from %v", pkg.path, compatVersion, mismatch.mod))

		case pkg.mod != mismatch.mod:
			// The package is loaded successfully by both Go versions, but from a
			// different module in each. This could lead to subtle (and perhaps even
			// unnoticed!) variations in behavior between builds with different
			// toolchains.
			suggestUpgrade = true
			ld.error(fmt.Errorf("%s loaded from %v,\n\tbut go %s would select %v\n", pkg.stackText(), pkg.mod, compatVersion, mismatch.mod.Version))

		default:
			log.Fatalf("go: internal error: mismatch recorded for package %s, but no differences found", pkg.path)
		}
	}

	ld.switchIfErrors(ctx)
	suggestFixes()
	ld.exitIfErrors(ctx)
}

// scanDir is like imports.ScanDir but elides known magic imports from the list,
// so that we do not go looking for packages that don't really exist.
//
// The standard magic import is "C", for cgo.
//
// The only other known magic imports are appengine and appengine/*.
// These are so old that they predate "go get" and did not use URL-like paths.
// Most code today now uses google.golang.org/appengine instead,
// but not all code has been so updated. When we mostly ignore build tags
// during "go vendor", we look into "// +build appengine" files and
// may see these legacy imports. We drop them so that the module
// search does not look for modules to try to satisfy them.
func scanDir(modroot string, dir string, tags map[string]bool) (imports_, testImports []string, err error) {
	if ip, mierr := modindex.GetPackage(modroot, dir); mierr == nil {
		imports_, testImports, err = ip.ScanDir(tags)
		goto Happy
	} else if !errors.Is(mierr, modindex.ErrNotIndexed) {
		return nil, nil, mierr
	}

	imports_, testImports, err = imports.ScanDir(dir, tags)
Happy:

	filter := func(x []string) []string {
		w := 0
		for _, pkg := range x {
			if pkg != "C" && pkg != "appengine" && !strings.HasPrefix(pkg, "appengine/") &&
				pkg != "appengine_internal" && !strings.HasPrefix(pkg, "appengine_internal/") {
				x[w] = pkg
				w++
			}
		}
		return x[:w]
	}

	return filter(imports_), filter(testImports), err
}

// buildStacks computes minimal import stacks for each package,
// for use in error messages. When it completes, packages that
// are part of the original root set have pkg.stack == nil,
// and other packages have pkg.stack pointing at the next
// package up the import stack in their minimal chain.
// As a side effect, buildStacks also constructs ld.pkgs,
// the list of all packages loaded.
func (ld *loader) buildStacks() {
	if len(ld.pkgs) > 0 {
		panic("buildStacks")
	}
	for _, pkg := range ld.roots {
		pkg.stack = pkg // sentinel to avoid processing in next loop
		ld.pkgs = append(ld.pkgs, pkg)
	}
	for i := 0; i < len(ld.pkgs); i++ { // not range: appending to ld.pkgs in loop
		pkg := ld.pkgs[i]
		for _, next := range pkg.imports {
			if next.stack == nil {
				next.stack = pkg
				ld.pkgs = append(ld.pkgs, next)
			}
		}
		if next := pkg.test; next != nil && next.stack == nil {
			next.stack = pkg
			ld.pkgs = append(ld.pkgs, next)
		}
	}
	for _, pkg := range ld.roots {
		pkg.stack = nil
	}
}

// stackText builds the import stack text to use when
// reporting an error in pkg. It has the general form
//
//	root imports
//		other imports
//		other2 tested by
//		other2.test imports
//		pkg
func (pkg *loadPkg) stackText() string {
	var stack []*loadPkg
	for p := pkg; p != nil; p = p.stack {
		stack = append(stack, p)
	}

	var buf strings.Builder
	for i := len(stack) - 1; i >= 0; i-- {
		p := stack[i]
		fmt.Fprint(&buf, p.path)
		if p.testOf != nil {
			fmt.Fprint(&buf, ".test")
		}
		if i > 0 {
			if stack[i-1].testOf == p {
				fmt.Fprint(&buf, " tested by\n\t")
			} else {
				fmt.Fprint(&buf, " imports\n\t")
			}
		}
	}
	return buf.String()
}

// why returns the text to use in "go mod why" output about the given package.
// It is less ornate than the stackText but contains the same information.
func (pkg *loadPkg) why() string {
	var buf strings.Builder
	var stack []*loadPkg
	for p := pkg; p != nil; p = p.stack {
		stack = append(stack, p)
	}

	for i := len(stack) - 1; i >= 0; i-- {
		p := stack[i]
		if p.testOf != nil {
			fmt.Fprintf(&buf, "%s.test\n", p.testOf.path)
		} else {
			fmt.Fprintf(&buf, "%s\n", p.path)
		}
	}
	return buf.String()
}
