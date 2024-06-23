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

type Versions struct {
	ID      int    `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
	Stable  bool   `json:"stable,omitempty"`
	Files   []File `json:"files,omitempty"`
}

type File struct {
	ID       int    `json:"id,omitempty"`
	Version  string `json:"version,omitempty"`
	Stable   bool   `json:"stable,omitempty"`
	Filename string `json:"filename,omitempty"`
	Os       string `json:"os,omitempty"`
	Arch     string `json:"arch,omitempty"`
	Sha256   string `json:"sha256,omitempty"`
	Size     int    `json:"size,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

type GoVersion struct {
	StableVersion    string     `json:"stable,omitempty"`
	Versions         []Versions `json:"versions,omitempty"`
	ReleaseCandidate string     `json:"release_candidate,omitempty"`
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
