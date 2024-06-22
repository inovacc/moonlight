package versions

import (
	"encoding/json"
	"fmt"
	"github.com/blang/semver"
	"io"
	"net/http"
	"sort"
	"strings"
)

const (
	goUrl = "https://go.dev/dl/?mode=json&include=all"
)

type kindString string

const (
	kindSource    kindString = "source"
	kindBinary    kindString = "binary"
	kindInstaller kindString = "installer"
	kindArchive   kindString = "archive"
)

type archString string

const (
	archAmd64    archString = "amd64"
	arch386      archString = "386"
	archArm64    archString = "arm64"
	archArm      archString = "arm"
	archPpc64    archString = "ppc64"
	archMips64   archString = "mips64"
	archMips64le archString = "mips64le"
	archS390x    archString = "s390x"
	archWasm     archString = "wasm"
)

type osString string

const (
	osLinux   osString = "linux"
	osDarwin  osString = "darwin"
	osWindows osString = "windows"
)

type File struct {
	Filename string `json:"filename"`
	Os       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	Sha256   string `json:"sha256"`
	Size     int    `json:"size"`
	Kind     string `json:"kind"`
}

type Versions struct {
	ID      int    `json:"id"`
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []File `json:"files"`
}

// GetWindows returns the Windows file and a boolean indicating if it was found.
func (g *Versions) GetWindows() (*File, bool) {
	for _, f := range g.Files {
		if f.Os == string(osWindows) && f.Arch == string(archAmd64) && f.Kind == string(kindArchive) {
			return &f, true
		}
	}
	return nil, false
}

// GetLinux returns the Linux file and a boolean indicating if it was found.
func (g *Versions) GetLinux() (*File, bool) {
	for _, f := range g.Files {
		if f.Os == string(osLinux) && f.Arch == string(archAmd64) && f.Kind == string(kindArchive) {
			return &f, true
		}
	}
	return nil, false
}

// GetDarwin returns the Darwin file and a boolean indicating if it was found.
func (g *Versions) GetDarwin() (*File, bool) {
	for _, f := range g.Files {
		if f.Os == string(osDarwin) && f.Arch == string(archAmd64) && f.Kind == string(kindArchive) {
			return &f, true
		}
	}
	return nil, false
}

type GoVersion struct {
	StableVersion    string     `json:"stable"`
	Versions         []Versions `json:"versions"`
	ReleaseCandidate string     `json:"release_candidate"`
}

// NewGoVersion returns a new GoVersion.
func NewGoVersion() (*GoVersion, error) {
	goVer, err := getJSON(goUrl)
	if err != nil {
		return nil, err
	}

	releaseCandidate := goVer.Versions[0].Version

	for i := range goVer.Versions {
		for j := range goVer.Versions[i].Files {
			if goVer.Versions[i].Files[j].Kind == string(kindSource) {
				goVer.Versions[i].Files[j].Os = "any"
				goVer.Versions[i].Files[j].Arch = "any"
			}
		}
	}

	sort.Slice(goVer.Versions, func(i, j int) bool {
		verI, _ := semver.Make(strings.TrimPrefix(goVer.Versions[i].Version, "go"))
		verJ, _ := semver.Make(strings.TrimPrefix(goVer.Versions[j].Version, "go"))
		return verI.GT(verJ)
	})

	return &GoVersion{
		StableVersion:    goVer.Versions[0].Version,
		ReleaseCandidate: releaseCandidate,
		Versions:         goVer.Versions,
	}, nil
}

// getJSON returns the GoVersion struct from the Go website
func getJSON(url string) (*GoVersion, error) {
	r, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		if err = Body.Close(); err != nil {
			fmt.Println(err)
		}
	}(r.Body)

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var goVer GoVersion
	if err = json.Unmarshal(data, &goVer.Versions); err != nil {
		return nil, err
	}
	return &goVer, nil
}
