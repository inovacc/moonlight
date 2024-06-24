package config

import (
	"fmt"
	"github.com/inovacc/moonlight/pkg/module/internal/modfetch/codehost"
	"github.com/spf13/viper"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const proxyDefault = "https://proxy.golang.org,direct"

var cfg = &Config{}

func init() {
	cfgFile, err := filepath.Abs("../../config.yaml")
	if err != nil {
		panic(err)
	}

	viper.SetConfigFile(cfgFile)
	viper.AutomaticEnv()

	// If a config file is found, read it in.
	if err = viper.ReadInConfig(); err != nil {
		panic(err)
	}

	if err = viper.Unmarshal(cfg); err != nil {
		panic(err)
	}

	if err = cfg.Validate(); err != nil {
		panic(err)
	}
}

var proxyOnce struct {
	sync.Once
	list []ProxySpec
	err  error
}

// A RevInfo describes a single revision in a module repository.
type RevInfo struct {
	Version string    // suggested version string for this revision
	Time    time.Time // commit time

	// These fields are used for Stat of arbitrary rev,
	// but they are not recorded when talking about module versions.
	Name  string `json:"-"` // complete ID in underlying repository
	Short string `json:"-"` // shortened ID, for use in pseudo-version

	Origin *codehost.Origin `json:",omitempty"` // provenance for reuse
}

type Config struct {
	ProxyServers []string `yaml:"proxyServers" mapstructure:"proxyServers"`
	NoProxy      bool     `yaml:"noproxy" mapstructure:"noproxy"`
}

func (c *Config) Validate() error {
	if c.ProxyServers == nil {
		c.ProxyServers = []string{proxyDefault}
	}
	return nil
}

type ProxySpec struct {
	// Url is the proxy URL or one of "off", "direct", "noproxy".
	Url string

	// FallBackOnError is true if a request should be attempted on the next proxy
	// in the list after any error from this proxy. If FallBackOnError is false,
	// the request will only be attempted on the next proxy if the error is
	// equivalent to os.ErrNotFound, which is true for 404 and 410 responses.
	FallBackOnError bool
}

type proxyRepoFn func(baseURL, path string) (any, error)

func ProxyList(fn proxyRepoFn) ([]ProxySpec, error) {
	proxyOnce.Do(func() {
		//if cfg.GONOPROXY != "" && cfg.GOPROXY != "direct" {
		//	proxyOnce.list = append(proxyOnce.list, ProxySpec{url: "noproxy"})
		//}

		goproxy := proxyDefault

		for goproxy != "" {
			var u string
			fallBackOnError := false
			if i := strings.IndexAny(goproxy, ",|"); i >= 0 {
				u = goproxy[:i]
				fallBackOnError = goproxy[i] == '|'
				goproxy = goproxy[i+1:]
			} else {
				u = goproxy
				goproxy = ""
			}

			if u = strings.TrimSpace(u); u == "" {
				continue
			}
			if u == "off" {
				// "off" always fails hard, so can stop walking list.
				proxyOnce.list = append(proxyOnce.list, ProxySpec{Url: "off"})
				break
			}
			if u == "direct" {
				proxyOnce.list = append(proxyOnce.list, ProxySpec{Url: "direct"})
				// For now, "direct" is the end of the line. We may decide to add some
				// sort of fallback behavior for them in the future, so ignore
				// subsequent entries for forward-compatibility.
				break
			}

			// Single-word tokens are reserved for built-in behaviors, and anything
			// containing the string ":/" or matching an absolute file path must be a
			// complete URL. For all other paths, implicitly add "https://".
			if strings.ContainsAny(u, ".:/") && !strings.Contains(u, ":/") && !filepath.IsAbs(u) && !path.IsAbs(u) {
				u = fmt.Sprintf("https://%s", u)
			}

			// Check that newProxyRepo accepts the URL.
			// It won't do anything with the path.
			if _, err := fn(u, "golang.org/x/text"); err != nil {
				proxyOnce.err = err
				return
			}

			proxyOnce.list = append(proxyOnce.list, ProxySpec{
				Url:             u,
				FallBackOnError: fallBackOnError,
			})
		}

		if len(proxyOnce.list) == 0 ||
			len(proxyOnce.list) == 1 && proxyOnce.list[0].Url == "noproxy" {
			// There were no proxies, other than the implicit "noproxy" added when
			// GONOPROXY is set. This can happen if GOPROXY is a non-empty string
			// like "," or " ".
			proxyOnce.err = fmt.Errorf("GOPROXY list is not the empty string, but contains no entries")
		}
	})

	return proxyOnce.list, proxyOnce.err
}
