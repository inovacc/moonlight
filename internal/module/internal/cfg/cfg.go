package cfg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	GOMODCACHE = envOr("GOMODCACHE", gopathDir("pkg/mod"))
	SumdbDir   = gopathDir("pkg/sumdb")
)

var envCache struct {
	once sync.Once
	m    map[string]string
}

// envOr returns Getenv(key) if set, or else def.
func envOr(key, def string) string {
	val := Getenv(key)
	if val == "" {
		val = def
	}
	return val
}

func gopathDir(rel string) string {
	list := filepath.SplitList(envOr("GOPATH", ""))
	if len(list) == 0 || list[0] == "" {
		return ""
	}
	return filepath.Join(list[0], rel)
}

// Getenv gets the value for the configuration key.
// It consults the operating system environment
// and then the go/env file.
// If Getenv is called for a key that cannot be set
// in the go/env file (for example GODEBUG), it panics.
// This ensures that CanGetenv is accurate, so that
// 'go env -w' stays in sync with what Getenv can retrieve.
func Getenv(key string) string {
	val := os.Getenv(key)
	if val != "" {
		return val
	}
	envCache.once.Do(initEnvCache)
	return envCache.m[key]
}

// EnvFile returns the name of the Go environment configuration file.
func EnvFile() (string, error) {
	file := os.Getenv("GOENV")
	if file != "" {
		return file, nil
	}
	return "", fmt.Errorf("GOENV not set")
}

func initEnvCache() {
	envCache.m = make(map[string]string)
	if file, _ := EnvFile(); file != "" {
		readEnvFile(file, "user")
	}
	goroot := findGOROOT(envCache.m["GOROOT"])
	if goroot != "" {
		readEnvFile(filepath.Join(goroot, "go.env"), "GOROOT")
	}

	// Save the goroot for func init calling SetGOROOT,
	// and also overwrite anything that might have been in go.env.
	// It makes no sense for GOROOT/go.env to specify
	// a different GOROOT.
	envCache.m["GOROOT"] = goroot
}

// findGOROOT returns the GOROOT value, using either an explicitly
// provided environment variable, a GOROOT that contains the current
// os.Executable value, or else the GOROOT that the binary was built
// with from runtime.GOROOT().
//
// There is a copy of this code in x/tools/cmd/godoc/goroot.go.
func findGOROOT(env string) string {
	if env == "" {
		// Not using Getenv because findGOROOT is called
		// to find the GOROOT/go.env file. initEnvCache
		// has passed in the setting from the user go/env file.
		env = os.Getenv("GOROOT")
	}
	if env != "" {
		return filepath.Clean(env)
	}
	def := ""
	if r := runtime.GOROOT(); r != "" {
		def = filepath.Clean(r)
	}
	if runtime.Compiler == "gccgo" {
		// gccgo has no real GOROOT, and it certainly doesn't
		// depend on the executable's location.
		return def
	}
	return def
}

func readEnvFile(file string, source string) {
	if file == "" {
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}

	for len(data) > 0 {
		// Get next line.
		line := data
		i := bytes.IndexByte(data, '\n')
		if i >= 0 {
			line, data = line[:i], data[i+1:]
		} else {
			data = nil
		}

		i = bytes.IndexByte(line, '=')
		if i < 0 || line[0] < 'A' || 'Z' < line[0] {
			// Line is missing = (or empty) or a comment or not a valid env name. Ignore.
			// This should not happen in the user file, since the file should be maintained almost
			// exclusively by "go env -w", but better to silently ignore than to make
			// the go command unusable just because somehow the env file has
			// gotten corrupted.
			// In the GOROOT/go.env file, we expect comments.
			continue
		}
		key, val := line[:i], line[i+1:]

		if source == "GOROOT" {
			// In the GOROOT/go.env file, do not overwrite fields loaded from the user's go/env file.
			if _, ok := envCache.m[string(key)]; ok {
				continue
			}
		}
		envCache.m[string(key)] = string(val)
	}
}
