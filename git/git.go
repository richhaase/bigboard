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
			local := strings.TrimPrefix(ref, "origin/")
			if local != ref {
				_, localErr := runGitContext(ctx, dir, "rev-parse", "--verify", "refs/heads/"+local)
				if localErr == nil {
					return local
				}
			}
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
	ref := detectDefaultBranch(ctx, repo.Path)
	return collectRepository(ctx, repo, ref, defaultPathFilter(options), newAIMatcher(options.AIIdentities))
}

func collectRepository(ctx context.Context, repo Repository, ref string, filter pathFilter, ai aiMatcher) ([]CommitRecord, error) {
	args := []string{"-c", "core.quotePath=false", "log", ref, "--no-merges", "-M", "-C",
		"--format=%aN%x1e%aE%x1e%aI%x1e%(trailers:key=Co-authored-by,valueonly,separator=%x1f)", "--numstat"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo.Path
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git log in %s: %w", repo.Path, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git log in %s: %w", repo.Path, err)
	}
	parser := newLogParser(repo, filter, ai)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		parser.feed(scanner.Text())
	}
	scanErr := scanner.Err()
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
		if cause == nil {
			cause = scanErr
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("git log failed in %s: %w: %s", repo.Path, cause, detail)
		}
		return nil, fmt.Errorf("git log failed in %s: %w", repo.Path, cause)
	}
	return parser.finish(), nil
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

func parseGitLog(output string, repoName string) ([]CommitRecord, error) {
	repo := Repository{ID: repoName, Path: repoName, Name: repoName}
	return parseGitLogForRepository(output, repo, legacyPathFilter(), newAIMatcher(nil))
}

func parseGitLogForRepository(output string, repo Repository, filter pathFilter, ai aiMatcher) ([]CommitRecord, error) {
	parser := newLogParser(repo, filter, ai)
	for _, line := range strings.Split(output, "\n") {
		parser.feed(line)
	}
	return parser.finish(), nil
}

type logParser struct {
	repo    Repository
	filter  pathFilter
	ai      aiMatcher
	records []CommitRecord
	current *CommitRecord
}

func newLogParser(repo Repository, filter pathFilter, ai aiMatcher) *logParser {
	return &logParser{repo: repo, filter: filter, ai: ai}
}

func (p *logParser) flush() {
	if p.current != nil {
		p.records = append(p.records, *p.current)
		p.current = nil
	}
}

func (p *logParser) feed(line string) {
	if line == "" {
		return
	}

	if strings.Contains(line, fieldSep) {
		parts := strings.SplitN(line, fieldSep, 4)
		if len(parts) >= 3 {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
			if err != nil {
				p.flush()
				return
			}
			p.flush()
			email := strings.TrimSpace(parts[1])
			aiAssisted := p.ai.isAI(email) || (len(parts) == 4 && p.ai.isAICoAuthor(parts[3]))
			p.current = &CommitRecord{
				Author:     strings.TrimSpace(parts[0]),
				Email:      email,
				Date:       t,
				RepoID:     p.repo.ID,
				RepoName:   p.repo.Name,
				AIAssisted: aiAssisted,
			}
			return
		}
	}

	if p.current == nil {
		return
	}
	fields := strings.SplitN(line, "\t", 3)
	if len(fields) != 3 {
		return
	}
	addedStr := fields[0]
	removedStr := fields[1]
	if addedStr == "-" || removedStr == "-" {
		return
	}
	if !p.filter.shouldCount(fields[2]) {
		return
	}
	added, err1 := strconv.Atoi(addedStr)
	removed, err2 := strconv.Atoi(removedStr)
	if err1 != nil || err2 != nil {
		return
	}
	p.current.Added += added
	p.current.Removed += removed
}

func (p *logParser) finish() []CommitRecord {
	p.flush()
	return p.records
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
