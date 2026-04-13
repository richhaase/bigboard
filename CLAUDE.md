# Big Board - Claude Code Context

## Project Overview

Cyberpunk-themed terminal dashboard for visualizing contributor statistics across multiple git repositories. Built with Go, Bubbletea, and Lipgloss.

## Architecture

```
cmd/bigboard/main.go    CLI entry, flag parsing, repo discovery
git/git.go              Git operations: repo discovery, branch detection, commit collection
stats/stats.go          Aggregation, filtering, sorting, fuzzy author merging
tui/app.go              Root Bubbletea model, view routing, keyboard handling
tui/styles.go           Color palette and lipgloss style definitions
tui/components.go       Shared UI components: banner, stat boxes, impact bars, footer
tui/aggregate.go        Contributor leaderboard table view
tui/operativeview.go    Per-contributor detail view (repo breakdown + timeline)
tui/repooverlay.go      Repo inclusion/exclusion toggle overlay
```

## Data Flow

1. `main.go` → `git.DiscoverRepos(paths)` scans for repos (skips worktrees)
2. `tui.LoadDataCmd` → concurrent goroutine per repo → `git.CollectCommits`
3. All commits stored in `Model.allRecords` (in-memory, never re-fetched)
4. `recomputeAuthors()` → `FilterByRepo` → `FilterByTime` → `Aggregate` → `Sort`
5. View renders from `Model.authors` (filtered/sorted slice)

## Key Design Decisions

- **No runtime git dependencies**: all git operations use `os/exec` calling `git`
- **In-memory filtering**: git log is collected once; all time/repo filtering is in-memory
- **Fuzzy author merging**: `stats.Aggregate` groups by email first, then fuzzy-matches names
- **Worktree detection**: `isWorktree()` checks if `.git` is a file containing `gitdir:` — skips these during discovery to avoid double-counting
- **Banner rendering**: figlet banner3 font with `#` → `█`, 7-line vertical color gradient, compact fallback for terminals < 82 cols

## Build & Test

```bash
go build ./cmd/bigboard
go test ./...
```

## CI

- GitHub Actions: `go test`, `go vet`, `gofmt -l .` check, `staticcheck`
- GoReleaser for releases (`.goreleaser.yaml`)

## Style Notes

- All visible UI strings use "contributor" (not "operative")
- Impact bars use gradient trailing glow: `████████▓▒░`
- Top 3 ranks styled gold/silver/bronze
- Negative net values rendered in red
- Section headers in detail view use `──╸ LABEL ╺──` style
- Heavy separator (`━`) between major sections
