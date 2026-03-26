package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"os"
)

// CommitRecord holds aggregated stats for a single commit.
type CommitRecord struct {
	Author   string
	Date     time.Time
	Added    int
	Removed  int
	Files    int
	RepoName string
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
	out, err := runGit(dir, "log", ref, "--no-merges", "--format=%aN|%aI", "--numstat")
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}
	repoName := repoNameFromPath(dir)
	return parseGitLog(out, repoName)
}

// DiscoverRepos takes a list of paths. If a path is a git repo, it's used
// directly. If it's a plain directory, it scans one level deep for git repos.
// Results are deduplicated by absolute path.
func DiscoverRepos(paths []string) []string {
	seen := map[string]struct{}{}
	var result []string

	add := func(p string) {
		abs := absPath(p)
		if _, ok := seen[abs]; !ok {
			seen[abs] = struct{}{}
			result = append(result, abs)
		}
	}

	for _, p := range paths {
		abs := absPath(p)
		if isGitRepo(abs) {
			add(abs)
			continue
		}
		// Scan one level deep
		entries, err := os.ReadDir(abs)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			candidate := filepath.Join(abs, e.Name())
			if isGitRepo(candidate) {
				add(candidate)
			}
		}
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

		// Header line: "Author Name|2024-01-01T12:00:00+00:00"
		if strings.Contains(line, "|") && !strings.HasPrefix(line, "\t") {
			// Could be a numstat line with a tab-separated format — numstat lines
			// are "<added>\t<removed>\t<file>", so they contain tabs, not pipes.
			// A header line has the form "name|date".
			parts := strings.SplitN(line, "|", 2)
			if len(parts) == 2 {
				t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
				if err != nil {
					// Not a valid date line — skip
					continue
				}
				if current != nil {
					records = append(records, *current)
				}
				current = &CommitRecord{
					Author:   strings.TrimSpace(parts[0]),
					Date:     t,
					RepoName: repoName,
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

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
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

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
