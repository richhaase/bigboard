package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const coAuthorSep = "\x1f"

const gitTimeout = 120 * time.Second

var (
	// FilterGeneratedPaths is retained for compatibility with CollectCommits.
	// New code should pass CollectOptions to CollectRepository instead.
	FilterGeneratedPaths = true

	defaultIgnoredDirs = []string{
		"vendor", "node_modules", "dist", "build", ".next", "target",
		".yarn", ".venv", "__pycache__", "Pods", "Carthage",
	}

	defaultIgnoredFileGlobs = []string{
		"*.min.js", "*.min.css", "*.map",
		"*.snap", "*.lock", "*.pb.go", "*_pb2.py",
		"package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"go.sum", "Cargo.lock", "composer.lock", "Gemfile.lock", "poetry.lock",
	}

	// IgnoredDirs and IgnoredFileGlobs are retained for compatibility with
	// CollectCommits. New code should use CollectRepository, whose defaults are
	// immutable from outside this package.
	IgnoredDirs      = append([]string(nil), defaultIgnoredDirs...)
	IgnoredFileGlobs = append([]string(nil), defaultIgnoredFileGlobs...)
)

// CollectOptions controls commit collection without process-wide state.
type CollectOptions struct {
	IncludeGenerated bool
	AIIdentities     []string
}

type pathFilter struct {
	includeGenerated bool
	ignoredDirs      []string
	ignoredFileGlobs []string
}

func defaultPathFilter(options CollectOptions) pathFilter {
	return pathFilter{
		includeGenerated: options.IncludeGenerated,
		ignoredDirs:      defaultIgnoredDirs,
		ignoredFileGlobs: defaultIgnoredFileGlobs,
	}
}

func legacyPathFilter() pathFilter {
	return pathFilter{
		includeGenerated: !FilterGeneratedPaths,
		ignoredDirs:      IgnoredDirs,
		ignoredFileGlobs: IgnoredFileGlobs,
	}
}

func shouldCountPath(path string) bool {
	return legacyPathFilter().shouldCount(path)
}

func (f pathFilter) shouldCount(path string) bool {
	if f.includeGenerated {
		return true
	}
	p := path
	for _, seg := range strings.Split(p, "/") {
		for _, d := range f.ignoredDirs {
			if seg == d {
				return false
			}
		}
	}
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	for _, g := range f.ignoredFileGlobs {
		if ok, _ := filepath.Match(g, base); ok {
			return false
		}
	}
	return true
}

// PathChange retains literal numstat paths, including binary and generated files.
// Numstat cannot distinguish a rename from a copy: PreviousPath records the
// origin only and must not be treated as another touched path or a change status.
type PathChange struct {
	Path         string
	PreviousPath string
	// Generated is true when the current generated-path filter excludes Path.
	Generated bool
}

// CommitRecord holds aggregated stats and changed paths for a single commit.
type CommitRecord struct {
	CommitID string
	// Subject is Git's unescaped commit subject; escape control characters for display.
	Subject    string
	Author     string
	Email      string
	Date       time.Time
	Added      int
	Removed    int
	RepoID     string
	RepoName   string
	AIAssisted bool
	// LinesUnknown identifies shallow boundaries whose parents are unavailable.
	LinesUnknown bool
	Changes      []PathChange
	// PathsUnknown marks missing path evidence at shallow boundaries. An empty
	// Changes slice without this flag is a genuinely empty commit diff.
	PathsUnknown bool
}

// Repository identifies a repository independently from its display name.
// ID is a cleaned absolute path; Name is the shortest unique path suffix among
// the repositories in the current scan.
type Repository struct {
	ID   string
	Path string
	Name string
}

// NewRepositories converts paths into stable repository identities. Duplicate
// paths are removed while preserving first-seen order.
func NewRepositories(paths []string) []Repository {
	repos := make([]Repository, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		id := absPath(path)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		repos = append(repos, Repository{ID: id, Path: id})
	}
	for i := range repos {
		repos[i].Name = shortestUniqueName(repos[i].Path, repos)
	}
	return repos
}

func shortestUniqueName(path string, repos []Repository) string {
	parts := pathParts(path)
	for depth := 1; depth <= len(parts); depth++ {
		candidate := pathSuffix(parts, depth)
		unique := true
		for _, other := range repos {
			if other.Path == path {
				continue
			}
			if pathSuffix(pathParts(other.Path), depth) == candidate {
				unique = false
				break
			}
		}
		if unique {
			return candidate
		}
	}
	return filepath.ToSlash(path)
}

func pathParts(path string) []string {
	clean := filepath.Clean(path)
	clean = strings.TrimPrefix(clean, filepath.VolumeName(clean))
	clean = strings.Trim(clean, string(filepath.Separator))
	if clean == "" {
		return []string{filepath.Base(path)}
	}
	return strings.Split(filepath.ToSlash(clean), "/")
}

func pathSuffix(parts []string, depth int) string {
	if depth > len(parts) {
		depth = len(parts)
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}

// DetectDefaultBranch resolves the available default branch to a commit ID.
// Cached remote history takes precedence over a potentially stale local branch.
func DetectDefaultBranch(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return detectDefaultBranch(ctx, dir)
}

func detectDefaultBranch(ctx context.Context, dir string) string {
	out, err := runGitContext(ctx, dir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		if oid := resolveCommit(ctx, dir, strings.TrimSpace(out)); oid != "" {
			return oid
		}
	}
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master", "HEAD"} {
		if oid := resolveCommit(ctx, dir, ref); oid != "" {
			return oid
		}
	}
	return "HEAD"
}

func resolveCommit(ctx context.Context, dir, ref string) string {
	out, err := runGitContext(ctx, dir, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CollectCommits runs git log on the repo at dir using the given ref and returns
// one CommitRecord per commit with aggregated numstat data.
func CollectCommits(dir string, ref string) ([]CommitRecord, error) {
	repo := NewRepositories([]string{dir})[0]
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return collectRepository(ctx, repo, ref, legacyPathFilter(), newAIMatcher(nil))
}

// CollectRepository collects commits using explicit repository identity and
// options, avoiding the compatibility globals used by CollectCommits.
func CollectRepository(repo Repository, ref string, options CollectOptions) ([]CommitRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return collectRepository(ctx, repo, ref, defaultPathFilter(options), newAIMatcher(options.AIIdentities))
}

// ScanRepository detects the default branch and collects its commits under one
// caller-cancelable repository timeout.
func ScanRepository(ctx context.Context, repo Repository, options CollectOptions) ([]CommitRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	return collectRepository(ctx, repo, "", defaultPathFilter(options), newAIMatcher(options.AIIdentities))
}

func collectRepository(ctx context.Context, repo Repository, ref string, filter pathFilter, ai aiMatcher) ([]CommitRecord, error) {
	partial, err := partialClone(ctx, repo.Path)
	if err != nil {
		return nil, err
	}
	if partial {
		version, err := runGitContext(ctx, repo.Path, "--version")
		if err != nil {
			return nil, err
		}
		var major, minor, patch int
		if _, err := fmt.Sscanf(version, "git version %d.%d.%d", &major, &minor, &patch); err != nil || major < 2 || (major == 2 && (minor < 45 || (minor == 45 && patch < 1))) {
			return nil, fmt.Errorf("partial clone %s requires Git 2.45.1 or newer to scan without fetching", repo.Path)
		}
	}
	if ref == "" {
		ref = detectDefaultBranch(ctx, repo.Path)
	}
	// Public callers may supply a short branch name. Prefer an actual branch
	// before letting Git resolve a tag or another revision expression.
	if !isCommitID(ref) && !strings.HasPrefix(ref, "refs/") && ref != "HEAD" {
		for _, prefix := range []string{"refs/heads/", "refs/remotes/"} {
			if oid := resolveCommit(ctx, repo.Path, prefix+ref); oid != "" {
				ref = oid
				break
			}
		}
	}
	shallow, err := shallowCommits(ctx, repo.Path)
	if err != nil {
		return nil, err
	}
	args := []string{"log", "--no-merges", "--root", "-M50%", "-C50%", "-l0",
		"--no-ext-diff", "--no-textconv", "--no-color", "--no-relative", "--no-show-signature",
		"--diff-algorithm=myers", "--no-indent-heuristic", "--ignore-submodules=none",
		"--format=%x00%H%x00%aN%x00%aE%x00%aI%x00%(trailers:key=Co-authored-by,valueonly,separator=%x1f)%x00%s%x00",
		"--numstat", "-z", ref, "--"}
	cmd := gitCommand(ctx, repo.Path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git log in %s: %w", repo.Path, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git log in %s: %w", repo.Path, err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	scanner.Split(splitNUL)
	records, scanErr := parseLog(scanner, repo, filter, ai)
	if scanErr != nil {
		// A parse failure must stop the producer before waiting for its exit.
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if waitErr != nil || scanErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return nil, fmt.Errorf("git log in %s: %w", repo.Path, context.DeadlineExceeded)
			}
			return nil, fmt.Errorf("git log in %s: %w", repo.Path, ctxErr)
		}
		if isEmptyRepo(ctx, repo.Path) {
			return nil, nil
		}
		cause := waitErr
		if scanErr != nil {
			cause = scanErr
		}
		if partial {
			return nil, fmt.Errorf("partial clone %s could not be scanned with automatic fetching disabled: %w: %s", repo.Path, cause, strings.TrimSpace(stderr.String()))
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("git log failed in %s: %w: %s", repo.Path, cause, detail)
		}
		return nil, fmt.Errorf("git log failed in %s: %w", repo.Path, cause)
	}
	for i := range records {
		if shallow[records[i].CommitID] {
			records[i].LinesUnknown = true
			records[i].PathsUnknown = true
			records[i].Changes = nil
			records[i].Added, records[i].Removed = 0, 0
		}
	}
	return records, nil
}

// DiscoverReposDepth scans paths for git repositories, descending up to maxDepth
// directory levels below each plain directory (1 = immediate children). Descent
// stops at any git repo and skips dot-directories. Worktrees are skipped;
// results are deduplicated by absolute path.
func DiscoverReposDepth(paths []string, maxDepth int) []string {
	seen := map[string]struct{}{}
	var result []string

	add := func(abs string) {
		key := abs
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			key = resolved
		}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			result = append(result, abs)
		}
	}

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if isGitRepo(dir) {
			if !isWorktree(dir) {
				add(dir)
			}
			return
		}
		if depth >= maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			child := filepath.Join(dir, e.Name())
			if !e.IsDir() {
				if e.Type()&os.ModeSymlink == 0 {
					continue
				}
				resolved, err := filepath.EvalSymlinks(child)
				if err != nil {
					continue
				}
				info, err := os.Stat(resolved)
				if err != nil || !info.IsDir() {
					continue
				}
				child = resolved
			}
			walk(child, depth+1)
		}
	}

	for _, p := range paths {
		abs := absPath(p)
		if isGitRepo(abs) {
			if !isWorktree(abs) {
				add(abs)
			}
			continue
		}
		walk(abs, 0)
	}

	return result
}

var aiEmailAddresses = []string{
	"noreply@anthropic.com",
	"noreply@openai.com",
	"hi@cursor.com",
	"hi@cursor.sh",
	"copilot@github.com",
	"devin@cognition.ai",
	"noreply@aider.chat",
	"bot@codium.ai",
}

var aiGitHubUsers = map[string]bool{
	"copilot":              true,
	"copilot-swe-agent":    true,
	"claude":               true,
	"devin-ai-integration": true,
	"google-labs-jules":    true,
	"cursoragent":          true,
}

type aiMatcher struct {
	extra []string
}

func newAIMatcher(entries []string) aiMatcher {
	cleaned := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry != "" {
			cleaned = append(cleaned, entry)
		}
	}
	return aiMatcher{extra: cleaned}
}

func (m aiMatcher) isAI(value string) bool {
	address := normalizeAddress(value)
	if address == "" {
		return false
	}
	for _, entry := range m.extra {
		if strings.HasPrefix(entry, "@") {
			if strings.HasSuffix(address, entry) {
				return true
			}
		} else if address == entry {
			return true
		}
	}
	return isAIAddress(address)
}

func (m aiMatcher) isAICoAuthor(trailerValue string) bool {
	for _, entry := range strings.Split(trailerValue, coAuthorSep) {
		if m.isAI(entry) {
			return true
		}
	}
	return false
}

func normalizeAddress(value string) string {
	address := strings.ToLower(strings.TrimSpace(value))
	if parsed, err := mail.ParseAddress(address); err == nil {
		return strings.ToLower(parsed.Address)
	}
	return strings.Trim(address, "<> ")
}

func isAIAddress(address string) bool {
	for _, addr := range aiEmailAddresses {
		if address == addr {
			return true
		}
	}
	if local, ok := strings.CutSuffix(address, "@users.noreply.github.com"); ok {
		if _, after, found := strings.Cut(local, "+"); found {
			local = after
		}
		local = strings.TrimSuffix(local, "[bot]")
		if aiGitHubUsers[local] {
			return true
		}
	}
	return false
}

func runGitContext(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := gitCommand(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return "", fmt.Errorf("git %s in %s: %w", args[0], dir, context.DeadlineExceeded)
			}
			return "", fmt.Errorf("git %s in %s: %w", args[0], dir, ctxErr)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return "", fmt.Errorf("git %s in %s: %w", args[0], dir, err)
		}
		return "", fmt.Errorf("git %s in %s: %w: %s", args[0], dir, err, detail)
	}
	return string(out), nil
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func isEmptyRepo(ctx context.Context, dir string) bool {
	// An unborn HEAD names a branch that does not exist yet. Failure to
	// resolve HEAD alone is insufficient: corrupt refs fail resolution too.
	_, err := runGitContext(ctx, dir, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return false
	}
	// Unlike show-ref --verify --quiet, listing refs distinguishes malformed
	// refs (an error) from an empty ref inventory (exit status 1).
	_, err = runGitContext(ctx, dir, "show-ref")
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

func isWorktree(dir string) bool {
	p := filepath.Join(dir, ".git")
	fi, err := os.Lstat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	gitDir, err := runGitContext(ctx, dir, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return false
	}
	commonDir, err := runGitContext(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return err == nil && strings.TrimSpace(gitDir) != strings.TrimSpace(commonDir)
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
