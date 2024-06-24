package envcmd

import (
	"fmt"
	"os"
)

//{
//        "AR": "ar",
//        "CC": "gcc",
//        "CGO_CFLAGS": "-O2 -g",
//        "CGO_CPPFLAGS": "",
//        "CGO_CXXFLAGS": "-O2 -g",
//        "CGO_ENABLED": "1",
//        "CGO_FFLAGS": "-O2 -g",
//        "CGO_LDFLAGS": "-O2 -g",
//        "CXX": "g++",
//        "GCCGO": "gccgo",
//        "GO111MODULE": "",
//        "GOAMD64": "v1",
//        "GOARCH": "amd64",
//        "GOBIN": "",
//        "GOCACHE": "C:\\Users\\ddaniels\\AppData\\Local\\go-build",
//        "GOENV": "C:\\Users\\ddaniels\\AppData\\Roaming\\go\\env",
//        "GOEXE": ".exe",
//        "GOEXPERIMENT": "",
//        "GOFLAGS": "",
//        "GOGCCFLAGS": "-m64 -mthreads -Wl,--no-gc-sections -fmessage-length=0 -ffile-prefix-map=C:\\Users\\ddaniels\\AppData\\Local\\Temp\\go-build2637212402=/tmp/go-build -gno-record-gcc-switches",
//        "GOHOSTARCH": "amd64",
//        "GOHOSTOS": "windows",
//        "GOINSECURE": "",
//        "GOMOD": "NUL",
//        "GOMODCACHE": "C:\\Users\\ddaniels\\go\\pkg\\mod",
//        "GONOPROXY": "",
//        "GONOSUMDB": "",
//        "GOOS": "windows",
//        "GOPATH": "C:\\Users\\ddaniels\\go",
//        "GOPRIVATE": "",
//        "GOPROXY": "https://proxy.golang.org,direct",
//        "GOROOT": "C:\\Users\\ddaniels\\scoop\\apps\\go\\current",
//        "GOSUMDB": "sum.golang.org",
//        "GOTMPDIR": "",
//        "GOTOOLCHAIN": "auto",
//        "GOTOOLDIR": "C:\\Users\\ddaniels\\scoop\\apps\\go\\current\\pkg\\tool\\windows_amd64",
//        "GOVCS": "",
//        "GOVERSION": "go1.22.4",
//        "GOWORK": "",
//        "PKG_CONFIG": "pkg-config"
//}

type EnvVar struct {
	Name  string
	Value string
}

type ModEnv struct {
	env map[string]string
}

//func NewModEnv() *ModEnv {
//	envFile, _ := cfg.EnvFile()
//	env := []EnvVar{
//		{Name: "GO111MODULE", Value: cfg.Getenv("GO111MODULE")},
//		{Name: "GOARCH", Value: cfg.Goarch},
//		{Name: "GOBIN", Value: cfg.GOBIN},
//		{Name: "GOCACHE", Value: cache.DefaultDir()},
//		{Name: "GOENV", Value: envFile},
//		{Name: "GOEXE", Value: cfg.ExeSuffix},
//
//		// List the raw value of GOEXPERIMENT, not the cleaned one.
//		// The set of default experiments may change from one release
//		// to the next, so a GOEXPERIMENT setting that is redundant
//		// with the current toolchain might actually be relevant with
//		// a different version (for example, when bisecting a regression).
//		{Name: "GOEXPERIMENT", Value: cfg.RawGOEXPERIMENT},
//
//		{Name: "GOFLAGS", Value: cfg.Getenv("GOFLAGS")},
//		{Name: "GOHOSTARCH", Value: runtime.GOARCH},
//		{Name: "GOHOSTOS", Value: runtime.GOOS},
//		{Name: "GOINSECURE", Value: cfg.GOINSECURE},
//		{Name: "GOMODCACHE", Value: cfg.GOMODCACHE},
//		{Name: "GONOPROXY", Value: cfg.GONOPROXY},
//		{Name: "GONOSUMDB", Value: cfg.GONOSUMDB},
//		{Name: "GOOS", Value: cfg.Goos},
//		{Name: "GOPATH", Value: cfg.BuildContext.GOPATH},
//		{Name: "GOPRIVATE", Value: cfg.GOPRIVATE},
//		{Name: "GOPROXY", Value: cfg.GOPROXY},
//		{Name: "GOROOT", Value: cfg.GOROOT},
//		{Name: "GOSUMDB", Value: cfg.GOSUMDB},
//		{Name: "GOTMPDIR", Value: cfg.Getenv("GOTMPDIR")},
//		{Name: "GOTOOLCHAIN", Value: cfg.Getenv("GOTOOLCHAIN")},
//		{Name: "GOTOOLDIR", Value: build.ToolDir},
//		{Name: "GOVCS", Value: cfg.GOVCS},
//		{Name: "GOVERSION", Value: runtime.Version()},
//	}
//
//	dir, err := os.UserConfigDir()
//	if err != nil {
//		return "", err
//	}
//}

func (m *ModEnv) loadEnv() {
	for _, e := range os.Environ() {
		pair := splitEnv(e)
		m.env[pair[0]] = pair[1]
	}
}

func splitEnv(env string) [2]string {
	for i, char := range env {
		if char == '=' {
			return [2]string{env[:i], env[i+1:]}
		}
	}
	return [2]string{env, ""}
}

func (m *ModEnv) Get(key string) string {
	return m.env[key]
}

func (m *ModEnv) Set(key, value string) {
	m.env[key] = value
	os.Setenv(key, value)
}

func (m *ModEnv) Unset(key string) {
	delete(m.env, key)
	os.Unsetenv(key)
}

func (m *ModEnv) PrintEnv() {
	for key, value := range m.env {
		fmt.Printf("%s=%s\n", key, value)
	}
}
