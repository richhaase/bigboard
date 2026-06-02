# Big Board - Claude Code Context

## Project Overview

Cyberpunk-themed terminal dashboard for visualizing contributor statistics across multiple git repositories. Built with Go, Bubbletea, and Lipgloss.

## Architecture

```
cmd/bigboard/main.go    CLI entry, flag + config resolution, repo discovery, export dispatch
cmd/bigboard/config.go  JSON config (~/.config/bigboard/config.json): default paths, excludes, groups, sort/since/depth
cmd/bigboard/export.go  Headless --export json|csv|md (runs the pipeline without the TUI)
git/git.go              Git ops: recursive discovery, branch detection, commit collection, path filtering, AI detection
stats/stats.go          Aggregation, identity merging, time/repo filtering, sorting, derived metrics
tui/app.go              Root Bubbletea model, view routing, keyboard handling, streaming loader, scroll/search state
tui/styles.go           Color palette and lipgloss style definitions
tui/components.go       Shared UI: banner, stat boxes, impact bars, help bar, footer, table state
tui/aggregate.go        Contributor leaderboard table (scrollable, AI% column)
tui/operativeview.go    Per-contributor detail: repo breakdown, monthly timeline, neon heatmap, derived metrics
tui/repooverlay.go      Repo inclusion/exclusion toggle overlay
```

## Data Flow

1. `main.go` resolves config + flags, picks scan paths (`--group` / args / config), then `git.DiscoverReposDepth(paths, depth)` (skips worktrees).
2. `--export` runs the pipeline headlessly and exits; otherwise the TUI launches.
3. `Model.Init` streams one scan command per repo; each emits a `RepoLoadedMsg` (driving the live scan log) and accumulates into `Model.allRecords` (in-memory; refetched only on `R` or `--watch`).
4. `recomputeAuthors()` → `filteredRecords()` (`FilterByRepo` → `FilterByTime`) → `Aggregate` → `Sort` (with ascending toggle).
5. View renders the scroll window of `displayedAuthors()` (sorted, optionally `/`-filtered).

## Key Design Decisions

- **No runtime git dependencies**: all git operations use `os/exec` calling `git`, with a per-repo timeout so a hung/huge repo can't block the whole load.
- **In-memory filtering**: git log is collected once; all time/repo/search filtering is in-memory.
- **Identity merging**: group by email, then exact normalized name; git's native `.mailmap` is honored (`%aN`/`%aE`). Substring fuzzy matching is **opt-in** (`--fuzzy`) because it over-merges distinct people. Output ordering is deterministic (sort tiebreaks; no map-iteration leaks).
- **Path filtering**: generated/vendored files (lockfiles, `vendor/`, `node_modules/`, `*.min.*`, `go.sum`, …) are excluded from line counts by default; `--all-files` includes them.
- **AI authorship**: detected from a `Co-authored-by` trailer or an AI author identity; surfaced as a first-class metric (leaderboard `AI%`, per-month/per-repo share).
- **Worktree detection**: `isWorktree()` checks if `.git` is a file containing `gitdir:` — skips these during discovery to avoid double-counting.
- **Banner rendering**: figlet banner3 font with `#` → `█`, 7-line vertical color gradient, compact fallback for terminals < 82 cols.

## Build & Test

```bash
go build ./cmd/bigboard
go test ./...
```

## CI

- GitHub Actions: `go test` (+ `-race`), `go vet`, `gofmt -l .` check, golangci-lint v2, `staticcheck`, `govulncheck`, `gosec`.
- GoReleaser for releases (`.goreleaser.yaml`); tag push (`vX.Y.Z`) publishes binaries + updates the Homebrew tap.

> Note: golangci-lint's bundled staticcheck enables the `QF*` quickfix checks that the standalone `staticcheck` binary leaves off by default — the Lint job is stricter than the Staticcheck job. Run `make lint` locally before pushing.

## Style Notes

- All visible UI strings use "contributor" (not "operative")
- Impact bars use gradient trailing glow: `████████▓▒░`
- Top 3 ranks styled gold/silver/bronze
- Negative net values rendered in red
- Section headers in detail view use `──╸ LABEL ╺──` style
- Heavy separator (`━`) between major sections
- No animation: the separator is a static rule, and the loading screen uses plain language (no sci-fi flavor)
