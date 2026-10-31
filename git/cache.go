package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/richhaase/bigboard/github"
)

const damagedCacheFile = "bigboard-damaged"

const (
	replacedCachePrefix = ".bigboard-replaced-"
	cloneCachePrefix    = ".bigboard-clone-"
	staleTemporaryAge   = 2 * gitTimeout
)

var ErrCacheDamaged = errors.New("repository cache is damaged and will be rebuilt on refresh")

var objectDamagePattern = regexp.MustCompile(`(?i)(unable to|could not|cannot) read( (object|tree|blob|commit|tag))? '?\(?[0-9a-f]{40,64}|(could not|cannot) parse (object|tree|commit|tag) '?\(?[0-9a-f]{40,64}|missing (blob|tree|commit|tag) [0-9a-f]{40,64}|bad( (tree|blob|commit|tag))? object [0-9a-f]{40,64}|inflate: |loose object [0-9a-f]{40,64} .*is corrupt|packfile .* cannot be accessed|failed to traverse parents of commit`)

type RepositoryCache struct {
	Root        string
	TokenSource func(context.Context) (string, error)
	run         func(context.Context, string, ...string) (string, error)
	mu          sync.Mutex
	queries     map[string]*repoQueries
}

type CacheInfo struct {
	Root     string
	Bytes    int64
	Orphans  []Repository
	Problems []string
}

func NewRepositoryCache() (*RepositoryCache, error) {
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(home, ".cache")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("cache directory must be absolute")
	}
	return &RepositoryCache{Root: filepath.Join(root, "bigboard", "repos")}, nil
}

func (c *RepositoryCache) Repository(id, defaultBranch string) (Repository, error) {
	id, err := github.CanonicalRepo(id)
	if err != nil {
		return Repository{}, err
	}
	if c.Root == "" || !filepath.IsAbs(c.Root) {
		return Repository{}, fmt.Errorf("cache directory must be absolute")
	}
	path := filepath.Join(c.Root, "github.com", filepath.FromSlash(id)+".git")
	if err := c.safePath(path); err != nil {
		return Repository{}, err
	}
	return Repository{ID: id, Name: id, Path: path, DefaultBranch: defaultBranch}, nil
}

func (c *RepositoryCache) ScanRepository(ctx context.Context, repo Repository, options CollectOptions) ([]CommitRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	expected, err := c.Repository(repo.ID, repo.DefaultBranch)
	if err != nil {
		return nil, err
	}
	if repo.Path != expected.Path {
		return nil, fmt.Errorf("repository path is outside its owned cache location")
	}
	stamp, err := c.verify(ctx, repo)
	if err != nil {
		return nil, err
	}
	records, err := c.scanCached(ctx, repo, options, stamp)
	if err == nil || ctx.Err() != nil || !isObjectDamage(err) {
		return records, err
	}
	if markErr := c.markDamaged(repo); markErr != nil {
		return nil, fmt.Errorf("%w: %v; record damage: %v", ErrCacheDamaged, err, markErr)
	}
	return nil, fmt.Errorf("%w: %v", ErrCacheDamaged, err)
}

func isObjectDamage(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return objectDamagePattern.MatchString(err.Error())
}

func (c *RepositoryCache) markDamaged(repo Repository) error {
	if err := c.safePath(repo.Path); err != nil {
		return err
	}
	root, err := os.OpenRoot(repo.Path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	owner, err := root.ReadFile("bigboard-cache")
	if err != nil || string(owner) != repo.ID+"\n" {
		return fmt.Errorf("cache ownership is not verified for %s", repo.ID)
	}
	temporary := "." + damagedCacheFile + ".tmp"
	_ = root.Remove(temporary)
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporary) }()
	if _, err := file.WriteString(repo.ID + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Rename(temporary, damagedCacheFile); err != nil {
		return err
	}
	if err := root.Remove(scanCacheFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (c *RepositoryCache) Revision(ctx context.Context, repo Repository) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	expected, err := c.Repository(repo.ID, repo.DefaultBranch)
	if err != nil {
		return "", err
	}
	if repo.Path != expected.Path {
		return "", fmt.Errorf("repository path is outside its owned cache location")
	}
	if _, err := os.Lstat(repo.Path); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	stamp, err := c.verify(ctx, repo)
	if err != nil {
		return "", err
	}
	ref := detectDefaultBranchWith(ctx, repo.Path, c.scanQuery(stamp))
	oid, err := c.cacheQuery(stamp)(ctx, repo.Path, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(oid), nil
}

func (c *RepositoryCache) RefreshIfChanged(ctx context.Context, repo Repository, branch, oid string) error {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if branch == "" || oid == "" {
		return c.Refresh(ctx, repo)
	}
	repo.DefaultBranch = branch
	expected, err := c.Repository(repo.ID, branch)
	if err != nil {
		return err
	}
	if repo.Path != expected.Path {
		return fmt.Errorf("repository path is outside its owned cache location")
	}
	stamp, err := c.verify(ctx, repo)
	if err != nil {
		return c.Refresh(ctx, repo)
	}
	query := c.cacheQuery(stamp)
	head := "refs/heads/" + branch
	remote := "refs/remotes/origin/" + branch
	target, err := query(ctx, repo.Path, "symbolic-ref", "HEAD")
	if err != nil || strings.TrimSpace(target) != head {
		return c.Refresh(ctx, repo)
	}
	refs, err := query(ctx, repo.Path, "for-each-ref", "--format=%(refname) %(symref) %(objecttype) %(objectname)")
	if err != nil {
		return c.Refresh(ctx, repo)
	}
	want := map[string]string{
		head:                       " commit " + oid,
		remote:                     " commit " + oid,
		"refs/remotes/origin/HEAD": remote + " commit " + oid,
	}
	lines := strings.Split(strings.TrimSpace(refs), "\n")
	if len(lines) != len(want) {
		return c.Refresh(ctx, repo)
	}
	for _, line := range lines {
		name, rest, _ := strings.Cut(line, " ")
		if expected, ok := want[name]; !ok || rest != expected {
			return c.Refresh(ctx, repo)
		}
		delete(want, name)
	}
	return ctx.Err()
}

func (c *RepositoryCache) Refresh(ctx context.Context, repo Repository) error {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	expected, err := c.Repository(repo.ID, repo.DefaultBranch)
	if err != nil {
		return err
	}
	if repo.Path != expected.Path {
		return fmt.Errorf("repository path is outside its owned cache location")
	}
	if repo.DefaultBranch == "" || strings.HasPrefix(repo.DefaultBranch, "-") {
		return fmt.Errorf("repository %s has no valid default branch", repo.ID)
	}
	if _, err := c.git(ctx, "", "check-ref-format", "refs/heads/"+repo.DefaultBranch); err != nil {
		return fmt.Errorf("invalid default branch: %w", err)
	}
	if _, err := c.verify(ctx, repo); err == nil {
		return c.fetch(ctx, repo)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(repo.Path), 0700); err != nil {
		return err
	}
	if err := c.safePath(repo.Path); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(filepath.Dir(repo.Path), cloneCachePrefix)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	defer c.forget(temporary)
	replacement := repo
	replacement.Path = temporary
	if _, err := c.git(ctx, temporary, "init", "--bare", "--quiet", "--template=", "--object-format=sha1"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(temporary, "bigboard-cache"), []byte(repo.ID+"\n"), 0600); err != nil {
		return err
	}
	if _, err := c.git(ctx, temporary, "config", "remote.origin.url", "https://github.com/"+repo.ID+".git"); err != nil {
		return err
	}
	if err := c.fetch(ctx, replacement); err != nil {
		return err
	}
	if _, err := c.verify(ctx, replacement); err != nil {
		return fmt.Errorf("verify replacement cache: %w", err)
	}
	if err := c.safePath(repo.Path); err != nil {
		return err
	}
	backup := ""
	if _, err := os.Lstat(repo.Path); err == nil {
		backup, err = os.MkdirTemp(filepath.Dir(repo.Path), replacedCachePrefix)
		if err != nil {
			return err
		}
		backup = filepath.Join(backup, filepath.Base(repo.Path))
		if err := os.Rename(repo.Path, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporary, repo.Path); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, repo.Path); restoreErr != nil {
				return fmt.Errorf("publish cache: %w; restore cache: %v", err, restoreErr)
			}
		}
		return err
	}
	if backup != "" {
		_ = c.removeTemporary(filepath.Dir(backup))
	}
	return nil
}

func (c *RepositoryCache) removeTemporary(path string) error {
	name := filepath.Base(path)
	owner := filepath.Dir(path)
	if !strings.HasPrefix(name, replacedCachePrefix) && !strings.HasPrefix(name, cloneCachePrefix) {
		return fmt.Errorf("not a temporary cache directory: %s", path)
	}
	if filepath.Dir(filepath.Dir(owner)) != filepath.Clean(c.Root) || filepath.Base(filepath.Dir(owner)) != "github.com" {
		return fmt.Errorf("temporary cache directory is outside an owner directory: %s", path)
	}
	if err := c.safePath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("temporary cache entry is not a directory: %s", path)
	}
	root, err := os.OpenRoot(c.Root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	rel, err := filepath.Rel(c.Root, path)
	if err != nil {
		return err
	}
	return root.RemoveAll(rel)
}

func (c *RepositoryCache) fetch(ctx context.Context, repo Repository) error {
	ref := "refs/remotes/origin/" + repo.DefaultBranch
	head := "refs/heads/" + repo.DefaultBranch
	refspec := "+refs/heads/" + repo.DefaultBranch + ":" + ref
	if _, err := c.git(ctx, repo.Path, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "https://github.com/"+repo.ID+".git", refspec); err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	oid, err := c.git(ctx, repo.Path, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return err
	}
	oid = strings.TrimSpace(oid)
	settings, err := c.git(ctx, repo.Path, "config", "--local", "--no-includes", "--null", "--list")
	if err != nil {
		return err
	}
	values := make(map[string][]string)
	for _, entry := range strings.Split(settings, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		values[key] = append(values[key], value)
	}
	refs, err := c.git(ctx, repo.Path, "for-each-ref", "--format=%(refname) %(symref) %(objectname)")
	if err != nil {
		return err
	}
	current := make(map[string]string)
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(refs), "\n") {
		if name, rest, ok := strings.Cut(line, " "); ok {
			current[name] = rest
			names = append(names, name)
		}
	}
	target, err := c.git(ctx, repo.Path, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		target = ""
	}
	same := func(key, value string) bool {
		return len(values[key]) == 1 && values[key][0] == value
	}
	var writes [][]string
	if !same("remote.origin.fetch", refspec) {
		writes = append(writes, []string{"config", "--replace-all", "remote.origin.fetch", refspec})
	}
	if !same("remote.origin.tagopt", "--no-tags") {
		writes = append(writes, []string{"config", "remote.origin.tagOpt", "--no-tags"})
	}
	if rest, ok := current["refs/remotes/origin/HEAD"]; !ok || !strings.HasPrefix(rest, ref+" ") {
		writes = append(writes, []string{"symbolic-ref", "refs/remotes/origin/HEAD", ref})
	}
	if current[head] != " "+oid {
		writes = append(writes, []string{"update-ref", head, oid})
	}
	if strings.TrimSpace(target) != head {
		writes = append(writes, []string{"symbolic-ref", "HEAD", head})
	}
	for _, args := range writes {
		if _, err := c.git(ctx, repo.Path, args...); err != nil {
			return err
		}
	}
	for _, old := range names {
		if old != ref && old != head && old != "refs/remotes/origin/HEAD" {
			if _, err := c.git(ctx, repo.Path, "update-ref", "--no-deref", "-d", old); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *RepositoryCache) verify(ctx context.Context, repo Repository) (repoStamp, error) {
	var none repoStamp
	if err := c.safePath(repo.Path); err != nil {
		return none, err
	}
	stamp, err := repositoryStamp(repo.Path)
	if err != nil {
		return none, err
	}
	marker, err := os.ReadFile(filepath.Join(repo.Path, "bigboard-cache"))
	if err != nil || string(marker) != repo.ID+"\n" {
		return none, fmt.Errorf("cache ownership is not verified for %s", repo.ID)
	}
	for _, path := range []string{"commondir", "gitdir", ".git", "config.worktree", "shallow", "objects/info/alternates", "objects/info/http-alternates"} {
		if _, err := os.Lstat(filepath.Join(repo.Path, filepath.FromSlash(path))); !errors.Is(err, fs.ErrNotExist) {
			return none, fmt.Errorf("cache is not a complete independent clone")
		}
	}
	query := c.cacheQuery(stamp)
	settings, err := query(ctx, repo.Path, "config", "--local", "--no-includes", "--null", "--list")
	if err != nil {
		return none, err
	}
	origin := ""
	for _, entry := range strings.Split(settings, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		if key == "remote.origin.url" {
			if origin != "" {
				return none, fmt.Errorf("cache has multiple origins")
			}
			origin = value
		}
		if strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") || strings.HasPrefix(key, "url.") ||
			strings.HasPrefix(key, "credential.") || strings.HasPrefix(key, "http.") || strings.HasSuffix(key, ".proxy") ||
			key == "core.worktree" || key == "core.sshcommand" || key == "core.gitproxy" ||
			strings.HasPrefix(key, "extensions.") || strings.HasSuffix(key, ".promisor") || strings.HasSuffix(key, ".partialclonefilter") {
			return none, fmt.Errorf("cache has unsupported configuration: %s", key)
		}
	}
	id, err := github.ParseOrigin(origin)
	if err != nil || id != repo.ID {
		return none, fmt.Errorf("cache origin does not match %s", repo.ID)
	}
	bare, err := query(ctx, repo.Path, "rev-parse", "--is-bare-repository")
	if err != nil || strings.TrimSpace(bare) != "true" {
		return none, fmt.Errorf("cache is not a bare repository")
	}
	if _, err := os.Lstat(filepath.Join(repo.Path, damagedCacheFile)); !errors.Is(err, fs.ErrNotExist) {
		return stamp, ErrCacheDamaged
	}
	return stamp, nil
}

func (c *RepositoryCache) CachedRepositories() ([]Repository, error) {
	repos, problems := c.inventory()
	if len(problems) > 0 {
		return repos, errors.New(strings.Join(problems, "; "))
	}
	return repos, nil
}

func (c *RepositoryCache) Info(listed []Repository) (CacheInfo, error) {
	info := CacheInfo{Root: c.Root}
	if err := c.safePath(c.Root); err != nil {
		return info, err
	}
	repos, problems := c.inventory()
	info.Problems = problems
	known := make(map[string]bool, len(listed))
	for _, repo := range listed {
		known[repo.ID] = true
	}
	for _, repo := range repos {
		if !known[repo.ID] {
			info.Orphans = append(info.Orphans, repo)
		}
	}
	err := filepath.WalkDir(c.Root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == c.Root {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			stat, err := entry.Info()
			if err != nil {
				return err
			}
			info.Bytes += stat.Size()
		}
		return nil
	})
	return info, err
}

func (c *RepositoryCache) inventory() ([]Repository, []string) {
	var repos []Repository
	var problems []string
	host := filepath.Join(c.Root, "github.com")
	if err := c.safePath(host); err != nil {
		return nil, []string{err.Error()}
	}
	owners, err := os.ReadDir(host)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	for _, owner := range owners {
		ownerPath := filepath.Join(host, owner.Name())
		if !owner.IsDir() || c.safePath(ownerPath) != nil {
			problems = append(problems, "unrecognized cache entry: "+ownerPath)
			continue
		}
		entries, err := os.ReadDir(ownerPath)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), replacedCachePrefix) || strings.HasPrefix(entry.Name(), cloneCachePrefix) {
				path := filepath.Join(ownerPath, entry.Name())
				info, err := os.Lstat(path)
				if err != nil || !info.IsDir() {
					problems = append(problems, "unrecognized cache entry: "+path)
				} else if time.Since(info.ModTime()) > staleTemporaryAge {
					_ = c.removeTemporary(path)
				}
				continue
			}
			id := owner.Name() + "/" + strings.TrimSuffix(entry.Name(), ".git")
			repo, err := c.Repository(id, "")
			if err != nil || !strings.HasSuffix(entry.Name(), ".git") || !entry.IsDir() || repo.Path != filepath.Join(ownerPath, entry.Name()) {
				problems = append(problems, "unrecognized cache entry: "+filepath.Join(ownerPath, entry.Name()))
				continue
			}
			stamp, err := c.verify(ctx, repo)
			if err != nil && !errors.Is(err, ErrCacheDamaged) {
				problems = append(problems, repo.ID+": "+err.Error())
				continue
			}
			query := c.cacheQuery(stamp)
			branch, err := query(ctx, repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD")
			if err == nil {
				repo.DefaultBranch = strings.TrimPrefix(strings.TrimSpace(branch), "refs/remotes/origin/")
			} else if branch, err := query(ctx, repo.Path, "symbolic-ref", "HEAD"); err == nil {
				repo.DefaultBranch = strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
			}
			repos = append(repos, repo)
		}
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].ID < repos[j].ID })
	return repos, problems
}

func (c *RepositoryCache) Remove(id string) error {
	repo, err := c.Repository(id, "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	if _, err := c.verify(ctx, repo); err != nil && !errors.Is(err, ErrCacheDamaged) {
		return fmt.Errorf("refusing to remove unverified cache: %w", err)
	}
	defer c.forget(repo.Path)
	root, err := os.OpenRoot(c.Root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	path, err := filepath.Rel(c.Root, repo.Path)
	if err != nil {
		return err
	}
	return root.RemoveAll(path)
}

func (c *RepositoryCache) safePath(path string) error {
	if c.Root == "" || !filepath.IsAbs(c.Root) {
		return fmt.Errorf("cache directory must be absolute")
	}
	rel, err := filepath.Rel(c.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path is outside the owned cache")
	}
	current := filepath.Clean(c.Root)
	parts := append([]string{""}, strings.Split(rel, string(filepath.Separator))...)
	for _, part := range parts {
		current = filepath.Join(current, part)
		stat, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if stat.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache path contains a symlink: %s", current)
		}
	}
	return nil
}

func (c *RepositoryCache) git(ctx context.Context, dir string, args ...string) (string, error) {
	if c.run != nil {
		return c.run(ctx, dir, args...)
	}
	if len(args) > 0 && args[0] == "fetch" {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if c.TokenSource == nil {
			return "", github.ErrAuthentication
		}
		token, err := c.TokenSource(ctx)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			for _, known := range []error{context.Canceled, context.DeadlineExceeded, github.ErrUnavailable, github.ErrRateLimited,
				github.ErrAuthentication, github.ErrPermission, github.ErrResponse, github.ErrRequest} {
				if errors.Is(err, known) {
					return "", known
				}
			}
			return "", github.ErrRequest
		}
		if token == "" || strings.ContainsAny(token, "\x00\r\n") {
			return "", github.ErrAuthentication
		}
		return runCacheGitWithToken(ctx, dir, token, args...)
	}
	return runCacheGit(ctx, dir, args...)
}

func runCacheGit(ctx context.Context, dir string, args ...string) (string, error) {
	return runCacheGitWithToken(ctx, dir, "", args...)
}

func runCacheGitWithToken(ctx context.Context, dir, token string, args ...string) (string, error) {
	pinned := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "credential.interactive=false",
		"-c", "credential.helper=", "-c", "http.followRedirects=false", "-c", "http.sslVerify=true", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "fetch.writeCommitGraph=false",
		"-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=never", "-c", "remote.origin.pruneTags=false"}
	if token != "" {
		pinned = append(pinned, "--config-env=http.https://github.com/.extraHeader=BIGBOARD_GIT_AUTHORIZATION")
	}
	if dir != "" {
		pinned = append(pinned, "--git-dir="+dir)
	}
	cmd := gitCommand(ctx, dir, append(pinned, args...)...)
	cmd.Env = nil
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "GIT_") && key != "SSH_ASKPASS" && key != "SSH_ASKPASS_REQUIRE" && key != "BIGBOARD_GIT_AUTHORIZATION" && key != "SSLKEYLOGFILE" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_ASKPASS=", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "GIT_SSH_VARIANT=ssh", "SSH_ASKPASS=", "SSH_ASKPASS_REQUIRE=never")
	if token != "" {
		header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
		cmd.Env = append(cmd.Env, "BIGBOARD_GIT_AUTHORIZATION="+header)
	}
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if token != "" {
			message := strings.ToLower(stderr.String())
			if strings.Contains(message, "repository not found") || strings.Contains(message, "error: 404") {
				return "", github.ErrRepository
			}
			if strings.Contains(message, "authentication failed") || strings.Contains(message, "could not read username") || strings.Contains(message, "error: 401") || strings.Contains(message, "error: 403") {
				return "", github.ErrAuthentication
			}
			return "", fmt.Errorf("git fetch: %w", err)
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	if token != "" {
		return "", nil
	}
	return string(output), nil
}
