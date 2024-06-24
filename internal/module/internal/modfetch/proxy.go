// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modfetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/inovacc/moonlight/internal/module/internal/config"
	"github.com/inovacc/moonlight/internal/module/internal/modfetch/codehost"
	"github.com/inovacc/moonlight/internal/module/internal/web"
	"io"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// TryProxies iterates f over each configured proxy (including "noproxy" and
// "direct" if applicable) until f returns no error or until f returns an
// error that is not equivalent to fs.ErrNotExist on a proxy configured
// not to fall back on errors.
//
// TryProxies then returns that final error.
//
// If GOPROXY is set to "off", TryProxies invokes f once with the argument
// "off".
func TryProxies(f func(proxy string) error) error {
	callback := func(baseURL, path string) (any, error) {
		repo, err := newProxyRepo(baseURL, path)
		return repo.(Repo), err
	}
	proxies, err := config.ProxyList(callback)
	if err != nil {
		return err
	}
	if len(proxies) == 0 {
		panic("GOPROXY list is empty")
	}

	// We try to report the most helpful error to the user. "direct" and "noproxy"
	// errors are best, followed by proxy errors other than ErrNotExist, followed
	// by ErrNotExist.
	//
	// Note that errProxyOff, errNoproxy, and errUseProxy are equivalent to
	// ErrNotExist. errUseProxy should only be returned if "noproxy" is the only
	// proxy. errNoproxy should never be returned, since there should always be a
	// more useful error from "noproxy" first.
	const (
		notExistRank = iota
		proxyRank
		directRank
	)
	var bestErr error
	bestErrRank := notExistRank
	for _, proxy := range proxies {
		if err = f(proxy.Url); err == nil {
			return nil
		}
		isNotExistErr := errors.Is(err, fs.ErrNotExist)

		if proxy.Url == "direct" || (proxy.Url == "noproxy" && !errors.Is(err, errUseProxy)) {
			bestErr = err
			bestErrRank = directRank
		} else if bestErrRank <= proxyRank && !isNotExistErr {
			bestErr = err
			bestErrRank = proxyRank
		} else if bestErrRank == notExistRank {
			bestErr = err
		}

		if !proxy.FallBackOnError && !isNotExistErr {
			break
		}
	}
	return bestErr
}

type proxyRepo struct {
	url          *url.URL // The combined module proxy URL joined with the module path.
	path         string   // The module path (unescaped).
	redactedBase string   // The base module proxy URL in [url.URL.Redacted] form.

	listLatestOnce sync.Once
	listLatest     *RevInfo
	listLatestErr  error
}

func newProxyRepo(baseURL, path string) (Repo, error) {
	// Parse the base proxy URL.
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	redactedBase := base.Redacted()
	switch base.Scheme {
	case "http", "https":
		// ok
	case "file":
		if *base != (url.URL{Scheme: base.Scheme, Path: base.Path, RawPath: base.RawPath}) {
			return nil, fmt.Errorf("invalid file:// proxy URL with non-path elements: %s", redactedBase)
		}
	case "":
		return nil, fmt.Errorf("invalid proxy URL missing scheme: %s", redactedBase)
	default:
		return nil, fmt.Errorf("invalid proxy URL scheme (must be https, http, file): %s", redactedBase)
	}

	// Append the module path to the URL.
	u := base
	enc, err := module.EscapePath(path)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(base.Path, "/") + "/" + enc
	u.RawPath = strings.TrimSuffix(base.RawPath, "/") + "/" + pathEscape(enc)

	return &proxyRepo{u, path, redactedBase, sync.Once{}, nil, nil}, nil
}

func (p *proxyRepo) ModulePath() string {
	return p.path
}

var errProxyReuse = fmt.Errorf("proxy does not support CheckReuse")

func (p *proxyRepo) CheckReuse(ctx context.Context, old *codehost.Origin) error {
	return errProxyReuse
}

// versionError returns err wrapped in a ModuleError for p.path.
func (p *proxyRepo) versionError(version string, err error) error {
	if version != "" && version != module.CanonicalVersion(version) {
		return &module.ModuleError{
			Path: p.path,
			Err: &module.InvalidVersionError{
				Version: version,
				Pseudo:  module.IsPseudoVersion(version),
				Err:     err,
			},
		}
	}

	return &module.ModuleError{
		Path:    p.path,
		Version: version,
		Err:     err,
	}
}

func (p *proxyRepo) getBytes(ctx context.Context, path string) ([]byte, error) {
	body, redactedURL, err := p.getBody(ctx, path)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	b, err := io.ReadAll(body)
	if err != nil {
		// net/http doesn't add context to Body read errors, so add it here.
		// (See https://go.dev/issue/52727.)
		return b, &url.Error{Op: "read", URL: redactedURL, Err: err}
	}
	return b, nil
}

func (p *proxyRepo) getBody(ctx context.Context, path string) (r io.ReadCloser, redactedURL string, err error) {
	fullPath := filepath.Join(p.url.Path, path)

	target := *p.url
	target.Path = fullPath
	target.RawPath = filepath.Join(target.RawPath, pathEscape(path))

	resp, err := web.Get(web.DefaultSecurity, &target)
	if err != nil {
		return nil, "", err
	}
	if err = resp.Err(); err != nil {
		resp.Body.Close()
		return nil, "", err
	}
	return resp.Body, resp.URL, nil
}

func (p *proxyRepo) Versions(ctx context.Context, prefix string) (*Versions, error) {
	data, err := p.getBytes(ctx, "@v/list")
	if err != nil {
		p.listLatestOnce.Do(func() {
			p.listLatest, p.listLatestErr = nil, p.versionError("", err)
		})
		return nil, p.versionError("", err)
	}
	var list []string
	allLine := strings.Split(string(data), "\n")
	for _, line := range allLine {
		f := strings.Fields(line)
		if len(f) >= 1 && semver.IsValid(f[0]) && strings.HasPrefix(f[0], prefix) && !module.IsPseudoVersion(f[0]) {
			list = append(list, f[0])
		}
	}
	p.listLatestOnce.Do(func() {
		p.listLatest, p.listLatestErr = p.latestFromList(ctx, allLine)
	})
	semver.Sort(list)
	return &Versions{List: list}, nil
}

func (p *proxyRepo) latest(ctx context.Context) (*RevInfo, error) {
	p.listLatestOnce.Do(func() {
		data, err := p.getBytes(ctx, "@v/list")
		if err != nil {
			p.listLatestErr = p.versionError("", err)
			return
		}
		list := strings.Split(string(data), "\n")
		p.listLatest, p.listLatestErr = p.latestFromList(ctx, list)
	})
	return p.listLatest, p.listLatestErr
}

func (p *proxyRepo) latestFromList(ctx context.Context, allLine []string) (*RevInfo, error) {
	var (
		bestTime    time.Time
		bestVersion string
	)
	for _, line := range allLine {
		f := strings.Fields(line)
		if len(f) >= 1 && semver.IsValid(f[0]) {
			// If the proxy includes timestamps, prefer the timestamp it reports.
			// Otherwise, derive the timestamp from the pseudo-version.
			var (
				ft time.Time
			)
			if len(f) >= 2 {
				ft, _ = time.Parse(time.RFC3339, f[1])
			} else if module.IsPseudoVersion(f[0]) {
				ft, _ = module.PseudoVersionTime(f[0])
			} else {
				// Repo.Latest promises that this method is only called where there are
				// no tagged versions. Ignore any tagged versions that were added in the
				// meantime.
				continue
			}
			if bestTime.Before(ft) {
				bestTime = ft
				bestVersion = f[0]
			}
		}
	}
	if bestVersion == "" {
		return nil, p.versionError("", codehost.ErrNoCommits)
	}

	// Call Stat to get all the other fields, including Origin information.
	return p.Stat(ctx, bestVersion)
}

func (p *proxyRepo) Stat(ctx context.Context, rev string) (*RevInfo, error) {
	encRev, err := module.EscapeVersion(rev)
	if err != nil {
		return nil, p.versionError(rev, err)
	}
	data, err := p.getBytes(ctx, "@v/"+encRev+".info")
	if err != nil {
		return nil, p.versionError(rev, err)
	}
	info := new(RevInfo)
	if err := json.Unmarshal(data, info); err != nil {
		return nil, p.versionError(rev, fmt.Errorf("invalid response from proxy %q: %w", p.redactedBase, err))
	}
	if info.Version != rev && rev == module.CanonicalVersion(rev) && module.Check(p.path, rev) == nil {
		// If we request a correct, appropriate version for the module path, the
		// proxy must return either exactly that version or an error — not some
		// arbitrary other version.
		return nil, p.versionError(rev, fmt.Errorf("proxy returned info for version %s instead of requested version", info.Version))
	}
	return info, nil
}

func (p *proxyRepo) Latest(ctx context.Context) (*RevInfo, error) {
	data, err := p.getBytes(ctx, "@latest")
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, p.versionError("", err)
		}
		return p.latest(ctx)
	}
	info := new(RevInfo)
	if err = json.Unmarshal(data, info); err != nil {
		return nil, p.versionError("", fmt.Errorf("invalid response from proxy %q: %w", p.redactedBase, err))
	}
	return info, nil
}

func (p *proxyRepo) GoMod(ctx context.Context, version string) ([]byte, error) {
	if version != module.CanonicalVersion(version) {
		return nil, p.versionError(version, fmt.Errorf("internal error: version passed to GoMod is not canonical"))
	}

	encVer, err := module.EscapeVersion(version)
	if err != nil {
		return nil, p.versionError(version, err)
	}
	data, err := p.getBytes(ctx, "@v/"+encVer+".mod")
	if err != nil {
		return nil, p.versionError(version, err)
	}
	return data, nil
}

func (p *proxyRepo) Zip(ctx context.Context, dst io.Writer, version string) error {
	if version != module.CanonicalVersion(version) {
		return p.versionError(version, fmt.Errorf("internal error: version passed to Zip is not canonical"))
	}

	encVer, err := module.EscapeVersion(version)
	if err != nil {
		return p.versionError(version, err)
	}
	path := fmt.Sprintf("@v/%s.zip", encVer)
	body, redactedURL, err := p.getBody(ctx, path)
	if err != nil {
		return p.versionError(version, err)
	}
	defer body.Close()

	lr := &io.LimitedReader{R: body, N: codehost.MaxZipFile + 1}
	if _, err = io.Copy(dst, lr); err != nil {
		// net/http doesn't add context to Body read errors, so add it here.
		// (See https://go.dev/issue/52727.)
		err = &url.Error{Op: "read", URL: redactedURL, Err: err}
		return p.versionError(version, err)
	}
	if lr.N <= 0 {
		return p.versionError(version, fmt.Errorf("downloaded zip file too large"))
	}
	return nil
}

// pathEscape escapes s, so it can be used in a path.
// That is, it escapes things like ? and # (which really shouldn't appear anyway).
// It does not escape / to %2F: our REST API is designed so that / can be left as is.
func pathEscape(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "%2F", "/")
}
