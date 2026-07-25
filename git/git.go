package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	fieldSep    = "\x1e"
	coAuthorSep = "\x1f"
)

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
	p := effectivePath(path)
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

func effectivePath(p string) string {
	p = strings.TrimSpace(p)
	if !strings.Contains(p, "=>") {
		return p
	}
	if open := strings.Index(p, "{"); open >= 0 {
		if closeIdx := strings.Index(p, "}"); closeIdx > open {
			inner := p[open+1 : closeIdx]
			if i := strings.Index(inner, "=>"); i >= 0 {
				inner = inner[i+2:]
			}
			return strings.TrimSpace(p[:open] + strings.TrimSpace(inner) + p[closeIdx+1:])
		}
	}
	if i := strings.Index(p, "=>"); i >= 0 {
		return strings.TrimSpace(p[i+2:])
	}
	return p
}

// CommitRecord holds aggregated stats for a single commit.
type CommitRecord struct {
	Author     string
	Email      string
	Date       time.Time
	Added      int
	Removed    int
	Files      int
	RepoID     string
	RepoName   string
	AIAssisted bool
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

// DetectDefaultBranch tries to determine the default branch of the repo at dir.
// It tries origin/HEAD, then main, then master, then falls back to HEAD.
func DetectDefaultBranch(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return detectDefaultBranch(ctx, dir)
}

func detectDefaultBranch(ctx context.Context, dir string) string {
	out, err := runGitContext(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return ref
		}
	}

	for _, branch := range []string{"main", "master"} {
		_, err := runGitContext(ctx, dir, "rev-parse", "--verify", branch)
		if err == nil {
			return branch
		}
	}

	return "HEAD"
}

// CollectCommits runs git log on the repo at dir using the given ref and returns
// one CommitRecord per commit with aggregated numstat data.
func CollectCommits(dir string, ref string) ([]CommitRecord, error) {
	repo := NewRepositories([]string{dir})[0]
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return collectRepository(ctx, repo, ref, legacyPathFilter())
}

// CollectRepository collects commits using explicit repository identity and
// options, avoiding the compatibility globals used by CollectCommits.
func CollectRepository(repo Repository, ref string, options CollectOptions) ([]CommitRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return collectRepository(ctx, repo, ref, defaultPathFilter(options))
}

// ScanRepository detects the default branch and collects its commits under one
// caller-cancelable repository timeout.
func ScanRepository(ctx context.Context, repo Repository, options CollectOptions) ([]CommitRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	ref := detectDefaultBranch(ctx, repo.Path)
	return collectRepository(ctx, repo, ref, defaultPathFilter(options))
}

func collectRepository(ctx context.Context, repo Repository, ref string, filter pathFilter) ([]CommitRecord, error) {
	out, err := runGitContext(ctx, repo.Path, "log", ref, "--no-merges", "-M", "-C",
		"--format=%aN%x1e%aE%x1e%aI%x1e%(trailers:key=Co-authored-by,valueonly,separator=%x1f)", "--numstat")
	if err != nil {
		if isEmptyRepo(ctx, repo.Path) {
			return nil, nil
		}
		return nil, fmt.Errorf("git log failed: %w", err)
	}
	return parseGitLogForRepository(out, repo, filter)
}

// DiscoverReposDepth scans paths for git repositories, descending up to maxDepth
// directory levels below each plain directory (1 = immediate children). Descent
// stops at any git repo and skips dot-directories. Worktrees are skipped;
// results are deduplicated by absolute path.
func DiscoverReposDepth(paths []string, maxDepth int) []string {
	seen := map[string]struct{}{}
	var result []string

	add := func(abs string) {
		if _, ok := seen[abs]; !ok {
			seen[abs] = struct{}{}
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
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			walk(filepath.Join(dir, e.Name()), depth+1)
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

func parseGitLog(output string, repoName string) ([]CommitRecord, error) {
	repo := Repository{ID: repoName, Path: repoName, Name: repoName}
	return parseGitLogForRepository(output, repo, legacyPathFilter())
}

func parseGitLogForRepository(output string, repo Repository, filter pathFilter) ([]CommitRecord, error) {
	var records []CommitRecord
	var current *CommitRecord

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		if strings.Contains(line, fieldSep) {
			parts := strings.SplitN(line, fieldSep, 4)
			if len(parts) >= 3 {
				t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
				if err != nil {
					if current != nil {
						records = append(records, *current)
						current = nil
					}
					continue
				}
				if current != nil {
					records = append(records, *current)
				}
				email := strings.TrimSpace(parts[1])
				aiAssisted := isAIIdentity(email) || (len(parts) == 4 && isAICoAuthor(parts[3]))
				current = &CommitRecord{
					Author:     strings.TrimSpace(parts[0]),
					Email:      email,
					Date:       t,
					RepoID:     repo.ID,
					RepoName:   repo.Name,
					AIAssisted: aiAssisted,
				}
				continue
			}
		}

		if current != nil {
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) == 3 {
				addedStr := fields[0]
				removedStr := fields[1]
				if addedStr == "-" || removedStr == "-" {
					continue
				}
				if !filter.shouldCount(fields[2]) {
					continue
				}
				added, err1 := strconv.Atoi(addedStr)
				removed, err2 := strconv.Atoi(removedStr)
				if err1 != nil || err2 != nil {
					continue
				}
				current.Added += added
				current.Removed += removed
				current.Files++
			}
		}
	}

	if current != nil {
		records = append(records, *current)
	}

	return records, nil
}

var aiEmailDomains = []string{
	"@anthropic.com",
	"@openai.com",
	"@cursor.com",
	"@cursor.sh",
	"@codeium.com",
	"@windsurf.com",
}

var aiEmailAddresses = []string{
	"copilot@github.com",
	"devin@cognition.ai",
	"noreply@aider.chat",
	"bot@codium.ai",
}

func isAIIdentity(value string) bool {
	address := strings.ToLower(strings.TrimSpace(value))
	if parsed, err := mail.ParseAddress(address); err == nil {
		address = strings.ToLower(parsed.Address)
	} else {
		address = strings.Trim(address, "<> ")
	}
	if address == "" {
		return false
	}
	for _, domain := range aiEmailDomains {
		if strings.HasSuffix(address, domain) {
			return true
		}
	}
	for _, addr := range aiEmailAddresses {
		if address == addr {
			return true
		}
	}
	return false
}

func isAICoAuthor(trailerValue string) bool {
	for _, entry := range strings.Split(trailerValue, coAuthorSep) {
		if isAIIdentity(entry) {
			return true
		}
	}
	return false
}

func runGitContext(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-c", "core.quotePath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
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
	if _, err := runGitContext(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return false
	}
	_, err := runGitContext(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD")
	return err != nil
}

func isWorktree(dir string) bool {
	p := filepath.Join(dir, ".git")
	fi, err := os.Lstat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(data), "gitdir:")
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
