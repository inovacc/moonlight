package git

import (
	"net/url"
	"strings"
)

const proxyDefault = "https://proxy.golang.org,direct"

//dir = C:/Users/ddaniels/AppData/Local/Temp/modload-test-1906163017/pkg/mod/cache/vcs/{7d9b3b49b55db5b40e68a94007f21a05905d3fda866f685220de88f9c9bad98a} <- hash url
//hashId = cd70d50baa6daa949efa12e295e10829f3a7bd46
//repoUrl = https://go.googlesource.com/tools
//git init --bare
//git remote add origin -- {repoUrl}
//git config core.longpaths true
//git ls-remote -q origin > list hashes
//git tag -l
//git -c log.showsignature=false log --no-decorate -n1 --format=format:%H %ct %D {hashId} --
//git -c protocol.version=2 fetch -f --depth=1 origin refs/tags/{repoName}/{tags}:refs/tags/{repoName}/{tags}
//git -c log.showsignature=false log --no-decorate -n1 --format=format:%H %ct %D refs/tags/{repoName}/{tags} --
//git cat-file blob {hashId}:{repoName}/{fileName}
//
//
//
//
//
//git -c log.showsignature=false log --no-decorate -n1 --format=format:%H %ct %D cd70d50baa6daa949efa12e295e10829f3a7bd46 --
//
//var pr *proxyRepo
//
//type proxyRepo struct {
//	url          *url.URL // The combined module proxy URL joined with the module path.
//	path         string   // The module path (unescaped).
//	redactedBase string   // The base module proxy URL in [url.URL.Redacted] form.
//
//	listLatestOnce sync.Once
//	listLatest     *RevInfo
//	listLatestErr  error
//}
//
//type RevInfo struct {
//	Version string    // suggested version string for this revision
//	Time    time.Time // commit time
//
//	// These fields are used for Stat of arbitrary rev,
//	// but they are not recorded when talking about module versions.
//	Name  string `json:"-"` // complete ID in underlying repository
//	Short string `json:"-"` // shortened ID, for use in pseudo-version
//
//	Origin *codehost.Origin `json:",omitempty"` // provenance for reuse
//}
//
//type Origin struct {
//	VCS    string `json:"vcs,omitempty"`     // "git" etc
//	URL    string `json:"url,omitempty"`     // URL of repository
//	Subdir string `json:"sub_dir,omitempty"` // subdirectory in repo
//
//	Hash string `json:"hash,omitempty"` // commit hash or ID
//
//	// If TagSum is non-empty, then the resolution of this module version
//	// depends on the set of tags present in the repo, specifically the tags
//	// of the form TagPrefix + a valid semver version.
//	// If the matching repo tags and their commit hashes still hash to TagSum,
//	// the Origin is still valid (at least as far as the tags are concerned).
//	// The exact checksum is up to the Repo implementation; see (*gitRepo).Tags.
//	TagPrefix string `json:"tag_prefix,omitempty"`
//	TagSum    string `json:"tag_sum,omitempty"`
//
//	// If Ref is non-empty, then the resolution of this module version
//	// depends on Ref resolving to the revision identified by Hash.
//	// If Ref still resolves to Hash, the Origin is still valid (at least as far as Ref is concerned).
//	// For Git, the Ref is a full ref like "refs/heads/main" or "refs/tags/v1.2.3",
//	// and the Hash is the Git object hash the ref maps to.
//	// Other VCS might choose differently, but the idea is that Ref is the name
//	// with a mutable meaning while Hash is a name with an immutable meaning.
//	Ref string `json:"ref,omitempty"`
//
//	// If RepoSum is non-empty, then the resolution of this module version
//	// failed due to the repo being available but the version not being present.
//	// This depends on the entire state of the repo, which RepoSum summarizes.
//	// For Git, this is a hash of all the refs and their hashes.
//	RepoSum string `json:"repo_sum,omitempty"`
//}
//
//type RepoGit struct {
//	*git.Repository
//	*git.CloneOptions
//	dirHash string
//}
//
//func NewRepoGit(ctx context.Context, repoUrl string) (*RepoGit, error) {
//	if err := module.CheckPath(repoUrl); err != nil {
//		return nil, err
//	}
//
//	proxy := proxyDefault
//	if i := strings.IndexAny(proxy, ",|"); i >= 0 {
//		proxy = proxy[:i]
//	}
//
//	base, err := url.Parse(proxy)
//	if err != nil {
//		return nil, err
//	}
//
//	u := base
//	enc, err := module.EscapePath(repoUrl)
//	if err != nil {
//		return nil, err
//	}
//
//	u.Path = fmt.Sprintf("%s/%s", strings.TrimSuffix(base.Path, "/"), enc)
//	u.RawPath = fmt.Sprintf("%s/%s", strings.TrimSuffix(base.RawPath, "/"), pathEscape(enc))
//
//	redactedBase := base.Redacted()
//
//	pr = &proxyRepo{
//		url:            u,
//		path:           repoUrl,
//		redactedBase:   redactedBase,
//		listLatestOnce: sync.Once{},
//		listLatest:     nil,
//		listLatestErr:  nil,
//	}
//
//	repo = modfetch.Lookup(ctx, proxy, repoUrl)
//
//	u, err := url.Parse(repoUrl)
//	if err != nil {
//		return nil, err
//	}
//
//	u.RawQuery = "go-get=1"
//
//	key := fmt.Sprintf("git3:%s://%s%s", u.Scheme, u.Host, u.Path)
//
//	return &RepoGit{
//		CloneOptions: &git.CloneOptions{URL: u.String()},
//		dirHash:      fmt.Sprintf("%x", sha256.Sum256([]byte(key))),
//	}, nil
//}
//
//func (r *RepoGit) SetDepth(depth int) {
//	r.CloneOptions.Depth = depth
//}
//
//func (r *RepoGit) Memory() error {
//	var err error
//	r.Repository, err = git.Clone(memory.NewStorage(), nil, r.CloneOptions)
//	return err
//}
//
//func (r *RepoGit) Storage(path string) error {
//	var err error
//	fs := filesystem.NewStorage(osfs.New(path), cache.NewObjectLRUDefault())
//	r.Repository, err = git.Clone(fs, nil, r.CloneOptions)
//	return err
//}
//
//func (r *RepoGit) Init(path string, isBare bool) error {
//	var err error
//	r.Repository, err = git.PlainInit(path, isBare)
//	return err
//}

func pathEscape(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "%2F", "/")
}

//func main() {
//	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
//		URL: "https://github.com/src-d/go-siva",
//	})
//	if err != nil {
//		panic(err)
//	}
//
//	ref, err := r.Head()
//	if err != nil {
//		panic(err)
//	}
//
//	since := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
//	until := time.Date(2019, 7, 30, 0, 0, 0, 0, time.UTC)
//
//	cIter, err := r.Log(&git.LogOptions{From: ref.Hash(), Since: &since, Until: &until})
//	if err != nil {
//		panic(err)
//	}
//
//	ob := func(c *object.Commit) error {
//		fmt.Println(c)
//		return nil
//	}
//
//	if err = cIter.ForEach(ob); err != nil {
//		panic(err)
//	}
//}
