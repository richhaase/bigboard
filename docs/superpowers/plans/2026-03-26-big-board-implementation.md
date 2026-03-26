# Big Board Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a cyberpunk-themed TUI that analyzes git repos and displays contributor statistics with interactive drill-down, time filtering, and sorting.

**Architecture:** CLI discovers git repos → `git/` package extracts commit data concurrently → `stats/` aggregates and filters → `tui/` renders interactive Bubble Tea views with Lip Gloss cyberpunk styling.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, Bubbles (Charm.sh), os/exec for git commands.

**Spec:** `docs/superpowers/specs/2026-03-26-big-board-design.md`

---

### Task 1: Project Scaffolding & Build Automation

**Files:**
- Create: `.gitignore`
- Create: `Makefile`
- Create: `.golangci.yml`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`
- Create: `.github/workflows/security.yml`
- Create: `.github/dependabot.yml`
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Create .gitignore**

```gitignore
# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib
*.test

# Coverage
*.out
coverage.*
*.coverprofile
profile.cov

# Go workspace
go.work
go.work.sum

# Env
.env

# Build output
bin/

# Editor
.claude/settings.local.json
```

- [ ] **Step 2: Create Makefile**

```makefile
# Big Board development tasks

.PHONY: help build test test-coverage fmt lint vet tidy clean staticcheck check

help:
	@echo "Available targets:"
	@echo "  build        - Build the bigboard binary with version information"
	@echo "  test         - Run all unit tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  fmt          - Format Go source code"
	@echo "  lint         - Run golangci-lint v2"
	@echo "  vet          - Run go vet"
	@echo "  tidy         - Tidy go modules"
	@echo "  clean        - Clean build artifacts and test cache"
	@echo "  staticcheck  - Run staticcheck"
	@echo "  check        - Run all quality checks (fmt, lint, vet, staticcheck, tests)"

build:
	@echo "Building bigboard with version information..."
	@mkdir -p bin
	@VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo "dev"); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "none"); \
	DATE=$$(date -u +"%Y-%m-%dT%H:%M:%SZ"); \
	if ! go build -ldflags "-X main.version=$$VERSION -X main.commit=$$COMMIT -X main.date=$$DATE" -o bin/bigboard ./cmd/bigboard; then \
		echo "Build failed"; \
		exit 1; \
	fi; \
	echo "Built bigboard binary to bin/ (version: $$VERSION)"

test:
	@echo "Running unit tests..."
	@go test ./...
	@echo "Unit tests passed!"

test-coverage:
	@echo "Running unit tests with coverage..."
	@go clean -testcache
	@go test -covermode=atomic -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out > coverage.txt
	@awk 'END{printf "Total coverage: %s\n", $$3}' coverage.txt
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Unit tests passed! Coverage report: coverage.html (see also coverage.txt)"

fmt:
	@echo "Formatting Go source code..."
	@go fmt ./...
	@echo "Formatting complete!"

lint:
	@echo "Running golangci-lint v2..."
	@go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0 run --timeout=10m ./...
	@echo "Linting passed!"

vet:
	@echo "Running go vet..."
	@go vet ./...
	@echo "Vet passed!"

tidy:
	@echo "Tidying go modules..."
	@go mod tidy
	@echo "Modules tidied!"

clean:
	@echo "Cleaning build artifacts and caches..."
	@rm -rf bin
	@rm -f coverage.out coverage.html coverage.txt
	@go clean
	@go clean -testcache
	@echo "Build artifacts and test cache cleaned"

staticcheck:
	@echo "Running staticcheck..."
	@go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	@echo "Staticcheck passed!"

check: fmt lint vet staticcheck test
```

- [ ] **Step 3: Create .golangci.yml**

```yaml
version: "2"

run:
  timeout: 10m
  tests: true

linters:
  default: none
  enable:
    - govet
    - ineffassign
    - misspell
    - unused
    - errcheck
    - staticcheck
    - gosec
    - nolintlint

  exclusions:
    presets:
      - std-error-handling
      - common-false-positives
    rules:
      - path: _test\.go
        linters:
          - errcheck
          - gosec

  settings:
    misspell:
      locale: US
    errcheck:
      exclude-functions:
        - (io.Closer).Close
        - (*os.File).Close
        - fmt.Fprint
        - fmt.Fprintf
        - fmt.Fprintln
        - fmt.Print
        - fmt.Println
        - fmt.Printf
    gosec:
      excludes:
        - G104
        - G204  # subprocess execution - needed for git commands
        - G304  # file path input - needed for repo path args
    nolintlint:
      require-explanation: true
      require-specific: true

output:
  formats:
    text:
      print-linter-name: true

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
```

- [ ] **Step 4: Create .github/workflows/ci.yml**

```yaml
name: CI

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  fmt:
    name: Format Check
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Check formatting
      run: |
        unformatted=$(gofmt -l .)
        if [ -n "$unformatted" ]; then
          echo "The following files are not formatted:"
          echo "$unformatted"
          exit 1
        fi

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Run tests with coverage
      run: go test -coverprofile=coverage.out ./...
    - name: Upload coverage
      uses: actions/upload-artifact@v7
      with:
        name: coverage
        path: coverage.out

  test-race:
    name: Test (Race Detection)
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Run tests with race detector
      run: go test -race ./...

  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Run lint
      run: make lint

  staticcheck:
    name: Staticcheck
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Run staticcheck
      run: make staticcheck

  vet:
    name: Vet
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Run vet
      run: make vet

  govulncheck:
    name: Vulnerability Check
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Run govulncheck
      run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

- [ ] **Step 5: Create .github/workflows/release.yml**

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v6
        with:
          fetch-depth: 0
      - name: Setup Go
        uses: actions/setup-go@v6
        with:
          go-version-file: 'go.mod'
      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
          QUILL_SIGN_P12: ${{ secrets.QUILL_SIGN_P12 }}
          QUILL_SIGN_PASSWORD: ${{ secrets.QUILL_SIGN_PASSWORD }}
          QUILL_NOTARY_KEY: ${{ secrets.QUILL_NOTARY_KEY }}
          QUILL_NOTARY_KEY_ID: ${{ secrets.QUILL_NOTARY_KEY_ID }}
          QUILL_NOTARY_ISSUER: ${{ secrets.QUILL_NOTARY_ISSUER }}
```

- [ ] **Step 6: Create .github/workflows/security.yml**

```yaml
name: Security

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]
  schedule:
    - cron: '0 2 * * *'
  workflow_dispatch:

jobs:
  security:
    name: Security Scan
    runs-on: ubuntu-latest
    steps:
    - name: Checkout code
      uses: actions/checkout@v6
    - name: Setup Go
      uses: actions/setup-go@v6
      with:
        go-version-file: 'go.mod'
    - name: Run govulncheck
      run: |
        echo "Checking for vulnerabilities in dependencies..."
        go run golang.org/x/vuln/cmd/govulncheck@latest ./...
    - name: Run gosec
      run: |
        echo "Running security analysis..."
        go run github.com/securego/gosec/v2/cmd/gosec@latest -exclude=G104,G204,G304 ./...
      continue-on-error: ${{ github.event_name == 'schedule' }}
    - name: Summary
      if: always()
      run: echo "Security checks completed. Review any findings above."
```

- [ ] **Step 7: Create .github/dependabot.yml**

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule:
      interval: weekly
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
```

- [ ] **Step 8: Create .goreleaser.yaml**

```yaml
version: 2

before:
  hooks:
    - go mod tidy
    - go test ./...

builds:
  - id: bigboard
    main: ./cmd/bigboard
    binary: bigboard
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}
      - -X main.builtBy=goreleaser

archives:
  - id: bigboard-archive
    name_template: >-
      {{ .ProjectName }}_
      {{- .Version }}_
      {{- title .Os }}_
      {{- if eq .Arch "amd64" }}x86_64
      {{- else if eq .Arch "386" }}i386
      {{- else }}{{ .Arch }}{{ end }}
    formats:
      - tar.gz
    files:
      - README.md
      - LICENSE

checksum:
  name_template: 'checksums.txt'
  algorithm: sha256

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  use: github
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^chore:'
      - '^style:'
      - '^refactor:'
      - '^perf:'
      - '^ci:'
  groups:
    - title: '🚀 Features'
      regexp: '^feat'
    - title: '🐛 Bug Fixes'
      regexp: '^fix'
    - title: '🔒 Security'
      regexp: '^sec'
    - title: '⚠️ Breaking Changes'
      regexp: '^.*!:'
    - title: 'Other Changes'

release:
  github:
    owner: richhaase
    name: bigboard
  draft: false
  prerelease: auto
  name_template: "{{.ProjectName}} v{{.Version}}"
  header: |
    ## Big Board v{{.Version}}

    Cyberpunk-themed TUI for assessing contributor volumes across git repositories.
  footer: |
    ---

    **Full Changelog**: https://github.com/richhaase/bigboard/compare/{{ .PreviousTag }}...{{ .Tag }}

    ## Installation

    ### Homebrew (Recommended)
    ```bash
    brew install --cask richhaase/tap/bigboard
    ```

    ### Direct Download
    Download the appropriate archive from the assets below and extract the `bigboard` binary.

    ### From Source
    ```bash
    go install github.com/richhaase/bigboard/cmd/bigboard@v{{ .Version }}
    ```

homebrew_casks:
  - name: bigboard
    repository:
      owner: richhaase
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    homepage: "https://github.com/richhaase/bigboard"
    description: "Cyberpunk-themed TUI for assessing contributor volumes across git repositories"
    license: "MIT"
    commit_author:
      name: goreleaserbot
      email: bot@goreleaser.com
    commit_msg_template: "Cask update for {{ .ProjectName }} version {{ .Tag }}"
    caveats: |
      Big Board has been installed! Get started with:
        bigboard --help
        bigboard ~/src/

      Documentation: https://github.com/richhaase/bigboard#readme

notarize:
  macos:
    - enabled: '{{ isEnvSet "QUILL_SIGN_P12" }}'
      ids:
        - bigboard
      sign:
        certificate: "{{.Env.QUILL_SIGN_P12}}"
        password: "{{.Env.QUILL_SIGN_PASSWORD}}"
      notarize:
        issuer_id: "{{.Env.QUILL_NOTARY_ISSUER}}"
        key_id: "{{.Env.QUILL_NOTARY_KEY_ID}}"
        key: "{{.Env.QUILL_NOTARY_KEY}}"
        wait: true
        timeout: 20m

announce:
  skip: true
```

- [ ] **Step 9: Commit scaffolding**

```bash
git add .gitignore Makefile .golangci.yml .goreleaser.yaml .github/
git commit -m "feat: add project scaffolding, CI, and release automation"
```

---

### Task 2: Git Data Collection Package

**Files:**
- Create: `git/git.go`
- Create: `git/git_test.go`

- [ ] **Step 1: Write tests for branch detection and git log parsing**

```go
// git/git_test.go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// createTestRepo creates a temporary git repo with some commits for testing.
func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v failed: %s %v", args, out, err)
		}
	}

	// Create a file and commit it
	if err := os.WriteFile(filepath.Join(dir, "file1.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial commit"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit %v failed: %s %v", args, out, err)
		}
	}

	// Add more content in a second commit
	if err := os.WriteFile(filepath.Join(dir, "file2.go"), []byte("package util\n\nfunc Helper() string {\n\treturn \"hello\"\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "add helper"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit %v failed: %s %v", args, out, err)
		}
	}

	return dir
}

func TestDetectDefaultBranch(t *testing.T) {
	dir := createTestRepo(t)
	branch := DetectDefaultBranch(dir)
	// git init defaults to "main" or "master" depending on config
	if branch != "main" && branch != "master" {
		t.Errorf("expected main or master, got %q", branch)
	}
}

func TestCollectCommits(t *testing.T) {
	dir := createTestRepo(t)
	branch := DetectDefaultBranch(dir)

	records, err := CollectCommits(dir, branch)
	if err != nil {
		t.Fatalf("CollectCommits failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 commit records, got %d", len(records))
	}

	// All records should be from "Test User"
	for _, r := range records {
		if r.Author != "Test User" {
			t.Errorf("expected author 'Test User', got %q", r.Author)
		}
	}

	// Check that dates are reasonable (within last minute)
	for _, r := range records {
		if time.Since(r.Date) > time.Minute {
			t.Errorf("commit date %v is too old", r.Date)
		}
	}

	// First commit: 1 file (file1.go), 3 lines added
	// Second commit: 1 file (file2.go), 5 lines added
	totalAdded := 0
	totalFiles := 0
	for _, r := range records {
		totalAdded += r.Added
		totalFiles += r.Files
	}
	if totalAdded != 8 {
		t.Errorf("expected 8 total lines added, got %d", totalAdded)
	}
	if totalFiles != 2 {
		t.Errorf("expected 2 total files, got %d", totalFiles)
	}
}

func TestCollectCommitsInvalidRepo(t *testing.T) {
	_, err := CollectCommits(t.TempDir(), "main")
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/rdh/src/bigboard && go test ./git/...`
Expected: FAIL — package `git` doesn't exist yet.

- [ ] **Step 3: Implement git package**

```go
// git/git.go
package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// CommitRecord holds aggregated stats for a single commit in a single repo.
type CommitRecord struct {
	Author   string
	Date     time.Time
	Added    int
	Removed  int
	Files    int
	RepoName string
}

// DetectDefaultBranch returns the default branch for the repo at dir.
// Tries origin/HEAD, then main, then master, then HEAD.
func DetectDefaultBranch(dir string) string {
	// Try origin/HEAD
	out, err := runGit(dir, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		if ref != "" {
			parts := strings.Split(ref, "/")
			return parts[len(parts)-1]
		}
	}

	// Try common branch names
	for _, candidate := range []string{"main", "master"} {
		_, err := runGit(dir, "rev-parse", "--verify", candidate)
		if err == nil {
			return candidate
		}
	}

	return "HEAD"
}

// CollectCommits runs git log once and returns all commit records for the repo.
func CollectCommits(dir string, ref string) ([]CommitRecord, error) {
	repoName := repoNameFromPath(dir)

	out, err := runGit(dir, "log", ref, "--no-merges", "--format=%aN|%aI", "--numstat")
	if err != nil {
		return nil, fmt.Errorf("git log in %s: %w", dir, err)
	}

	return parseGitLog(out, repoName), nil
}

// parseGitLog parses the combined format/numstat output into CommitRecords.
// Each commit appears as:
//
//	AuthorName|2026-03-26T10:00:00-07:00
//
//	3	0	file1.go
//	5	2	file2.go
//
// Blank lines separate commits.
func parseGitLog(output string, repoName string) []CommitRecord {
	var records []CommitRecord
	var current *CommitRecord

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		if line == "" {
			if current != nil && current.Files > 0 {
				records = append(records, *current)
				current = nil
			}
			continue
		}

		// Try to parse as author|date header
		if strings.Contains(line, "|") && !strings.Contains(line, "\t") {
			// Flush previous if it has data
			if current != nil && current.Files > 0 {
				records = append(records, *current)
			}

			parts := strings.SplitN(line, "|", 2)
			if len(parts) == 2 {
				date, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
				if err != nil {
					continue
				}
				current = &CommitRecord{
					Author:   strings.TrimSpace(parts[0]),
					Date:     date,
					RepoName: repoName,
				}
			}
			continue
		}

		// Try to parse as numstat line: added\tremoved\tfilename
		if current != nil && strings.Contains(line, "\t") {
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) < 3 {
				continue
			}
			// Skip binary files (shown as "-")
			if fields[0] == "-" || fields[1] == "-" {
				continue
			}
			added, err1 := strconv.Atoi(fields[0])
			removed, err2 := strconv.Atoi(fields[1])
			if err1 != nil || err2 != nil {
				continue
			}
			current.Added += added
			current.Removed += removed
			current.Files++
		}
	}

	// Flush final record
	if current != nil && current.Files > 0 {
		records = append(records, *current)
	}

	return records
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func repoNameFromPath(dir string) string {
	parts := strings.Split(strings.TrimRight(dir, "/"), "/")
	if len(parts) == 0 {
		return dir
	}
	return parts[len(parts)-1]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/rdh/src/bigboard && go test ./git/... -v`
Expected: PASS — all 3 tests pass.

- [ ] **Step 5: Write tests for repo discovery**

Add to `git/git_test.go`:

```go
func TestDiscoverRepos(t *testing.T) {
	// Create a parent dir with two git repos and one non-repo dir
	parent := t.TempDir()

	repo1 := filepath.Join(parent, "repo1")
	repo2 := filepath.Join(parent, "repo2")
	notRepo := filepath.Join(parent, "notrepo")

	for _, d := range []string{repo1, repo2, notRepo} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Init repo1 and repo2 as git repos
	for _, d := range []string{repo1, repo2} {
		cmd := exec.Command("git", "init")
		cmd.Dir = d
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init in %s failed: %s %v", d, out, err)
		}
	}

	// Discover from parent directory
	repos := DiscoverRepos([]string{parent})
	if len(repos) != 2 {
		t.Errorf("expected 2 repos, got %d: %v", len(repos), repos)
	}

	// Discover from direct repo path
	repos = DiscoverRepos([]string{repo1})
	if len(repos) != 1 {
		t.Errorf("expected 1 repo, got %d", len(repos))
	}

	// Mix of direct and directory
	repos = DiscoverRepos([]string{repo1, parent})
	// Should find repo1 directly + repo1 and repo2 from parent, deduplicated
	// At minimum we expect >= 2
	if len(repos) < 2 {
		t.Errorf("expected at least 2 repos, got %d", len(repos))
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd /Users/rdh/src/bigboard && go test ./git/... -run TestDiscoverRepos -v`
Expected: FAIL — `DiscoverRepos` not defined.

- [ ] **Step 7: Implement DiscoverRepos**

Add to `git/git.go`:

```go
// DiscoverRepos takes a list of paths and returns deduplicated git repo directories.
// If a path is a git repo, it's used directly.
// If a path is a directory, it scans one level deep for git repos.
func DiscoverRepos(paths []string) []string {
	seen := make(map[string]bool)
	var repos []string

	for _, p := range paths {
		if isGitRepo(p) {
			abs := absPath(p)
			if !seen[abs] {
				seen[abs] = true
				repos = append(repos, abs)
			}
			continue
		}

		// Scan one level deep
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sub := filepath.Join(p, entry.Name())
			if isGitRepo(sub) {
				abs := absPath(sub)
				if !seen[abs] {
					seen[abs] = true
					repos = append(repos, abs)
				}
			}
		}
	}

	return repos
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
```

Add these imports to the top of `git/git.go`: `"os"` and `"path/filepath"`.

- [ ] **Step 8: Run all git tests**

Run: `cd /Users/rdh/src/bigboard && go test ./git/... -v`
Expected: PASS — all tests pass.

- [ ] **Step 9: Commit**

```bash
cd /Users/rdh/src/bigboard
git add git/
git commit -m "feat: add git data collection package

Single-pass git log parsing, branch detection, and repo discovery."
```

---

### Task 3: Stats Aggregation Package

**Files:**
- Create: `stats/stats.go`
- Create: `stats/stats_test.go`

- [ ] **Step 1: Write tests for author merging and aggregation**

```go
// stats/stats_test.go
package stats

import (
	"testing"
	"time"

	"github.com/rdh/bigboard/git"
)

func TestMergeAuthors(t *testing.T) {
	names := []string{"John Smith", "john smith", "JOHN SMITH", "Jane Doe"}
	canonical := MergeAuthorName("John Smith", names, map[string]int{
		"John Smith": 10,
		"john smith": 5,
		"JOHN SMITH": 2,
	})
	if canonical != "John Smith" {
		t.Errorf("expected 'John Smith', got %q", canonical)
	}
}

func TestAreSimilarNames(t *testing.T) {
	tests := []struct {
		a, b   string
		expect bool
	}{
		{"John Smith", "john smith", true},
		{"John Smith", "JOHN SMITH", true},
		{"John Smith", "Jane Doe", false},
		{"jsmith", "John Smith", false}, // too short for substring
		{"johnsmith", "John Smith Jones", true}, // substring match
		{"ab", "cd", false},
	}
	for _, tt := range tests {
		got := AreSimilarNames(tt.a, tt.b)
		if got != tt.expect {
			t.Errorf("AreSimilarNames(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.expect)
		}
	}
}

func TestAggregate(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice", Date: now, Added: 100, Removed: 20, Files: 3, RepoName: "repo1"},
		{Author: "Alice", Date: now.Add(-time.Hour), Added: 50, Removed: 10, Files: 2, RepoName: "repo1"},
		{Author: "Bob", Date: now, Added: 200, Removed: 80, Files: 5, RepoName: "repo1"},
		{Author: "Alice", Date: now, Added: 30, Removed: 5, Files: 1, RepoName: "repo2"},
	}

	result := Aggregate(records)

	if len(result) != 2 {
		t.Fatalf("expected 2 authors, got %d", len(result))
	}

	// Find Alice
	var alice *AuthorStats
	for i := range result {
		if result[i].Name == "Alice" {
			alice = &result[i]
			break
		}
	}
	if alice == nil {
		t.Fatal("Alice not found")
	}

	if alice.Commits != 3 {
		t.Errorf("Alice commits: got %d, want 3", alice.Commits)
	}
	if alice.Added != 180 {
		t.Errorf("Alice added: got %d, want 180", alice.Added)
	}
	if alice.Removed != 35 {
		t.Errorf("Alice removed: got %d, want 35", alice.Removed)
	}
	if alice.Net != 145 {
		t.Errorf("Alice net: got %d, want 145", alice.Net)
	}
	if alice.TotalChange != 215 {
		t.Errorf("Alice total: got %d, want 215", alice.TotalChange)
	}

	// Check per-repo breakdown
	if len(alice.PerRepo) != 2 {
		t.Errorf("Alice per-repo: got %d repos, want 2", len(alice.PerRepo))
	}
}

func TestFilterByTime(t *testing.T) {
	now := time.Now()
	records := []git.CommitRecord{
		{Author: "Alice", Date: now, Added: 10, Removed: 0, Files: 1, RepoName: "repo1"},
		{Author: "Alice", Date: now.Add(-60 * 24 * time.Hour), Added: 20, Removed: 0, Files: 1, RepoName: "repo1"},
		{Author: "Alice", Date: now.Add(-400 * 24 * time.Hour), Added: 30, Removed: 0, Files: 1, RepoName: "repo1"},
	}

	// Filter to last 30 days
	filtered := FilterByTime(records, 30*24*time.Hour)
	if len(filtered) != 1 {
		t.Errorf("30d filter: got %d records, want 1", len(filtered))
	}

	// Filter to last 90 days
	filtered = FilterByTime(records, 90*24*time.Hour)
	if len(filtered) != 2 {
		t.Errorf("90d filter: got %d records, want 2", len(filtered))
	}

	// No filter (0 duration = all)
	filtered = FilterByTime(records, 0)
	if len(filtered) != 3 {
		t.Errorf("all filter: got %d records, want 3", len(filtered))
	}
}

func TestSort(t *testing.T) {
	stats := []AuthorStats{
		{Name: "Alice", Commits: 10, Added: 100, TotalChange: 200},
		{Name: "Bob", Commits: 20, Added: 50, TotalChange: 300},
		{Name: "Charlie", Commits: 5, Added: 200, TotalChange: 100},
	}

	Sort(stats, SortByCommits)
	if stats[0].Name != "Bob" {
		t.Errorf("sort by commits: first should be Bob, got %s", stats[0].Name)
	}

	Sort(stats, SortByTotal)
	if stats[0].Name != "Bob" {
		t.Errorf("sort by total: first should be Bob, got %s", stats[0].Name)
	}

	Sort(stats, SortByAdded)
	if stats[0].Name != "Charlie" {
		t.Errorf("sort by added: first should be Charlie, got %s", stats[0].Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/rdh/src/bigboard && go test ./stats/... -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement stats package**

```go
// stats/stats.go
package stats

import (
	"sort"
	"strings"
	"time"

	"github.com/rdh/bigboard/git"
)

// SortField represents the column to sort by.
type SortField int

const (
	SortByTotal SortField = iota
	SortByCommits
	SortByAdded
	SortByRemoved
	SortByNet
)

// AuthorStats holds aggregated statistics for a single author.
type AuthorStats struct {
	Name        string
	Commits     int
	Added       int
	Removed     int
	Net         int
	TotalChange int
	PerRepo     map[string]*RepoContribution
}

// RepoContribution holds an author's stats within a single repo.
type RepoContribution struct {
	Commits     int
	Added       int
	Removed     int
	Net         int
	TotalChange int
}

// Aggregate takes raw commit records and returns per-author stats.
// Author names are fuzzy-merged before aggregation.
func Aggregate(records []git.CommitRecord) []AuthorStats {
	// Collect all unique author names and their commit counts
	commitCounts := make(map[string]int)
	for _, r := range records {
		commitCounts[r.Author]++
	}

	// Build canonical name mapping
	names := make([]string, 0, len(commitCounts))
	for name := range commitCounts {
		names = append(names, name)
	}
	canonicalMap := buildCanonicalMap(names, commitCounts)

	// Aggregate using canonical names
	byAuthor := make(map[string]*AuthorStats)
	for _, r := range records {
		canonical := canonicalMap[r.Author]
		as, ok := byAuthor[canonical]
		if !ok {
			as = &AuthorStats{
				Name:    canonical,
				PerRepo: make(map[string]*RepoContribution),
			}
			byAuthor[canonical] = as
		}

		as.Commits++
		as.Added += r.Added
		as.Removed += r.Removed

		rc, ok := as.PerRepo[r.RepoName]
		if !ok {
			rc = &RepoContribution{}
			as.PerRepo[r.RepoName] = rc
		}
		rc.Commits++
		rc.Added += r.Added
		rc.Removed += r.Removed
	}

	// Compute derived fields
	result := make([]AuthorStats, 0, len(byAuthor))
	for _, as := range byAuthor {
		as.Net = as.Added - as.Removed
		as.TotalChange = as.Added + as.Removed
		for _, rc := range as.PerRepo {
			rc.Net = rc.Added - rc.Removed
			rc.TotalChange = rc.Added + rc.Removed
		}
		result = append(result, *as)
	}

	return result
}

// FilterByTime returns records within the given duration from now.
// A duration of 0 returns all records.
func FilterByTime(records []git.CommitRecord, d time.Duration) []git.CommitRecord {
	if d == 0 {
		return records
	}

	cutoff := time.Now().Add(-d)
	var filtered []git.CommitRecord
	for _, r := range records {
		if r.Date.After(cutoff) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// Sort sorts author stats by the given field in descending order.
func Sort(stats []AuthorStats, field SortField) {
	sort.Slice(stats, func(i, j int) bool {
		switch field {
		case SortByCommits:
			return stats[i].Commits > stats[j].Commits
		case SortByAdded:
			return stats[i].Added > stats[j].Added
		case SortByRemoved:
			return stats[i].Removed > stats[j].Removed
		case SortByNet:
			return stats[i].Net > stats[j].Net
		default: // SortByTotal
			return stats[i].TotalChange > stats[j].TotalChange
		}
	})
}

// SortFieldFromString converts a string to a SortField.
func SortFieldFromString(s string) SortField {
	switch strings.ToLower(s) {
	case "commits":
		return SortByCommits
	case "added":
		return SortByAdded
	case "removed":
		return SortByRemoved
	case "net":
		return SortByNet
	default:
		return SortByTotal
	}
}

// AreSimilarNames returns true if two author names are likely the same person.
func AreSimilarNames(a, b string) bool {
	normA := normalizedName(a)
	normB := normalizedName(b)

	if normA == "" || normB == "" {
		return false
	}
	if normA == normB {
		return true
	}
	if len(normA) > 5 && (strings.Contains(normA, normB) || strings.Contains(normB, normA)) {
		return true
	}
	if len(normB) > 5 && (strings.Contains(normA, normB) || strings.Contains(normB, normA)) {
		return true
	}
	return false
}

// MergeAuthorName returns the canonical name for an author given a list of similar names.
func MergeAuthorName(name string, allNames []string, commitCounts map[string]int) string {
	var similar []string
	for _, other := range allNames {
		if AreSimilarNames(name, other) {
			similar = append(similar, other)
		}
	}
	if len(similar) == 0 {
		return name
	}

	best := similar[0]
	for _, s := range similar[1:] {
		if commitCounts[s] > commitCounts[best] {
			best = s
		}
	}
	return best
}

func buildCanonicalMap(names []string, commitCounts map[string]int) map[string]string {
	canonical := make(map[string]string)
	processed := make(map[string]bool)

	for _, name := range names {
		if processed[name] {
			continue
		}

		// Find all similar names
		var group []string
		for _, other := range names {
			if AreSimilarNames(name, other) {
				group = append(group, other)
				processed[other] = true
			}
		}

		// Pick canonical: highest commit count
		best := group[0]
		for _, g := range group[1:] {
			if commitCounts[g] > commitCounts[best] {
				best = g
			}
		}

		for _, g := range group {
			canonical[g] = best
		}
	}

	return canonical
}

func normalizedName(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), "")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/rdh/src/bigboard && go test ./stats/... -v`
Expected: PASS — all tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/rdh/src/bigboard
git add stats/
git commit -m "feat: add stats aggregation package

Author merging, time filtering, sorting, and per-repo breakdown."
```

---

### Task 4: TUI Styles

**Files:**
- Create: `tui/styles.go`

- [ ] **Step 1: Create the cyberpunk theme**

```go
// tui/styles.go
package tui

import "github.com/charmbracelet/lipgloss"

// Cyberpunk color palette
var (
	ColorBg        = lipgloss.Color("#050510")
	ColorCyan      = lipgloss.Color("#00FFFF")
	ColorMagenta   = lipgloss.Color("#FF00FF")
	ColorGreen     = lipgloss.Color("#00FF88")
	ColorDimCyan   = lipgloss.Color("#005566")
	ColorDimWhite  = lipgloss.Color("#666666")
	ColorBrightWht = lipgloss.Color("#E0E0E0")
	ColorRowEven   = lipgloss.Color("#0A0A18")
	ColorRowOdd    = lipgloss.Color("#080814")
	ColorRowSelect = lipgloss.Color("#0F1F2F")
)

// Layout styles
var (
	StyleApp = lipgloss.NewStyle().
			Background(ColorBg)

	StyleTitle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColorDimCyan)

	StyleGlitchLine = lipgloss.NewStyle().
			Foreground(ColorCyan)

	StyleStatBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDimCyan).
			Padding(0, 2).
			Align(lipgloss.Center)

	StyleStatValue = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

	StyleStatLabel = lipgloss.NewStyle().
			Foreground(ColorDimCyan)

	StyleRepoTag = lipgloss.NewStyle().
			Foreground(ColorMagenta).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#550055")).
			Padding(0, 1)

	StyleRepoTagActive = lipgloss.NewStyle().
				Foreground(ColorMagenta).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorMagenta).
				Bold(true).
				Padding(0, 1)

	StyleTableHeader = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true)

	StyleRowEven = lipgloss.NewStyle().
			Background(ColorRowEven)

	StyleRowOdd = lipgloss.NewStyle().
			Background(ColorRowOdd)

	StyleRowSelected = lipgloss.NewStyle().
				Background(ColorRowSelect).
				Bold(true)

	StyleRank = lipgloss.NewStyle().
			Foreground(ColorDimCyan)

	StyleAuthor = lipgloss.NewStyle().
			Foreground(ColorBrightWht)

	StyleNumeric = lipgloss.NewStyle().
			Foreground(ColorGreen)

	StyleTimePicker = lipgloss.NewStyle().
			Foreground(ColorDimWhite)

	StyleTimePickerActive = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorCyan).
				Padding(0, 1)

	StyleTimePickerInactive = lipgloss.NewStyle().
				Foreground(ColorDimWhite).
				Padding(0, 1)

	StyleFooter = lipgloss.NewStyle().
			Foreground(ColorDimCyan)

	StyleHelpKey = lipgloss.NewStyle().
			Foreground(ColorCyan)

	StyleHelpDesc = lipgloss.NewStyle().
			Foreground(ColorDimWhite)

	StyleBarCyan    = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleBarMagenta = lipgloss.NewStyle().Foreground(ColorMagenta)
)
```

- [ ] **Step 2: Install dependencies and verify compilation**

Run: `cd /Users/rdh/src/bigboard && go get github.com/charmbracelet/lipgloss && go build ./tui/...`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
cd /Users/rdh/src/bigboard
git add tui/
git commit -m "feat: add cyberpunk TUI theme

Lip Gloss styles for the Big Board aesthetic — cyan, magenta, green
on deep dark background with CRT-inspired alternating rows."
```

---

### Task 5: TUI Components

**Files:**
- Create: `tui/components.go`
- Create: `tui/components_test.go`

- [ ] **Step 1: Write tests for reusable components**

```go
// tui/components_test.go
package tui

import (
	"strings"
	"testing"
)

func TestRenderImpactBar(t *testing.T) {
	bar := RenderImpactBar(100, 100, 20)
	if len([]rune(bar)) == 0 {
		t.Error("impact bar should not be empty for max value")
	}

	bar = RenderImpactBar(0, 100, 20)
	if strings.ContainsRune(bar, '█') {
		t.Error("impact bar should be empty for zero value")
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}
	for _, tt := range tests {
		got := FormatNumber(tt.n)
		if got != tt.want {
			t.Errorf("FormatNumber(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		w    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world foo", 10, "hello w..."},
		{"hi", 2, "hi"},
	}
	for _, tt := range tests {
		got := Truncate(tt.s, tt.w)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.w, got, tt.want)
		}
	}
}

func TestRenderHeader(t *testing.T) {
	header := RenderHeader(80)
	if !strings.Contains(header, "BIG BOARD") {
		t.Error("header should contain BIG BOARD")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/rdh/src/bigboard && go test ./tui/... -v`
Expected: FAIL — functions not defined.

- [ ] **Step 3: Implement components**

```go
// tui/components.go
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// TimePreset represents a time range filter option.
type TimePreset struct {
	Label    string
	Duration time.Duration // 0 means "all time"
}

// TimePresets are the available time range filters.
var TimePresets = []TimePreset{
	{Label: "30d", Duration: 30 * 24 * time.Hour},
	{Label: "90d", Duration: 90 * 24 * time.Hour},
	{Label: "1y", Duration: 365 * 24 * time.Hour},
	{Label: "ALL", Duration: 0},
}

// RenderHeader renders the Big Board title banner.
func RenderHeader(width int) string {
	title := StyleTitle.Render("⟐ BIG BOARD ⟐")
	subtitle := StyleSubtitle.Render("// CONTRIBUTOR INTELLIGENCE SYSTEM")
	glitch := StyleGlitchLine.Render(strings.Repeat("═", width))

	return lipgloss.JoinVertical(lipgloss.Left, title, subtitle, glitch)
}

// RenderStatBoxes renders the summary stat boxes (commits, added, removed).
func RenderStatBoxes(commits, added, removed int) string {
	box := func(label string, value int) string {
		val := StyleStatValue.Render(FormatNumber(value))
		lab := StyleStatLabel.Render(label)
		return StyleStatBox.Render(lipgloss.JoinVertical(lipgloss.Center, val, lab))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		box("COMMITS", commits),
		"  ",
		box("ADDED", added),
		"  ",
		box("REMOVED", removed),
	)
}

// RenderTimePicker renders the time range selector.
func RenderTimePicker(activeIdx int) string {
	var parts []string
	for i, preset := range TimePresets {
		if i == activeIdx {
			parts = append(parts, StyleTimePickerActive.Render(preset.Label))
		} else {
			parts = append(parts, StyleTimePickerInactive.Render(preset.Label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

// RenderRepoTags renders the repo tag pills.
func RenderRepoTags(repos []string, focusedIdx int, hasFocus bool) string {
	var parts []string
	for i, repo := range repos {
		if hasFocus && i == focusedIdx {
			parts = append(parts, StyleRepoTagActive.Render(repo))
		} else {
			parts = append(parts, StyleRepoTag.Render(repo))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

// RenderImpactBar renders a gradient bar from cyan to magenta.
func RenderImpactBar(value, maxValue, barWidth int) string {
	if maxValue == 0 || value == 0 {
		return strings.Repeat(" ", barWidth)
	}

	filled := (value * barWidth) / maxValue
	if filled > barWidth {
		filled = barWidth
	}
	if filled == 0 && value > 0 {
		filled = 1
	}

	// Gradient: first half cyan, second half magenta
	mid := filled / 2
	bar := StyleBarCyan.Render(strings.Repeat("█", mid)) +
		StyleBarMagenta.Render(strings.Repeat("█", filled-mid))

	return bar + strings.Repeat(" ", barWidth-filled)
}

// RenderFooter renders the bottom status bar.
func RenderFooter(repoCount int, width int) string {
	left := StyleFooter.Render(fmt.Sprintf("SYS.STATUS: NOMINAL  //  %d repos scanned", repoCount))
	right := StyleFooter.Render(time.Now().Format("2006-01-02T15:04:05Z"))

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

// RenderHelpBar renders the key binding help at the bottom.
func RenderHelpBar() string {
	help := func(key, desc string) string {
		return StyleHelpKey.Render("["+key+"]") + StyleHelpDesc.Render(desc)
	}

	return lipgloss.JoinHorizontal(lipgloss.Center,
		help("q", "uit"), "  ",
		help("s", "ort"), "  ",
		help("↵", "drill"), "  ",
		help("←→", "time"), "  ",
		help("tab", "focus"),
	)
}

// FormatNumber formats an integer with thousand separators.
func FormatNumber(n int) string {
	if n < 0 {
		return "-" + FormatNumber(-n)
	}

	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
		if len(s) > remainder {
			result.WriteString(",")
		}
	}
	for i := remainder; i < len(s); i += 3 {
		if i > remainder {
			result.WriteString(",")
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}

// Truncate clamps a string to the given width, adding "..." if truncated.
func Truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 3 {
		return s[:width]
	}
	return s[:width-3] + "..."
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/rdh/src/bigboard && go test ./tui/... -v`
Expected: PASS — all component tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/rdh/src/bigboard
git add tui/
git commit -m "feat: add TUI components

Stat boxes, impact bars, time picker, repo tags, header, footer,
and number formatting helpers."
```

---

### Task 6: TUI Aggregate View

**Files:**
- Create: `tui/aggregate.go`

- [ ] **Step 1: Implement the aggregate leaderboard view**

```go
// tui/aggregate.go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rdh/bigboard/stats"
)

// AggregateView renders the main leaderboard screen.
type AggregateView struct{}

// RenderTable renders the contributor table.
func (v AggregateView) RenderTable(authors []stats.AuthorStats, selectedRow int, sortField stats.SortField, width int) string {
	if len(authors) == 0 {
		return StyleSubtitle.Render("\n  No commit data found.\n")
	}

	nameW := 22
	numW := 10
	barW := 20

	// Find max total for impact bar scaling
	maxTotal := 0
	for _, a := range authors {
		if a.TotalChange > maxTotal {
			maxTotal = a.TotalChange
		}
	}

	// Header
	headers := []struct {
		label string
		width int
		field stats.SortField
	}{
		{"#", 4, -1},
		{"OPERATIVE", nameW, -1},
		{"COMMITS", numW, stats.SortByCommits},
		{"ADDED", numW, stats.SortByAdded},
		{"REMOVED", numW, stats.SortByRemoved},
		{"NET", numW, stats.SortByNet},
		{"IMPACT", barW, stats.SortByTotal},
	}

	var headerParts []string
	for _, h := range headers {
		label := h.label
		if h.field == sortField {
			label += " ▼"
		}
		headerParts = append(headerParts, StyleTableHeader.Render(fmt.Sprintf("%-*s", h.width, label)))
	}
	header := strings.Join(headerParts, " ")
	separator := StyleGlitchLine.Render(strings.Repeat("─", width))

	var rows []string
	rows = append(rows, header)
	rows = append(rows, separator)

	// Limit to 20 rows
	limit := len(authors)
	if limit > 20 {
		limit = 20
	}

	for i := 0; i < limit; i++ {
		a := authors[i]

		rank := StyleRank.Render(fmt.Sprintf("%02d", i+1))
		name := StyleAuthor.Render(fmt.Sprintf("%-*s", nameW, Truncate(a.Name, nameW)))
		commits := StyleNumeric.Render(fmt.Sprintf("%-*s", numW, FormatNumber(a.Commits)))
		added := StyleNumeric.Render(fmt.Sprintf("%-*s", numW, FormatNumber(a.Added)))
		removed := StyleNumeric.Render(fmt.Sprintf("%-*s", numW, FormatNumber(a.Removed)))
		net := StyleNumeric.Render(fmt.Sprintf("%-*s", numW, FormatNumber(a.Net)))
		bar := RenderImpactBar(a.TotalChange, maxTotal, barW)

		row := fmt.Sprintf("%s  %s %s %s %s %s %s", rank, name, commits, added, removed, net, bar)

		// Apply row styling
		var rowStyle lipgloss.Style
		if i == selectedRow {
			rowStyle = StyleRowSelected
		} else if i%2 == 0 {
			rowStyle = StyleRowEven
		} else {
			rowStyle = StyleRowOdd
		}
		rows = append(rows, rowStyle.Render(row))
	}

	return strings.Join(rows, "\n")
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/rdh/src/bigboard && go build ./tui/...`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
cd /Users/rdh/src/bigboard
git add tui/aggregate.go
git commit -m "feat: add aggregate leaderboard view

Renders contributor table with rank, stats, impact bars, and sort indicators."
```

---

### Task 7: TUI Repo Drill-Down View

**Files:**
- Create: `tui/repoview.go`

- [ ] **Step 1: Implement the per-repo drill-down view**

```go
// tui/repoview.go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rdh/bigboard/stats"
)

// RepoView renders a single repo's contributor stats.
type RepoView struct{}

// RenderRepoTable renders the contributor table filtered to a single repo.
func (v RepoView) RenderRepoTable(repoName string, authors []stats.AuthorStats, selectedRow int, sortField stats.SortField, width int) string {
	// Filter to authors who contributed to this repo and build repo-specific stats
	var filtered []stats.AuthorStats
	for _, a := range authors {
		rc, ok := a.PerRepo[repoName]
		if !ok {
			continue
		}
		filtered = append(filtered, stats.AuthorStats{
			Name:        a.Name,
			Commits:     rc.Commits,
			Added:       rc.Added,
			Removed:     rc.Removed,
			Net:         rc.Net,
			TotalChange: rc.TotalChange,
		})
	}

	// Re-sort the filtered list
	stats.Sort(filtered, sortField)

	// Render header
	repoHeader := StyleTitle.Render(fmt.Sprintf("⟐ %s ⟐", strings.ToUpper(repoName)))
	backHint := StyleSubtitle.Render("// [ESC] back to aggregate")

	header := lipgloss.JoinVertical(lipgloss.Left, repoHeader, backHint,
		StyleGlitchLine.Render(strings.Repeat("═", width)))

	// Compute summary stats for this repo
	totalCommits, totalAdded, totalRemoved := 0, 0, 0
	for _, a := range filtered {
		totalCommits += a.Commits
		totalAdded += a.Added
		totalRemoved += a.Removed
	}
	statBoxes := RenderStatBoxes(totalCommits, totalAdded, totalRemoved)

	// Reuse aggregate view for the table
	agg := AggregateView{}
	table := agg.RenderTable(filtered, selectedRow, sortField, width)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", statBoxes, "", table)
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/rdh/src/bigboard && go build ./tui/...`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
cd /Users/rdh/src/bigboard
git add tui/repoview.go
git commit -m "feat: add repo drill-down view

Per-repo contributor table with filtered stats and back navigation hint."
```

---

### Task 8: TUI App (Bubble Tea Root Model)

**Files:**
- Create: `tui/app.go`

- [ ] **Step 1: Implement the root Bubble Tea model**

```go
// tui/app.go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
)

// ViewMode tracks which screen is active.
type ViewMode int

const (
	ViewAggregate ViewMode = iota
	ViewRepo
)

// FocusArea tracks which UI element has focus.
type FocusArea int

const (
	FocusTable FocusArea = iota
	FocusRepos
)

// Model is the root Bubble Tea model for Big Board.
type Model struct {
	// Data
	allRecords []git.CommitRecord
	authors    []stats.AuthorStats
	repoNames  []string

	// View state
	viewMode    ViewMode
	focus       FocusArea
	selectedRow int
	selectedRepo int
	activeRepo  string // repo name when in drill-down

	// Controls
	sortField   stats.SortField
	timeIdx     int

	// Layout
	width  int
	height int

	// Loading state
	loading bool
	err     error
}

// dataLoadedMsg is sent when git data collection is complete.
type dataLoadedMsg struct {
	records   []git.CommitRecord
	repoNames []string
	err       error
}

// NewModel creates a new Big Board model.
func NewModel(repoPaths []string, initialSort stats.SortField) Model {
	return Model{
		sortField: initialSort,
		timeIdx:   3, // default to ALL
		loading:   true,
	}
}

// LoadDataCmd returns a command that collects git data from repos.
func LoadDataCmd(repoPaths []string) tea.Cmd {
	return func() tea.Msg {
		var allRecords []git.CommitRecord
		var repoNames []string

		type result struct {
			records  []git.CommitRecord
			repoName string
			err      error
		}

		ch := make(chan result, len(repoPaths))
		for _, path := range repoPaths {
			go func(p string) {
				branch := git.DetectDefaultBranch(p)
				records, err := git.CollectCommits(p, branch)
				name := ""
				if len(records) > 0 {
					name = records[0].RepoName
				} else {
					// Extract name from path
					parts := strings.Split(strings.TrimRight(p, "/"), "/")
					name = parts[len(parts)-1]
				}
				ch <- result{records: records, repoName: name, err: err}
			}(path)
		}

		for range repoPaths {
			r := <-ch
			if r.err != nil {
				continue // skip repos that fail
			}
			allRecords = append(allRecords, r.records...)
			repoNames = append(repoNames, r.repoName)
		}

		return dataLoadedMsg{records: allRecords, repoNames: repoNames}
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case dataLoadedMsg:
		m.loading = false
		m.err = msg.err
		m.allRecords = msg.records
		m.repoNames = msg.repoNames
		m.recomputeAuthors()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc":
		if m.viewMode == ViewRepo {
			m.viewMode = ViewAggregate
			m.selectedRow = 0
		} else {
			return m, tea.Quit
		}

	case "up", "k":
		if m.focus == FocusTable && m.selectedRow > 0 {
			m.selectedRow--
		}

	case "down", "j":
		if m.focus == FocusTable {
			max := len(m.authors) - 1
			if max > 19 {
				max = 19
			}
			if m.selectedRow < max {
				m.selectedRow++
			}
		}

	case "left", "h":
		if m.focus == FocusRepos {
			if m.selectedRepo > 0 {
				m.selectedRepo--
			}
		} else {
			if m.timeIdx > 0 {
				m.timeIdx--
				m.recomputeAuthors()
				m.selectedRow = 0
			}
		}

	case "right", "l":
		if m.focus == FocusRepos {
			if m.selectedRepo < len(m.repoNames)-1 {
				m.selectedRepo++
			}
		} else {
			if m.timeIdx < len(TimePresets)-1 {
				m.timeIdx++
				m.recomputeAuthors()
				m.selectedRow = 0
			}
		}

	case "s":
		// Cycle sort field
		m.sortField = (m.sortField + 1) % 5
		stats.Sort(m.authors, m.sortField)
		m.selectedRow = 0

	case "tab":
		if m.viewMode == ViewAggregate {
			if m.focus == FocusTable {
				m.focus = FocusRepos
			} else {
				m.focus = FocusTable
			}
		}

	case "enter":
		if m.focus == FocusRepos && m.viewMode == ViewAggregate {
			if m.selectedRepo < len(m.repoNames) {
				m.activeRepo = m.repoNames[m.selectedRepo]
				m.viewMode = ViewRepo
				m.selectedRow = 0
				m.focus = FocusTable
			}
		}
	}

	return m, nil
}

func (m *Model) recomputeAuthors() {
	preset := TimePresets[m.timeIdx]
	filtered := stats.FilterByTime(m.allRecords, preset.Duration)
	m.authors = stats.Aggregate(filtered)
	stats.Sort(m.authors, m.sortField)
}

// View implements tea.Model.
func (m Model) View() string {
	if m.loading {
		return StyleTitle.Render("\n  ⟐ BIG BOARD ⟐\n\n") +
			StyleSubtitle.Render("  // SCANNING REPOSITORIES...") + "\n"
	}

	if m.err != nil {
		return StyleTitle.Render("\n  ⟐ BIG BOARD ⟐\n\n") +
			lipgloss.NewStyle().Foreground(ColorMagenta).Render(
				fmt.Sprintf("  ERROR: %v\n", m.err))
	}

	w := m.width
	if w == 0 {
		w = 100
	}

	switch m.viewMode {
	case ViewRepo:
		return m.renderRepoView(w)
	default:
		return m.renderAggregateView(w)
	}
}

func (m Model) renderAggregateView(width int) string {
	var sections []string

	// Header
	sections = append(sections, RenderHeader(width))
	sections = append(sections, "")

	// Time picker + repo count
	timePicker := RenderTimePicker(m.timeIdx)
	repoCount := StyleSubtitle.Render(fmt.Sprintf("%d repos scanned", len(m.repoNames)))
	gap := width - lipgloss.Width(timePicker) - lipgloss.Width(repoCount)
	if gap < 1 {
		gap = 1
	}
	sections = append(sections, timePicker+strings.Repeat(" ", gap)+repoCount)
	sections = append(sections, "")

	// Summary stats
	totalCommits, totalAdded, totalRemoved := 0, 0, 0
	for _, a := range m.authors {
		totalCommits += a.Commits
		totalAdded += a.Added
		totalRemoved += a.Removed
	}
	sections = append(sections, RenderStatBoxes(totalCommits, totalAdded, totalRemoved))
	sections = append(sections, "")

	// Repo tags
	if len(m.repoNames) > 0 {
		sections = append(sections, RenderRepoTags(m.repoNames, m.selectedRepo, m.focus == FocusRepos))
		sections = append(sections, "")
	}

	// Table
	agg := AggregateView{}
	sections = append(sections, agg.RenderTable(m.authors, m.selectedRow, m.sortField, width))
	sections = append(sections, "")

	// Help + Footer
	sections = append(sections, RenderHelpBar())
	sections = append(sections, RenderFooter(len(m.repoNames), width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) renderRepoView(width int) string {
	rv := RepoView{}
	table := rv.RenderRepoTable(m.activeRepo, m.authors, m.selectedRow, m.sortField, width)

	help := RenderHelpBar()
	footer := RenderFooter(len(m.repoNames), width)

	return lipgloss.JoinVertical(lipgloss.Left, table, "", help, footer)
}
```

- [ ] **Step 2: Install Bubble Tea dependency and verify compilation**

Run: `cd /Users/rdh/src/bigboard && go get github.com/charmbracelet/bubbletea && go build ./tui/...`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
cd /Users/rdh/src/bigboard
git add tui/app.go
git commit -m "feat: add Bubble Tea root model

View switching, key bindings, time filtering, sort cycling,
drill-down navigation, and loading state."
```

---

### Task 9: CLI Entry Point

**Files:**
- Create: `cmd/bigboard/main.go`

- [ ] **Step 1: Implement CLI entry point**

```go
// cmd/bigboard/main.go
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
	"github.com/rdh/bigboard/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	sortFlag := flag.String("sort", "total", "Initial sort: commits|added|removed|net|total")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("bigboard %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	// Discover repos
	repoPaths := git.DiscoverRepos(paths)
	if len(repoPaths) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no git repositories found in %v\n", paths)
		os.Exit(1)
	}

	initialSort := stats.SortFieldFromString(*sortFlag)
	model := tui.NewModel(repoPaths, initialSort)

	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start data loading after the program starts
	go func() {
		p.Send(tui.LoadDataCmd(repoPaths)())
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Tidy modules and verify build**

Run: `cd /Users/rdh/src/bigboard && go mod tidy && make build`
Expected: Binary built to `bin/bigboard`.

- [ ] **Step 3: Smoke test — run against a real repo**

Run: `cd /Users/rdh/src/bigboard && ./bin/bigboard /Users/rdh/src/agentic-code-reviewer`
Expected: Big Board TUI launches, shows contributor stats for the acr repo. Press `q` to exit.

- [ ] **Step 4: Smoke test — run with directory scan**

Run: `cd /Users/rdh/src/bigboard && ./bin/bigboard /Users/rdh/src`
Expected: Discovers multiple repos, shows aggregate stats. Repo tags visible. Can tab to repos, press Enter to drill down, Esc to go back, ←→ to change time range, `s` to cycle sort. Press `q` to exit.

- [ ] **Step 5: Commit**

```bash
cd /Users/rdh/src/bigboard
git add cmd/ go.mod go.sum
git commit -m "feat: add CLI entry point

Repo discovery, Bubble Tea program launch, version flag,
and async data loading."
```

---

### Task 10: Integration Test

**Files:**
- Create: `cmd/bigboard/main_test.go`

- [ ] **Step 1: Write an integration test for the full pipeline**

```go
// cmd/bigboard/main_test.go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rdh/bigboard/git"
	"github.com/rdh/bigboard/stats"
)

func TestFullPipeline(t *testing.T) {
	// Create a test repo with known data
	dir := t.TempDir()
	setupCmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "alice@example.com"},
		{"git", "config", "user.name", "Alice"},
	}
	for _, args := range setupCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s %v", args, out, err)
		}
	}

	// Alice makes 2 commits
	for i, content := range []string{"line1\nline2\nline3\n", "line1\nline2\nline3\nline4\nline5\n"} {
		file := filepath.Join(dir, "code.go")
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cmds := [][]string{
			{"git", "add", "."},
			{"git", "commit", "-m", "commit " + string(rune('A'+i))},
		}
		for _, args := range cmds {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v failed: %s %v", args, out, err)
			}
		}
	}

	// Run the pipeline
	repoPaths := git.DiscoverRepos([]string{dir})
	if len(repoPaths) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repoPaths))
	}

	branch := git.DetectDefaultBranch(repoPaths[0])
	records, err := git.CollectCommits(repoPaths[0], branch)
	if err != nil {
		t.Fatalf("CollectCommits: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	authors := stats.Aggregate(records)
	if len(authors) != 1 {
		t.Fatalf("expected 1 author, got %d", len(authors))
	}

	alice := authors[0]
	if alice.Name != "Alice" {
		t.Errorf("expected Alice, got %s", alice.Name)
	}
	if alice.Commits != 2 {
		t.Errorf("expected 2 commits, got %d", alice.Commits)
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd /Users/rdh/src/bigboard && go test ./cmd/bigboard/... -v`
Expected: PASS.

- [ ] **Step 3: Run all tests and quality checks**

Run: `cd /Users/rdh/src/bigboard && go test ./... -v`
Expected: All tests pass across all packages.

- [ ] **Step 4: Commit**

```bash
cd /Users/rdh/src/bigboard
git add cmd/bigboard/main_test.go
git commit -m "test: add integration test for full pipeline

End-to-end test covering repo discovery, commit collection,
and stats aggregation against a temp git repo."
```

---

### Task 11: Final Polish & README

**Files:**
- Create: `README.md`
- Create: `LICENSE`

- [ ] **Step 1: Create LICENSE (MIT)**

```
MIT License

Copyright (c) 2026 Rich Haase

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 2: Create README.md**

```markdown
# Big Board

A cyberpunk-themed TUI for assessing contributor volumes across git repositories. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

## Install

### Homebrew

```bash
brew install --cask richhaase/tap/bigboard
```

### From Source

```bash
go install github.com/richhaase/bigboard/cmd/bigboard@latest
```

## Usage

```bash
# Analyze current directory
bigboard

# Analyze specific repos
bigboard ~/src/repo1 ~/src/repo2

# Scan a directory for repos (one level deep)
bigboard ~/src/

# Set initial sort column
bigboard --sort commits ~/src/
```

## Controls

| Key | Action |
|-----|--------|
| `↑/↓` `j/k` | Navigate rows |
| `←/→` `h/l` | Cycle time range (30d / 90d / 1y / all) |
| `Enter` | Drill into selected repo |
| `Esc` | Back / Quit |
| `s` | Cycle sort column |
| `Tab` | Toggle focus (table / repo tags) |
| `q` | Quit |

## License

MIT
```

- [ ] **Step 3: Run make check**

Run: `cd /Users/rdh/src/bigboard && make check`
Expected: All checks pass (fmt, lint, vet, staticcheck, tests).

- [ ] **Step 4: Commit**

```bash
cd /Users/rdh/src/bigboard
git add README.md LICENSE
git commit -m "docs: add README and MIT license"
```
