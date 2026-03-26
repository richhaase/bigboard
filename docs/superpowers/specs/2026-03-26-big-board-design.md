# Big Board — Design Spec

A cyberpunk-themed TUI for assessing contribution volumes across git repositories. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

## Overview

Big Board is an on-demand, interactive terminal application that analyzes one or more git repositories and displays contributor statistics in a full-screen cyberpunk-styled TUI. It replaces the existing `leaderboard.py` CLI tool with a richer, more visual experience built as a single Go binary.

## Core Decisions

- **Language:** Go
- **TUI framework:** Bubble Tea + Lip Gloss + Bubbles (Charm.sh stack)
- **Output:** Interactive full-screen TUI (not static output)
- **Distribution:** Single binary via GoReleaser, Homebrew cask
- **Data source:** Local git history only (v1). GitHub PR/review data is a future enhancement (see gh-prboard for prior art).

## CLI Interface

```
bigboard [paths...]

Arguments:
  paths    Git repos or directories containing repos (default: .)
           - If a path is a git repo, use it directly
           - If a path is a directory, scan one level deep for git repos

Flags:
  --sort   Initial sort column: commits|added|removed|net|total (default: total)
```

No `--ref`, `--since`, or `--reverse` flags — those are handled interactively in the TUI.

## Architecture

```
bigboard/
├── cmd/bigboard/
│   └── main.go           CLI entry point, arg parsing, version info
├── git/
│   └── git.go            Git command execution, per-repo stats extraction
├── stats/
│   └── stats.go          Aggregation, author merging, sorting, time filtering
├── tui/
│   ├── app.go            Root Bubble Tea model, key bindings, view switching
│   ├── aggregate.go      Aggregate leaderboard view
│   ├── repoview.go       Per-repo drill-down view
│   ├── styles.go         Lip Gloss cyberpunk theme
│   └── components.go     Reusable: stat boxes, bar charts, header, time picker
├── Makefile
├── .goreleaser.yaml
├── .golangci.yml
├── .github/
│   └── workflows/
│       ├── ci.yml
│       ├── release.yml
│       └── security.yml
└── .github/dependabot.yml
```

### Data Flow

1. CLI parses args, walks paths to discover git repos
2. `git/` runs git commands per repo concurrently (goroutines + channels), returns raw commit records
3. `stats/` merges author names (fuzzy match), holds all records in memory
4. `tui/` receives data, renders views. Time range and sort changes re-filter/re-sort in-memory data — no re-running git commands.

## Data Collection

### Single-Pass Git Extraction

Each repo is queried once on startup with:

```
git log <ref> --format='%aN|%aI' --numstat
```

This yields author name, ISO timestamp, and per-file add/remove counts in a single pass. One commit touching N files produces N numstat lines, all sharing the same author/date header. Binary files (shown as `-` in numstat) are skipped.

### Data Model

```go
// One record per commit (aggregated from its numstat lines)
type CommitRecord struct {
    Author    string
    Date      time.Time
    Added     int
    Removed   int
    Files     int
    RepoName  string
}
```

All commit records are held in memory. Time range changes filter this slice; sort changes re-sort the aggregated results. No git commands after initial load.

### Branch Detection

Auto-detect each repo's default branch:
1. `git symbolic-ref refs/remotes/origin/HEAD` → extract branch name
2. Fall back to `main`, then `master`
3. Final fallback: `HEAD`

### Author Merging

Silent, automatic fuzzy matching:
- Case-insensitive comparison
- Whitespace normalization
- Substring matching for names >5 characters
- Canonical name chosen by highest commit count across all repos

### Concurrency

Repos are analyzed concurrently via goroutines. Results collected via channels. A loading spinner is shown in the TUI while data is being collected.

## TUI Design

### Aggregate View (Home Screen)

```
┌──────────────────────────────────────────────────────────┐
│  ⟐ BIG BOARD ⟐                                          │
│  // CONTRIBUTOR INTELLIGENCE SYSTEM                      │
│  ════════════════════════════════════════════════════════ │
│  [30d] [90d] [1y] [ALL]              3 repos scanned     │
│                                                          │
│  ┌─COMMITS──┐ ┌─ADDED────┐ ┌─REMOVED───┐                │
│  │   847    │ │  142,891  │ │   89,104  │                │
│  └──────────┘ └──────────-┘ └───────────┘                │
│                                                          │
│  [backend] [frontend] [infra]                            │
│                                                          │
│  #  OPERATIVE    COMMITS  ADDED    REMOVED   IMPACT      │
│  ─────────────────────────────────────────────────────── │
│  01 rdh          312      48,291   22,108   █████████    │
│  02 sarah.k      198      31,442   18,903   ███████      │
│  03 mchen        156      28,107   24,661   ██████       │
│  ...                                                     │
│                                                          │
│  [q]uit  [s]ort  [↵]drill  [←→]time         ref: main   │
└──────────────────────────────────────────────────────────┘
```

### Repo Drill-Down View

Same layout, scoped to a single repo. Header shows repo name. `Esc` returns to aggregate.

### Navigation & Key Bindings

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate contributor rows |
| `←/→` or `h/l` | Cycle time range presets (30d / 90d / 1y / all) |
| `Enter` on repo tag | Drill into that repo's stats |
| `Esc` | Back to aggregate (from drill-down) or quit |
| `s` | Cycle sort column (commits / added / removed / net / total) |
| `Tab` | Toggle focus between repo tags and contributor table |
| `q` | Quit |

### Cyberpunk Aesthetic

- **Background:** Deep dark (#050510)
- **Primary accent:** Cyan (#00FFFF) — headings, borders, active elements, glow effects
- **Secondary accent:** Magenta (#FF00FF) — repo tags, secondary highlights
- **Data values:** Green (#00FF88) — numeric values
- **Impact bars:** Cyan→Magenta gradient
- **Scanline effect:** Alternating row opacity for CRT feel
- **Header:** Neon glow text-shadow effect
- **Footer:** System-status-style metadata display
- **Typography:** Monospace throughout
- **Glitch line:** Animated gradient divider below header

## Build & Automation

Following patterns from richhaase/agentic-code-reviewer:

### Makefile

Targets: `help`, `build`, `test`, `test-coverage`, `fmt`, `lint`, `vet`, `tidy`, `clean`, `staticcheck`, `check` (runs all quality checks).

Build embeds version info via ldflags (`-X main.version`, `-X main.commit`, `-X main.date`).

### CI (.github/workflows/ci.yml)

Parallel jobs on push/PR to main:
- Format check (gofmt)
- Unit tests with coverage
- Race detection tests
- golangci-lint v2
- staticcheck
- go vet
- govulncheck

### Release (.github/workflows/release.yml)

GoReleaser on tag push:
- Multi-platform builds (Linux/Darwin, AMD64/ARM64)
- CGO_ENABLED=0
- SHA256 checksums
- GitHub releases with changelog
- Homebrew cask distribution via richhaase/homebrew-tap
- macOS signing/notarization (when certs available)

### Security (.github/workflows/security.yml)

Daily + push/PR:
- govulncheck
- gosec

### Dependabot

Weekly updates for gomod and github-actions.

### Linting (.golangci.yml)

Explicit enable list: govet, ineffassign, misspell, unused, errcheck, staticcheck, gosec, nolintlint. Standard exclusions for test files and safe operations.

## Future Enhancements (Out of Scope for v1)

- **GitHub PR/review data integration** — review counts, review turnaround time, PR throughput per contributor. Prior art in richhaase/gh-prboard (GraphQL API, batched queries, review state classification).
- **Enter on contributor row** — drill into per-contributor detail view (repos contributed to, activity timeline)
- **Config file** — persistent repo groups, author aliases
- **Export** — dump current view as JSON/CSV
