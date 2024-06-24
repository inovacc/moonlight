// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modload

import (
	"context"
	"errors"
	"fmt"
	base2 "github.com/inovacc/moonlight/pkg/module/internal/base"
	"github.com/inovacc/moonlight/pkg/module/internal/cfg"
	gover2 "github.com/inovacc/moonlight/pkg/module/internal/gover"
	"github.com/inovacc/moonlight/pkg/module/internal/modfetch"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// Variables set by other packages.
//
// TODO(#40775): See if these can be plumbed as explicit parameters.
var (
	// RootMode determines whether a module root is needed.
	RootMode Root

	// ForceUseModules may be set to force modules to be enabled when
	// GO111MODULE=auto or to report an error when GO111MODULE=off.
	ForceUseModules bool

	allowMissingModuleImports bool
)

// Variables set in Init.
var (
	initialized bool

	// These are primarily used to initialize the MainModules, and should be
	// eventually superseded by them but are still used in cases where the module
	// roots are required but MainModules hasn't been initialized yet. Set to
	// the modRoots of the main modules.
	// modRoots != nil implies len(modRoots) > 0
	modRoots []string
	//gopath   string
)

// Variable set in InitWorkfile
var (
	// Set to the path to the go.work file, or "" if workspace mode is disabled.
	workFilePath string
)

type MainModuleSet struct {
	// versions are the module.Version values of each of the main modules.
	// For each of them, the Path fields are ordinary module paths and the Version
	// fields are empty strings.
	// versions is clipped (len=cap).
	versions []module.Version

	// modRoot maps each module in versions to its absolute filesystem path.
	modRoot map[module.Version]string

	// pathPrefix is the path prefix for packages in the module, without a trailing
	// slash. For most modules, pathPrefix is just version.Path, but the
	// standard-library module "std" has an empty prefix.
	pathPrefix map[module.Version]string

	// inGorootSrc caches whether modRoot is within GOROOT/src.
	// The "std" module is special within GOROOT/src, but not otherwise.
	inGorootSrc map[module.Version]bool

	modFiles map[module.Version]*modfile.File

	modContainingCWD module.Version

	workFile *modfile.WorkFile

	workFileReplaceMap map[module.Version]module.Version
	// highest replaced version of each module path; empty string for wildcard-only replacements
	highestReplaced map[string]string

	indexMu sync.Mutex
	indices map[module.Version]*modFileIndex
}

func (mms *MainModuleSet) PathPrefix(m module.Version) string {
	return mms.pathPrefix[m]
}

// Versions returns the module.Version values of each of the main modules.
// For each of them, the Path fields are ordinary module paths and the Version
// fields are empty strings.
// Callers should not modify the returned slice.
func (mms *MainModuleSet) Versions() []module.Version {
	if mms == nil {
		return nil
	}
	return mms.versions
}

func (mms *MainModuleSet) Contains(path string) bool {
	if mms == nil {
		return false
	}
	for _, v := range mms.versions {
		if v.Path == path {
			return true
		}
	}
	return false
}

func (mms *MainModuleSet) ModRoot(m module.Version) string {
	if mms == nil {
		return ""
	}
	return mms.modRoot[m]
}

func (mms *MainModuleSet) InGorootSrc(m module.Version) bool {
	if mms == nil {
		return false
	}
	return mms.inGorootSrc[m]
}

func (mms *MainModuleSet) mustGetSingleMainModule() module.Version {
	if mms == nil || len(mms.versions) == 0 {
		panic("internal error: mustGetSingleMainModule called in context with no main modules")
	}
	if len(mms.versions) != 1 {
		if inWorkspaceMode() {
			panic("internal error: mustGetSingleMainModule called in workspace mode")
		} else {
			panic("internal error: multiple main modules present outside of workspace mode")
		}
	}
	return mms.versions[0]
}

func (mms *MainModuleSet) GetSingleIndexOrNil() *modFileIndex {
	if mms == nil {
		return nil
	}
	if len(mms.versions) == 0 {
		return nil
	}
	return mms.indices[mms.mustGetSingleMainModule()]
}

func (mms *MainModuleSet) Index(m module.Version) *modFileIndex {
	mms.indexMu.Lock()
	defer mms.indexMu.Unlock()
	return mms.indices[m]
}

func (mms *MainModuleSet) SetIndex(m module.Version, index *modFileIndex) {
	mms.indexMu.Lock()
	defer mms.indexMu.Unlock()
	mms.indices[m] = index
}

func (mms *MainModuleSet) ModFile(m module.Version) *modfile.File {
	return mms.modFiles[m]
}

func (mms *MainModuleSet) WorkFile() *modfile.WorkFile {
	return mms.workFile
}

func (mms *MainModuleSet) Len() int {
	if mms == nil {
		return 0
	}
	return len(mms.versions)
}

// ModContainingCWD returns the main module containing the working directory,
// or module.Version{} if none of the main modules contain the working
// directory.
func (mms *MainModuleSet) ModContainingCWD() module.Version {
	return mms.modContainingCWD
}

func (mms *MainModuleSet) HighestReplaced() map[string]string {
	return mms.highestReplaced
}

// GoVersion returns the go version set on the single module, in module mode,
// or the go.work file in workspace mode.
func (mms *MainModuleSet) GoVersion() string {
	if inWorkspaceMode() {
		return gover2.FromGoWork(mms.workFile)
	}
	if mms != nil && len(mms.versions) == 1 {
		f := mms.ModFile(mms.mustGetSingleMainModule())
		if f == nil {
			// Special case: we are outside a module, like 'go run x.go'.
			// Assume the local Go version.
			// TODO(#49228): Clean this up; see loadModFile.
			return gover2.Local()
		}
		return gover2.FromGoMod(f)
	}
	return gover2.DefaultGoModVersion
}

// Toolchain returns the toolchain set on the single module, in module mode,
// or the go.work file in workspace mode.
func (mms *MainModuleSet) Toolchain() string {
	if inWorkspaceMode() {
		if mms.workFile != nil && mms.workFile.Toolchain != nil {
			return mms.workFile.Toolchain.Name
		}
		return "go" + mms.GoVersion()
	}
	if mms != nil && len(mms.versions) == 1 {
		f := mms.ModFile(mms.mustGetSingleMainModule())
		if f == nil {
			// Special case: we are outside a module, like 'go run x.go'.
			// Assume the local Go version.
			// TODO(#49228): Clean this up; see loadModFile.
			return gover2.LocalToolchain()
		}
		if f.Toolchain != nil {
			return f.Toolchain.Name
		}
	}
	return "go" + mms.GoVersion()
}

func (mms *MainModuleSet) WorkFileReplaceMap() map[module.Version]module.Version {
	return mms.workFileReplaceMap
}

var MainModules *MainModuleSet

type Root int

const (
	// AutoRoot is the default for most commands. modload.Init will look for
	// a go.mod file in the current directory or any parent. If none is found,
	// modules may be disabled (GO111MODULE=auto) or commands may run in a
	// limited module mode.
	AutoRoot Root = iota

	// NoRoot is used for commands that run in module mode and ignore any go.mod
	// file the current directory or in parent directories.
	NoRoot

	// NeedRoot is used for commands that must run in module mode and don't
	// make sense without a main module.
	NeedRoot
)

// WorkFilePath returns the absolute path of the go.work file, or "" if not in
// workspace mode. WorkFilePath must be called after InitWorkfile.
func WorkFilePath() string {
	return workFilePath
}

// Init determines whether module mode is enabled, locates the root of the
// current module (if any), sets environment variables for Git subprocesses, and
// configures the cfg, codehost, load, modfetch, and search packages for use
// with modules.
func Init() {
	if initialized {
		return
	}
	initialized = true

	// Keep in sync with WillBeEnabled. We perform extra validation here, and
	// there are lots of diagnostics and side effects, so we can't use
	// WillBeEnabled directly.
	var mustUseModules bool
	env := cfg.Getenv("GO111MODULE")
	switch env {
	default:
		log.Fatalf("go: unknown environment setting GO111MODULE=%s", env)
	case "auto":
		mustUseModules = ForceUseModules
	case "on", "":
		mustUseModules = true
	case "off":
		if ForceUseModules {
			log.Fatalf("go: modules disabled by GO111MODULE=off; see 'go help modules'")
		}
		mustUseModules = false
		return
	}

	if err := base2.Init(base2.Cwd()); err != nil {
		log.Fatal(err)
	}

	// Disable any prompting for passwords by Git.
	// Only has an effect for 2.3.0 or later, but avoiding
	// the prompt in earlier versions is just too hard.
	// If user has explicitly set GIT_TERMINAL_PROMPT=1, keep
	// prompting.
	// See golang.org/issue/9341 and golang.org/issue/12706.
	if os.Getenv("GIT_TERMINAL_PROMPT") == "" {
		os.Setenv("GIT_TERMINAL_PROMPT", "0")
	}

	// Disable any ssh connection pooling by Git.
	// If a Git subprocess forks a child into the background to cache a new connection,
	// that child keeps stdout/stderr open. After the Git subprocess exits,
	// os/exec expects to be able to read from the stdout/stderr pipe
	// until EOF to get all the data that the Git subprocess wrote before exiting.
	// The EOF doesn't come until the child exits too, because the child
	// is holding the write end of the pipe.
	// This is unfortunate, but it has come up at least twice
	// (see golang.org/issue/13453 and golang.org/issue/16104)
	// and confuses users when it does.
	// If the user has explicitly set GIT_SSH or GIT_SSH_COMMAND,
	// assume they know what they are doing and don't step on it.
	// But default to turning off ControlMaster.
	if os.Getenv("GIT_SSH") == "" && os.Getenv("GIT_SSH_COMMAND") == "" {
		os.Setenv("GIT_SSH_COMMAND", "ssh -o ControlMaster=no -o BatchMode=yes")
	}

	if os.Getenv("GCM_INTERACTIVE") == "" {
		os.Setenv("GCM_INTERACTIVE", "never")
	}
	if modRoots != nil {
		// modRoot set before Init was called ("go mod init" does this).
		// No need to search for go.mod.
	} else if RootMode == NoRoot {
		//if cfg.ModFile != "" && !base.InGOFLAGS("-modfile") {
		//	log.Fatalf("go: -modfile cannot be used with commands that ignore the current module")
		//}
		modRoots = nil
	} else if workFilePath != "" {
		// We're in workspace mode, which implies module mode.
		//if cfg.ModFile != "" {
		//	log.Fatalf("go: -modfile cannot be used in workspace mode")
		//}
	} else {
		if modRoot := findModuleRoot(base2.Cwd()); modRoot == "" {
			//if cfg.ModFile != "" {
			//	log.Fatalf("go: cannot find main module, but -modfile was set.\n\t-modfile cannot be used to set the module root directory.")
			//}
			if RootMode == NeedRoot {
				log.Fatal(ErrNoModRoot)
			}
			if !mustUseModules {
				// GO111MODULE is 'auto', and we can't find a module root.
				// Stay in GOPATH mode.
				return
			}
			//} else if search.InDir(modRoot, os.TempDir()) == "." {
			//	// If you create /tmp/go.mod for experimenting,
			//	// then any tests that create work directories under /tmp
			//	// will find it and get modules when they're not expecting them.
			//	// It's a bit of a peculiar thing to disallow but quite mysterious
			//	// when it happens. See golang.org/issue/26708.
			//	fmt.Fprintf(os.Stderr, "go: warning: ignoring go.mod in system temp root %v\n", os.TempDir())
			//	if RootMode == NeedRoot {
			//		log.Fatal(ErrNoModRoot)
			//	}
			//	if !mustUseModules {
			//		return
			//	}
		} else {
			modRoots = []string{modRoot}
		}
	}
	//if cfg.ModFile != "" && !strings.HasSuffix(cfg.ModFile, ".mod") {
	//	log.Fatalf("go: -modfile=%s: file does not have .mod extension", cfg.ModFile)
	//}

	// We're in module mode. Set any global variables that need to be set.
	//cfg.ModulesEnabled = true
	//setDefaultBuildMod()
	//list := filepath.SplitList(cfg.BuildContext.GOPATH)
	//if len(list) > 0 && list[0] != "" {
	//	gopath = list[0]
	//	if _, err := fsys.Stat(filepath.Join(gopath, "go.mod")); err == nil {
	//		fmt.Fprintf(os.Stderr, "go: warning: ignoring go.mod in $GOPATH %v\n", gopath)
	//		if RootMode == NeedRoot {
	//			log.Fatal(ErrNoModRoot)
	//		}
	//		if !mustUseModules {
	//			return
	//		}
	//	}
	//}
}

// Enabled reports whether modules are (or must be) enabled.
// If modules are enabled but there is no main module, Enabled returns true
// and then the first use of module information will call die
// (usually through MustModRoot).
func Enabled() bool {
	//Init()
	return modRoots != nil //|| cfg.ModulesEnabled
}

func inWorkspaceMode() bool {
	if !initialized {
		panic("inWorkspaceMode called before modload.Init called")
	}
	if !Enabled() {
		return false
	}
	return workFilePath != ""
}

// HasModRoot reports whether a main module is present.
// HasModRoot may return false even if Enabled returns true: for example, 'get'
// does not require a main module.
func HasModRoot() bool {
	Init()
	return modRoots != nil
}

func modFilePath(modRoot string) string {
	//if cfg.ModFile != "" {
	//	return cfg.ModFile
	//}
	return filepath.Join(modRoot, "go.mod")
}

var ErrNoModRoot = errors.New("go.mod file not found in current directory or any parent directory; see 'go help modules'")

type goModDirtyError struct{}

func (goModDirtyError) Error() string {
	/*if cfg.BuildModExplicit {
		return fmt.Sprintf("updates to go.mod needed, disabled by -mod=; to update it:\n\tgo mod tidy")
	}
	if cfg.BuildModReason != "" {
		return fmt.Sprintf("updates to go.mod needed, disabled by -mod=\n\t(%s)\n\tto update it:\n\tgo mod tidy", cfg.BuildModReason)
	}*/
	return "updates to go.mod needed; to update it:\n\tgo mod tidy"
}

var errGoModDirty error = goModDirtyError{}

// LoadModFile sets Target and, if there is a main module, parses the initial
// build list from its go.mod file.
//
// LoadModFile may make changes in memory, like adding a go directive and
// ensuring requirements are consistent. The caller is responsible for ensuring
// those changes are written to disk by calling LoadPackages or ListModules
// (unless ExplicitWriteGoMod is set) or by calling WriteGoMod directly.
//
// As a side-effect, LoadModFile may change cfg.BuildMod to "vendor" if
// -mod wasn't set explicitly and automatic vendoring should be enabled.
//
// If LoadModFile or CreateModFile has already been called, LoadModFile returns
// the existing in-memory requirements (rather than re-reading them from disk).
//
// LoadModFile checks the roots of the module graph for consistency with each
// other, but unlike LoadModGraph does not load the full module graph or check
// it for global consistency. Most callers outside of the modload package should
// use LoadModGraph instead.
func LoadModFile(ctx context.Context) *Requirements {
	rs, err := loadModFile(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	return rs
}

func loadModFile(ctx context.Context, opts *PackageOpts) (*Requirements, error) {
	Init()
	var workFile *modfile.WorkFile

	modfetch.GoSumFile = strings.TrimSuffix(modFilePath(modRoots[0]), ".mod") + ".sum"
	//}
	if len(modRoots) == 0 {
		// TODO(#49228): Instead of creating a fake module with an empty modroot,
		// make MainModules.Len() == 0 mean that we're in module mode but not inside
		// any module.
		mainModule := module.Version{Path: "command-line-arguments"}
		MainModules = makeMainModules([]module.Version{mainModule}, []string{""}, []*modfile.File{nil}, []*modFileIndex{nil}, nil)
		var (
			goVersion string
			pruning   modPruning
			roots     []module.Version
			direct    = map[string]bool{"go": true}
		)
		if inWorkspaceMode() {
			// Since we are in a workspace, the Go version for the synthetic
			// "command-line-arguments" module must not exceed the Go version
			// for the workspace.
			goVersion = MainModules.GoVersion()
			pruning = workspace
			roots = []module.Version{
				mainModule,
				{Path: "go", Version: goVersion},
				{Path: "toolchain", Version: gover2.LocalToolchain()},
			}
		} else {
			goVersion = gover2.Local()
			pruning = pruningForGoVersion(goVersion)
			roots = []module.Version{
				{Path: "go", Version: goVersion},
				{Path: "toolchain", Version: gover2.LocalToolchain()},
			}
		}
		rawGoVersion.Store(mainModule, goVersion)
		requirements = newRequirements(pruning, roots, direct)
		//if cfg.BuildMod == "vendor" {
		//	// For issue 56536: Some users may have GOFLAGS=-mod=vendor set.
		//	// Make sure it behaves as though the fake module is vendored
		//	// with no dependencies.
		//	requirements.initVendor(nil)
		//}
		return requirements, nil
	}

	var modFiles []*modfile.File
	var mainModules []module.Version
	var indices []*modFileIndex
	var errs []error
	for _, modroot := range modRoots {
		gomod := modFilePath(modroot)
		var fixed bool
		data, f, err := ReadModFile(gomod, fixVersion(ctx, &fixed))
		if err != nil {
			if inWorkspaceMode() {
				var tooNew *gover2.TooNewError
				if errors.As(err, &tooNew) /*&& !strings.HasPrefix(cfg.CmdName, "work ") */ {
					// Switching to a newer toolchain won't help - the go.work has the wrong version.
					// Report this more specific error, unless we are a command like 'go work use'
					// or 'go work sync', which will fix the problem after the caller sees the TooNewError
					// and switches to a newer toolchain.
					err = errWorkTooOld(gomod, workFile, tooNew.GoVersion)
				}
			}
			errs = append(errs, err)
			continue
		}
		if inWorkspaceMode() /*&& !strings.HasPrefix(cfg.CmdName, "work ")*/ {
			// Refuse to use workspace if its go version is too old.
			// Disable this check if we are a workspace command like work use or work sync,
			// which will fix the problem.
			mv := gover2.FromGoMod(f)
			wv := gover2.FromGoWork(workFile)
			if gover2.Compare(mv, wv) > 0 && gover2.Compare(mv, gover2.GoStrictVersion) >= 0 {
				errs = append(errs, errWorkTooOld(gomod, workFile, mv))
				continue
			}
		}

		modFiles = append(modFiles, f)
		mainModule := f.Module.Mod
		mainModules = append(mainModules, mainModule)
		indices = append(indices, indexModFile(data, f, mainModule, fixed))

		if err = module.CheckImportPath(f.Module.Mod.Path); err != nil {
			var pathErr *module.InvalidPathError
			if errors.As(err, &pathErr) {
				pathErr.Kind = "module"
			}
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	MainModules = makeMainModules(mainModules, modRoots, modFiles, indices, workFile)
	//setDefaultBuildMod() // possibly enable automatic vendoring
	rs := requirementsFromModFiles(ctx, workFile, modFiles, opts)

	//if cfg.BuildMod == "vendor" {
	//	readVendorList(VendorDir())
	//	var indexes []*modFileIndex
	//	var modFiles []*modfile.File
	//	var modRoots []string
	//	for _, m := range MainModules.Versions() {
	//		indexes = append(indexes, MainModules.Index(m))
	//		modFiles = append(modFiles, MainModules.ModFile(m))
	//		modRoots = append(modRoots, MainModules.ModRoot(m))
	//	}
	//	checkVendorConsistency(indexes, modFiles, modRoots)
	//	rs.initVendor(vendorList)
	//}

	if inWorkspaceMode() {
		// We don't need to update the mod file so return early.
		requirements = rs
		return rs, nil
	}

	mainModule := MainModules.mustGetSingleMainModule()

	//if rs.hasRedundantRoot() {
	//	// If any module path appears more than once in the roots, we know that the
	//	// go.mod file needs to be updated even though we have not yet loaded any
	//	// transitive dependencies.
	//	var err error
	//	rs, err = updateRoots(ctx, rs.direct, rs, nil, nil, false)
	//	if err != nil {
	//		return nil, err
	//	}
	//}

	if MainModules.Index(mainModule).goVersion == "" && rs.pruning != workspace {
		// TODO(#45551): Do something more principled instead of checking
		// cfg.CmdName directly here.
		//if /*cfg.BuildMod == "mod" && */ /*cfg.CmdName != "mod graph" && cfg.CmdName != "mod why" */{
		//	// go line is missing from go.mod; add one there and add to derived requirements.
		//	v := gover.Local()
		//	if opts != nil && opts.TidyGoVersion != "" {
		//		v = opts.TidyGoVersion
		//	}
		//	addGoStmt(MainModules.ModFile(mainModule), mainModule, v)
		//	rs = overrideRoots(ctx, rs, []module.Version{{Path: "go", Version: v}})
		//
		//	// We need to add a 'go' version to the go.mod file, but we must assume
		//	// that its existing contents match something between Go 1.11 and 1.16.
		//	// Go 1.11 through 1.16 do not support graph pruning, but the latest Go
		//	// version uses a pruned module graph — so we need to convert the
		//	// requirements to support pruning.
		//	if gover.Compare(v, gover.ExplicitIndirectVersion) >= 0 {
		//		var err error
		//		rs, err = convertPruning(ctx, rs, pruned)
		//		if err != nil {
		//			return nil, err
		//		}
		//	}
		//} else {
		rawGoVersion.Store(mainModule, gover2.DefaultGoModVersion)
		//}
	}

	requirements = rs
	return requirements, nil
}

func errWorkTooOld(gomod string, wf *modfile.WorkFile, goVers string) error {
	return fmt.Errorf("module %s listed in go.work file requires go >= %s, but go.work lists go %s; to update it:\n\tgo work use",
		base2.ShortPath(filepath.Dir(gomod)), goVers, gover2.FromGoWork(wf))
}

// fixVersion returns a modfile.VersionFixer implemented using the Query function.
//
// It resolves commit hashes and branch names to versions,
// canonicalizes versions that appeared in early vgo drafts,
// and does nothing for versions that already appear to be canonical.
//
// The VersionFixer sets 'fixed' if it ever returns a non-canonical version.
func fixVersion(ctx context.Context, fixed *bool) modfile.VersionFixer {
	return func(path, vers string) (resolved string, err error) {
		defer func() {
			if err == nil && resolved != vers {
				*fixed = true
			}
		}()

		// Special case: remove the old -gopkgin- hack.
		if strings.HasPrefix(path, "gopkg.in/") && strings.Contains(vers, "-gopkgin-") {
			vers = vers[strings.Index(vers, "-gopkgin-")+len("-gopkgin-"):]
		}

		// fixVersion is called speculatively on every
		// module, version pair from every go.mod file.
		// Avoid the query if it looks OK.
		_, pathMajor, ok := module.SplitPathVersion(path)
		if !ok {
			return "", &module.ModuleError{
				Path: path,
				Err: &module.InvalidVersionError{
					Version: vers,
					Err:     fmt.Errorf("malformed module path %q", path),
				},
			}
		}
		if vers != "" && module.CanonicalVersion(vers) == vers {
			if err := module.CheckPathMajor(vers, pathMajor); err != nil {
				return "", module.VersionError(module.Version{Path: path, Version: vers}, err)
			}
			return vers, nil
		}

		info, err := Query(ctx, path, vers, "", nil)
		if err != nil {
			return "", err
		}
		return info.Version, nil
	}
}

// makeMainModules creates a MainModuleSet and associated variables according to
// the given main modules.
func makeMainModules(ms []module.Version, rootDirs []string, modFiles []*modfile.File, indices []*modFileIndex, workFile *modfile.WorkFile) *MainModuleSet {
	for _, m := range ms {
		if m.Version != "" {
			panic("mainModulesCalled with module.Version with non empty Version field: " + fmt.Sprintf("%#v", m))
		}
	}
	modRootContainingCWD := findModuleRoot(base2.Cwd())
	mainModules := &MainModuleSet{
		versions:        slices.Clip(ms),
		inGorootSrc:     map[module.Version]bool{},
		pathPrefix:      map[module.Version]string{},
		modRoot:         map[module.Version]string{},
		modFiles:        map[module.Version]*modfile.File{},
		indices:         map[module.Version]*modFileIndex{},
		highestReplaced: map[string]string{},
		workFile:        workFile,
	}
	var workFileReplaces []*modfile.Replace
	if workFile != nil {
		workFileReplaces = workFile.Replace
		mainModules.workFileReplaceMap = toReplaceMap(workFile.Replace)
	}
	mainModulePaths := make(map[string]bool)
	for _, m := range ms {
		if mainModulePaths[m.Path] {
			log.Fatalf("go: module %s appears multiple times in workspace", m.Path)
		}
		mainModulePaths[m.Path] = true
	}
	replacedByWorkFile := make(map[string]bool)
	replacements := make(map[module.Version]module.Version)
	for _, r := range workFileReplaces {
		if mainModulePaths[r.Old.Path] && r.Old.Version == "" {
			log.Fatalf("go: workspace module %v is replaced at all versions in the go.work file. To fix, remove the replacement from the go.work file or specify the version at which to replace the module.", r.Old.Path)
		}
		replacedByWorkFile[r.Old.Path] = true
		v, ok := mainModules.highestReplaced[r.Old.Path]
		if !ok || gover2.ModCompare(r.Old.Path, r.Old.Version, v) > 0 {
			mainModules.highestReplaced[r.Old.Path] = r.Old.Version
		}
		replacements[r.Old] = r.New
	}
	for i, m := range ms {
		mainModules.pathPrefix[m] = m.Path
		mainModules.modRoot[m] = rootDirs[i]
		mainModules.modFiles[m] = modFiles[i]
		mainModules.indices[m] = indices[i]

		if mainModules.modRoot[m] == modRootContainingCWD {
			mainModules.modContainingCWD = m
		}

		//if rel := search.InDir(rootDirs[i], cfg.GOROOTsrc); rel != "" {
		//	mainModules.inGorootSrc[m] = true
		//	if m.Path == "std" {
		//		// The "std" module in GOROOT/src is the Go standard library. Unlike other
		//		// modules, the packages in the "std" module have no import-path prefix.
		//		//
		//		// Modules named "std" outside of GOROOT/src do not receive this special
		//		// treatment, so it is possible to run 'go test .' in other GOROOTs to
		//		// test individual packages using a combination of the modified package
		//		// and the ordinary standard library.
		//		// (See https://golang.org/issue/30756.)
		//		mainModules.pathPrefix[m] = ""
		//	}
		//}

		if modFiles[i] != nil {
			curModuleReplaces := make(map[module.Version]bool)
			for _, r := range modFiles[i].Replace {
				if replacedByWorkFile[r.Old.Path] {
					continue
				}
				var newV module.Version = r.New
				if WorkFilePath() != "" && newV.Version == "" && !filepath.IsAbs(newV.Path) {
					// Since we are in a workspace, we may be loading replacements from
					// multiple go.mod files. Relative paths in those replacement are
					// relative to the go.mod file, not the workspace, so the same string
					// may refer to two different paths and different strings may refer to
					// the same path. Convert them all to be absolute instead.
					//
					// (We could do this outside of a workspace too, but it would mean that
					// replacement paths in error strings needlessly differ from what's in
					// the go.mod file.)
					newV.Path = filepath.Join(rootDirs[i], newV.Path)
				}
				if prev, ok := replacements[r.Old]; ok && !curModuleReplaces[r.Old] && prev != newV {
					log.Fatalf("go: conflicting replacements for %v:\n\t%v\n\t%v\nuse \"go work edit -replace %v=[override]\" to resolve", r.Old, prev, newV, r.Old)
				}
				curModuleReplaces[r.Old] = true
				replacements[r.Old] = newV

				v, ok := mainModules.highestReplaced[r.Old.Path]
				if !ok || gover2.ModCompare(r.Old.Path, r.Old.Version, v) > 0 {
					mainModules.highestReplaced[r.Old.Path] = r.Old.Version
				}
			}
		}
	}
	return mainModules
}

// requirementsFromModFiles returns the set of non-excluded requirements from
// the global modFile.
func requirementsFromModFiles(ctx context.Context, workFile *modfile.WorkFile, modFiles []*modfile.File, opts *PackageOpts) *Requirements {
	var roots []module.Version
	direct := map[string]bool{}
	var pruning modPruning
	if inWorkspaceMode() {
		pruning = workspace
		roots = make([]module.Version, len(MainModules.Versions()), 2+len(MainModules.Versions()))
		copy(roots, MainModules.Versions())
		goVersion := gover2.FromGoWork(workFile)
		var toolchain string
		if workFile.Toolchain != nil {
			toolchain = workFile.Toolchain.Name
		}
		roots = appendGoAndToolchainRoots(roots, goVersion, toolchain, direct)
	} else {
		pruning = pruningForGoVersion(MainModules.GoVersion())
		if len(modFiles) != 1 {
			panic(fmt.Errorf("requirementsFromModFiles called with %v modfiles outside workspace mode", len(modFiles)))
		}
		modFile := modFiles[0]
		roots, direct = rootsFromModFile(MainModules.mustGetSingleMainModule(), modFile, withToolchainRoot)
	}

	gover2.ModSort(roots)
	rs := newRequirements(pruning, roots, direct)
	return rs
}

type addToolchainRoot bool

const (
	omitToolchainRoot addToolchainRoot = false
	withToolchainRoot                  = true
)

func rootsFromModFile(m module.Version, modFile *modfile.File, addToolchainRoot addToolchainRoot) (roots []module.Version, direct map[string]bool) {
	direct = make(map[string]bool)
	padding := 2 // Add padding for the toolchain and go version, added upon return.
	if !addToolchainRoot {
		padding = 1
	}
	roots = make([]module.Version, 0, padding+len(modFile.Require))
	for _, r := range modFile.Require {
		if index := MainModules.Index(m); index != nil && index.exclude[r.Mod] {
			/*if cfg.BuildMod == "mod" {
				fmt.Fprintf(os.Stderr, "go: dropping requirement on excluded version %s %s\n", r.Mod.Path, r.Mod.Version)
			} else {
				fmt.Fprintf(os.Stderr, "go: ignoring requirement on excluded version %s %s\n", r.Mod.Path, r.Mod.Version)
			}*/
			continue
		}

		roots = append(roots, r.Mod)
		if !r.Indirect {
			direct[r.Mod.Path] = true
		}
	}
	goVersion := gover2.FromGoMod(modFile)
	var toolchain string
	if addToolchainRoot && modFile.Toolchain != nil {
		toolchain = modFile.Toolchain.Name
	}
	roots = appendGoAndToolchainRoots(roots, goVersion, toolchain, direct)
	return roots, direct
}

func appendGoAndToolchainRoots(roots []module.Version, goVersion, toolchain string, direct map[string]bool) []module.Version {
	// Add explicit go and toolchain versions, inferring as needed.
	roots = append(roots, module.Version{Path: "go", Version: goVersion})
	direct["go"] = true // Every module directly uses the language and runtime.

	if toolchain != "" {
		roots = append(roots, module.Version{Path: "toolchain", Version: toolchain})
		// Leave the toolchain as indirect: nothing in the user's module directly
		// imports a package from the toolchain, and (like an indirect dependency in
		// a module without graph pruning) we may remove the toolchain line
		// automatically if the 'go' version is changed so that it implies the exact
		// same toolchain.
	}
	return roots
}

//// setDefaultBuildMod sets a default value for cfg.BuildMod if the -mod flag
//// wasn't provided. setDefaultBuildMod may be called multiple times.
//func setDefaultBuildMod() {
//	/*if cfg.BuildModExplicit {
//	//if inWorkspaceMode() /* && cfg.BuildMod != "readonly" && cfg.BuildMod != "vendor"*/ //{
//	//	log.Fatalf("go: -mod may only be set to readonly or vendor when in workspace mode, but it is set to %q" +
//	//		"\n\tRemove the -mod flag to use the default readonly value, " +
//	//		"\n\tor set GOWORK=off to disable workspace mode." /*cfg.BuildMod*/)
//	//}
//	// Don't override an explicit '-mod=' argument.
//	return
//	//}*/
//
//	// TODO(#40775): commands should pass in the module mode as an option
//	// to modload functions instead of relying on an implicit setting
//	// based on command name.
//	/*switch cfg.CmdName {
//	case "get", "mod download", "mod init", "mod tidy", "work sync":
//		// These commands are intended to update go.mod and go.sum.
//		//cfg.BuildMod = "mod"
//		return
//	case "mod graph", "mod verify", "mod why":
//		// These commands should not update go.mod or go.sum, but they should be
//		// able to fetch modules not in go.sum and should not report errors if
//		// go.mod is inconsistent. They're useful for debugging, and they need
//		// to work in buggy situations.
//		//cfg.BuildMod = "mod"
//		return
//	case "mod vendor", "work vendor":
//		//cfg.BuildMod = "readonly"
//		return
//	}*/
//	if modRoots == nil {
//		if allowMissingModuleImports {
//			//cfg.BuildMod = "mod"
//		} else {
//			//cfg.BuildMod = "readonly"
//		}
//		return
//	}
//
//	if len(modRoots) >= 1 {
//		var goVersion string
//		var versionSource string
//		if inWorkspaceMode() {
//			versionSource = "go.work"
//			if wfg := MainModules.WorkFile().Go; wfg != nil {
//				goVersion = wfg.Version
//			}
//		} else {
//			versionSource = "go.mod"
//			index := MainModules.GetSingleIndexOrNil()
//			if index != nil {
//				goVersion = index.goVersion
//			}
//		}
//		vendorDir := ""
//		if workFilePath != "" {
//			vendorDir = filepath.Join(filepath.Dir(workFilePath), "vendor")
//		} else {
//			if len(modRoots) != 1 {
//				panic(fmt.Errorf("outside workspace mode, but have %v modRoots", modRoots))
//			}
//			vendorDir = filepath.Join(modRoots[0], "vendor")
//		}
//		if fi, err := os.Stat(vendorDir); err == nil && fi.IsDir() {
//			//modGo := "unspecified"
//			if goVersion != "" {
//				if gover.Compare(goVersion, "1.14") < 0 {
//					// The go version is less than 1.14. Don't set -mod=vendor by default.
//					// Since a vendor directory exists, we should record why we didn't use it.
//					// This message won't normally be shown, but it may appear with import errors.
//					//cfg.BuildModReason = fmt.Sprintf("Go version in "+versionSource+" is %s, so vendor directory was not used.", modGo)
//				} else {
//					vendoredWorkspace, err := modulesTextIsForWorkspace(vendorDir)
//					if err != nil {
//						log.Fatalf("go: reading modules.txt for vendor directory: %v", err)
//					}
//					if vendoredWorkspace != (versionSource == "go.work") {
//						//if vendoredWorkspace {
//						//	cfg.BuildModReason = "Outside workspace mode, but vendor directory is for a workspace."
//						//} else {
//						//	cfg.BuildModReason = "In workspace mode, but vendor directory is not for a workspace"
//						//}
//					} else {
//						// The Go version is at least 1.14, a vendor directory exists, and
//						// the modules.txt was generated in the same mode the command is running in.
//						// Set -mod=vendor by default.
//						//cfg.BuildMod = "vendor"
//						//cfg.BuildModReason = "Go version in " + versionSource + " is at least 1.14 and vendor directory exists."
//						return
//					}
//				}
//				//modGo = goVersion
//			}
//
//		}
//	}
//
//	/*cfg.BuildMod = "readonly"*/
//}
//
//func modulesTextIsForWorkspace(vendorDir string) (bool, error) {
//	f, err := os.Open(filepath.Join(vendorDir, "modules.txt"))
//	if errors.Is(err, os.ErrNotExist) {
//		// Some vendor directories exist that don't contain modules.txt.
//		// This mostly happens when converting to modules.
//		// We want to preserve the behavior that mod=vendor is set (even though
//		// readVendorList does nothing in that case).
//		return false, nil
//	}
//	if err != nil {
//		return false, err
//	}
//	var buf [512]byte
//	n, err := f.Read(buf[:])
//	if err != nil && err != io.EOF {
//		return false, err
//	}
//	line, _, _ := strings.Cut(string(buf[:n]), "\n")
//	if annotations, ok := strings.CutPrefix(line, "## "); ok {
//		for _, entry := range strings.Split(annotations, ";") {
//			entry = strings.TrimSpace(entry)
//			if entry == "workspace" {
//				return true, nil
//			}
//		}
//	}
//	return false, nil
//}

func mustHaveCompleteRequirements() bool {
	return /* cfg.BuildMod != "mod" && */ !inWorkspaceMode()
}

func findModuleRoot(dir string) (roots string) {
	if dir == "" {
		panic("dir not set")
	}
	dir = filepath.Clean(dir)

	// Look for enclosing go.mod.
	for {
		if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !fi.IsDir() {
			return dir
		}
		d := filepath.Dir(dir)
		if d == dir {
			break
		}
		dir = d
	}
	return ""
}

// WriteOpts control the behavior of WriteGoMod.
type WriteOpts struct {
	DropToolchain     bool // go get toolchain@none
	ExplicitToolchain bool // go get has set explicit toolchain version

	// TODO(bcmills): Make 'go mod tidy' update the go version in the Requirements
	// instead of writing directly to the modfile.File
	TidyWroteGo bool // Go.Version field already updated by 'go mod tidy'
}
