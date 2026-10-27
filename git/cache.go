package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/richhaase/bigboard/github"
)

type RepositoryCache struct {
	Root string
	run  func(context.Context, string, ...string) (string, error)
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
	if err := c.verify(ctx, repo, false); err != nil {
		return nil, err
	}
	return c.scanCached(ctx, repo, options)
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
	if err := c.verify(ctx, repo, false); err != nil {
		return "", err
	}
	ref := detectDefaultBranch(ctx, repo.Path)
	oid, err := c.git(ctx, repo.Path, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
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
	if err := c.verify(ctx, repo, true); err != nil {
		return c.Refresh(ctx, repo)
	}
	for _, ref := range []struct{ name, target string }{
		{"refs/remotes/origin/HEAD", "refs/remotes/origin/" + branch},
		{"HEAD", "refs/heads/" + branch},
	} {
		target, err := c.git(ctx, repo.Path, "symbolic-ref", ref.name)
		if err != nil || strings.TrimSpace(target) != ref.target {
			return c.Refresh(ctx, repo)
		}
		current, err := c.git(ctx, repo.Path, "rev-parse", "--verify", "--end-of-options", ref.target+"^{commit}")
		if err != nil || strings.TrimSpace(current) != oid {
			return c.Refresh(ctx, repo)
		}
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
	if err := c.verify(ctx, repo, true); err == nil {
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
	temporary, err := os.MkdirTemp(filepath.Dir(repo.Path), ".bigboard-clone-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temporary) }()
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
	if err := c.verify(ctx, replacement, true); err != nil {
		return fmt.Errorf("verify replacement cache: %w", err)
	}
	if err := c.safePath(repo.Path); err != nil {
		return err
	}
	backup := ""
	if _, err := os.Lstat(repo.Path); err == nil {
		backup, err = os.MkdirTemp(filepath.Dir(repo.Path), ".bigboard-replaced-")
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
	return nil
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
	for _, args := range [][]string{
		{"config", "--replace-all", "remote.origin.fetch", refspec},
		{"config", "remote.origin.tagOpt", "--no-tags"},
		{"symbolic-ref", "refs/remotes/origin/HEAD", ref},
		{"update-ref", head, strings.TrimSpace(oid)},
		{"symbolic-ref", "HEAD", head},
	} {
		if _, err := c.git(ctx, repo.Path, args...); err != nil {
			return err
		}
	}
	refs, err := c.git(ctx, repo.Path, "for-each-ref", "--format=%(refname)")
	if err != nil {
		return err
	}
	for _, old := range strings.Fields(refs) {
		if old != ref && old != head && old != "refs/remotes/origin/HEAD" {
			if _, err := c.git(ctx, repo.Path, "update-ref", "--no-deref", "-d", old); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *RepositoryCache) verify(ctx context.Context, repo Repository, full bool) error {
	if err := c.safePath(repo.Path); err != nil {
		return err
	}
	if err := filepath.WalkDir(repo.Path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache contains a symlink: %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	marker, err := os.ReadFile(filepath.Join(repo.Path, "bigboard-cache"))
	if err != nil || string(marker) != repo.ID+"\n" {
		return fmt.Errorf("cache ownership is not verified for %s", repo.ID)
	}
	for _, path := range []string{"commondir", "gitdir", ".git", "config.worktree", "shallow", "objects/info/alternates", "objects/info/http-alternates"} {
		if _, err := os.Lstat(filepath.Join(repo.Path, filepath.FromSlash(path))); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cache is not a complete independent clone")
		}
	}
	settings, err := c.git(ctx, repo.Path, "config", "--local", "--no-includes", "--null", "--list")
	if err != nil {
		return err
	}
	origin := ""
	for _, entry := range strings.Split(settings, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		key = strings.ToLower(key)
		if key == "remote.origin.url" {
			if origin != "" {
				return fmt.Errorf("cache has multiple origins")
			}
			origin = value
		}
		if strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") || strings.HasPrefix(key, "url.") ||
			strings.HasPrefix(key, "credential.") || strings.HasPrefix(key, "http.") || strings.HasSuffix(key, ".proxy") ||
			key == "core.worktree" || key == "core.sshcommand" || key == "core.gitproxy" ||
			strings.HasPrefix(key, "extensions.") || strings.HasSuffix(key, ".promisor") || strings.HasSuffix(key, ".partialclonefilter") {
			return fmt.Errorf("cache has unsupported configuration: %s", key)
		}
	}
	id, err := github.ParseOrigin(origin)
	if err != nil || id != repo.ID {
		return fmt.Errorf("cache origin does not match %s", repo.ID)
	}
	bare, err := c.git(ctx, repo.Path, "rev-parse", "--is-bare-repository")
	if err != nil || strings.TrimSpace(bare) != "true" {
		return fmt.Errorf("cache is not a bare repository")
	}
	if full {
		if _, err := c.git(ctx, repo.Path, "fsck", "--connectivity-only", "--no-reflogs"); err != nil {
			return fmt.Errorf("cache history is incomplete: %w", err)
		}
	}
	return nil
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
			id := owner.Name() + "/" + strings.TrimSuffix(entry.Name(), ".git")
			repo, err := c.Repository(id, "")
			if err != nil || !strings.HasSuffix(entry.Name(), ".git") || !entry.IsDir() || repo.Path != filepath.Join(ownerPath, entry.Name()) {
				problems = append(problems, "unrecognized cache entry: "+filepath.Join(ownerPath, entry.Name()))
				continue
			}
			if err := c.verify(ctx, repo, false); err != nil {
				problems = append(problems, repo.ID+": "+err.Error())
				continue
			}
			branch, err := c.git(ctx, repo.Path, "symbolic-ref", "refs/remotes/origin/HEAD")
			if err == nil {
				repo.DefaultBranch = strings.TrimPrefix(strings.TrimSpace(branch), "refs/remotes/origin/")
			} else if branch, err := c.git(ctx, repo.Path, "symbolic-ref", "HEAD"); err == nil {
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
	if err := c.verify(ctx, repo, false); err != nil {
		return fmt.Errorf("refusing to remove unverified cache: %w", err)
	}
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
	return runCacheGit(ctx, dir, args...)
}

func runCacheGit(ctx context.Context, dir string, args ...string) (string, error) {
	pinned := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "credential.interactive=false",
		"-c", "credential.helper=!gh auth git-credential", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "fetch.writeCommitGraph=false",
		"-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=never", "-c", "remote.origin.pruneTags=false"}
	if dir != "" {
		pinned = append(pinned, "--git-dir="+dir)
	}
	cmd := gitCommand(ctx, dir, append(pinned, args...)...)
	cmd.Env = nil
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "GIT_") && key != "SSH_ASKPASS" && key != "SSH_ASKPASS_REQUIRE" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		"GIT_ASKPASS=", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "GIT_SSH_VARIANT=ssh", "SSH_ASKPASS=", "SSH_ASKPASS_REQUIRE=never")
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(output), nil
}
