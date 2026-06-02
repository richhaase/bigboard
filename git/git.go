package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// fieldSep separates the header fields emitted by git log --format. It is the
// ASCII record-separator (0x1e), which cannot appear in an author name, email,
// or date — unlike '|', which silently corrupted parsing when a name contained
// one. The co-author trailer list uses 0x1f (unit-separator) internally.
const fieldSep = "\x1e"

// gitTimeout bounds a single git invocation so a hung or pathological repo
// (huge history, stuck credential helper, network-mounted .git) cannot block
// the whole concurrent load indefinitely. Generous so legitimate large repos
// still complete; a timeout surfaces as a per-repo failure, not a crash.
const gitTimeout = 120 * time.Second

// Path filtering. When FilterGeneratedPaths is true, churn from generated or
// vendored files (which inflate line counts without representing authored work)
// is not counted. The lists are exported so config/flags can extend or replace
// them. IgnoredDirs matches any path segment; IgnoredFileGlobs matches the
// basename via filepath.Match.
var (
	FilterGeneratedPaths = true

	IgnoredDirs = []string{
		"vendor", "node_modules", "dist", "build", ".next", "target",
		".yarn", ".venv", "__pycache__", "Pods", "Carthage",
	}

	IgnoredFileGlobs = []string{
		"*.min.js", "*.min.css", "*.map",
		"*.snap", "*.lock", "*.pb.go", "*_pb2.py",
		"package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"go.sum", "Cargo.lock", "composer.lock", "Gemfile.lock", "poetry.lock",
	}
)

// shouldCountPath reports whether a numstat path's churn should be counted.
func shouldCountPath(path string) bool {
	if !FilterGeneratedPaths {
		return true
	}
	p := effectivePath(path)
	for _, seg := range strings.Split(p, "/") {
		for _, d := range IgnoredDirs {
			if seg == d {
				return false
			}
		}
	}
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	for _, g := range IgnoredFileGlobs {
		if ok, _ := filepath.Match(g, base); ok {
			return false
		}
	}
	return true
}

// effectivePath resolves the post-change path from a numstat path field, which
// for a rename/copy looks like "old => new", "{old => new}", or
// "pre/{old => new}/post". We only need a representative path to match against.
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
	RepoName   string
	AIAssisted bool
}

// DetectDefaultBranch tries to determine the default branch of the repo at dir.
// It tries origin/HEAD, then main, then master, then falls back to HEAD.
func DetectDefaultBranch(dir string) string {
	// Try origin/HEAD symbolic ref
	out, err := runGit(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		parts := strings.SplitN(strings.TrimSpace(out), "/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
		return strings.TrimSpace(out)
	}

	// Try local branches
	for _, branch := range []string{"main", "master"} {
		_, err := runGit(dir, "rev-parse", "--verify", branch)
		if err == nil {
			return branch
		}
	}

	return "HEAD"
}

// CollectCommits runs git log on the repo at dir using the given ref and returns
// one CommitRecord per commit with aggregated numstat data.
func CollectCommits(dir string, ref string) ([]CommitRecord, error) {
	// -M / -C make rename and copy detection explicit rather than inheriting
	// the user's ambient diff.renames config, so churn counts are reproducible
	// across machines (a pure rename counts as 0 added/0 removed instead of a
	// full delete+add). Fields are 0x1e-separated; co-authors 0x1f-separated.
	out, err := runGit(dir, "log", ref, "--no-merges", "-M", "-C",
		"--format=%aN%x1e%aE%x1e%aI%x1e%(trailers:key=Co-authored-by,valueonly,separator=%x1f)", "--numstat")
	if err != nil {
		if isEmptyRepo(dir) {
			return nil, nil
		}
		return nil, fmt.Errorf("git log failed: %w", err)
	}
	repoName := repoNameFromPath(dir)
	return parseGitLog(out, repoName)
}

// DiscoverRepos takes a list of paths. If a path is a git repo, it's used
// directly. If it's a plain directory, it scans one level deep for git repos.
// Results are deduplicated by absolute path.
func DiscoverRepos(paths []string) []string {
	return DiscoverReposDepth(paths, 1)
}

// DiscoverReposDepth is like DiscoverRepos but scans up to maxDepth directory
// levels below each plain directory (1 = immediate children, the default).
// Descent stops at any git repo and skips dot-directories. Worktrees are
// skipped; results are deduplicated by absolute path.
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
			return // never descend into a repo
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

// --- internal helpers ---

func parseGitLog(output string, repoName string) ([]CommitRecord, error) {
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
					// Unparseable header: finalize the prior commit and clear
					// current so this commit's numstat lines are dropped rather
					// than misattributed to the previous commit.
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
				// AI-assisted if a Co-authored-by trailer names an AI identity,
				// or if the commit's own author IS an AI identity.
				aiAssisted := isAIIdentity(email) || (len(parts) == 4 && isAICoAuthor(parts[3]))
				current = &CommitRecord{
					Author:     strings.TrimSpace(parts[0]),
					Email:      email,
					Date:       t,
					RepoName:   repoName,
					AIAssisted: aiAssisted,
				}
				continue
			}
		}

		// Numstat line: "<added>\t<removed>\t<file>"
		if current != nil {
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) == 3 {
				addedStr := fields[0]
				removedStr := fields[1]
				// Skip binary files (shown as "-")
				if addedStr == "-" || removedStr == "-" {
					continue
				}
				// Skip generated/vendored files so they don't inflate scores.
				if !shouldCountPath(fields[2]) {
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

// aiEmailDomains / aiEmailAddresses are the single source of truth for AI
// authorship detection. Keep entries specific (full domains / exact addresses)
// to avoid false-positiving on human contributors who happen to work at these
// companies — match on the commit-bot identity, not the org.
var aiEmailDomains = []string{
	"@anthropic.com",
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

// isAIIdentity reports whether a single name<email> or bare email string refers
// to a known AI agent. Shared by the author check and the co-author check.
func isAIIdentity(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return false
	}
	for _, domain := range aiEmailDomains {
		if strings.Contains(v, domain) {
			return true
		}
	}
	for _, addr := range aiEmailAddresses {
		if strings.Contains(v, addr) {
			return true
		}
	}
	return false
}

// isAICoAuthor reports whether a Co-authored-by trailer value (one or more
// 0x1f-separated entries) names an AI agent.
func isAICoAuthor(trailerValue string) bool {
	for _, entry := range strings.Split(trailerValue, "\x1f") {
		if isAIIdentity(entry) {
			return true
		}
	}
	return false
}

func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append([]string{"-c", "core.quotePath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git %s timed out after %s", args[0], gitTimeout)
		}
		return "", err
	}
	return string(out), nil
}

func repoNameFromPath(dir string) string {
	abs := absPath(dir)
	return filepath.Base(abs)
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func isEmptyRepo(dir string) bool {
	if _, err := runGit(dir, "rev-parse", "--git-dir"); err != nil {
		return false
	}
	_, err := runGit(dir, "rev-parse", "--verify", "--quiet", "HEAD")
	return err != nil
}

// isWorktree returns true if dir is a git worktree (as opposed to a main repo).
// Worktrees have a .git *file* containing "gitdir: <path>" instead of a .git directory.
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
