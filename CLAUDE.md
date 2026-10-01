# Big Board - Claude Code Context

## Project Overview

High-contrast terminal dashboard for visualizing contributor statistics across multiple git repositories. Built with Go, Bubbletea, and Lipgloss.

## Architecture

```
cmd/bigboard/main.go    CLI entry (4 flags: --version/--config/--group/--export), config resolution, repo discovery
cmd/bigboard/config.go  JSON config (~/.config/bigboard/config.json): paths, excludes, groups, sort/since/depth, ai_identities/bot_identities
cmd/bigboard/export.go  Headless --export (JSON only, ALL window, concurrent scans)
git/git.go              Git ops: recursive discovery (follows symlinks), branch detection, streaming commit collection, path filtering, AI detection
stats/stats.go          Aggregation, identity merging, bot tagging, time/repo filtering, sorting, derived metrics
tui/app.go              Root Bubbletea model, view routing, keyboard handling, streaming loader, scroll/search state, bot toggle
tui/styles.go           Color palette and lipgloss style definitions
tui/worklist*.go        Responsive Worklist rows, selected evidence, and compact list→detail
tui/nightops.go         Shared high-contrast canvas for retained views
tui/components.go       Shared UI: banner, stat boxes, impact bars, help bar, footer, table state
tui/aggregate.go        Contributor leaderboard table (scrollable, AI% column, BOT tag)
tui/operativeview.go    Per-contributor detail: repo breakdown, gap-aware monthly timeline, neon heatmap, derived metrics
tui/repooverlay.go      Repo inclusion/exclusion toggle overlay
```

## Data Flow

1. `main.go` loads config (all preferences are config-only; the CLI has exactly 4 flags), picks scan paths (`--group` / args / config), then `git.DiscoverReposDepth(paths, depth)` (skips worktrees, follows symlinked dirs, dedupes on resolved path).
2. `--export` runs the pipeline headlessly (JSON, ALL window, 8-way concurrent scans) and exits; otherwise the TUI launches.
3. `Model.Init` streams bounded scan commands once on launch. Only explicit `R` requests start subsequent refreshes; there is no periodic refresh timer. Generation-tagged `RepoLoadedMsg` results accumulate into a pending snapshot, committed atomically when the batch finishes. Background updates preserve the visible last-good data and navigation; `R` refreshes immediately.
4. `recomputeAuthors()` → `filteredRecords()` (`FilterByRepo` → `FilterByTime`) → `Aggregate` (tags bots) → optional bot filter (`b`) → `Sort` (with ascending toggle).
5. Worklist renders scoped repository/area rows and selected evidence through `worklist*.go`; focused lenses share identity-preserving navigation in `glance*.go`. Statistics renders the scroll window of `displayedAuthors()` (sorted, optionally `/`-filtered).

## Key Design Decisions

- **Git CLI integration**: there is no Git library dependency; operations invoke the `git` executable, with timeouts and bounded concurrent repository scans. `git log` output is parsed as a stream (pipe + scanner), never buffered whole.
- **TUI-first CLI**: exactly 4 flags (`--version`, `--config`, `--group`, `--export`); every preference lives in the config file. `since` accepts only the TUI preset labels (1d/7d/14d/30d/90d/1y/all) so the initial window always matches a picker state.
- **In-memory filtering**: git log is collected once; all time/repo/search filtering is in-memory.
- **Identity merging**: canonical emails after Git `.mailmap` establish identity; names never merge contributors. Missing-email identities are repository-scoped. `fuzzy` is accepted for old configs but does not affect aggregation. Contributor selection follows the stable identity, not its current display name.
- **Path filtering**: generated/vendored files (lockfiles, `vendor/`, `node_modules/`, `*.min.*`, `go.sum`, …) are excluded from line counts by default; the `"all_files": true` config setting includes them.
- **Repository identity**: repositories are keyed by absolute path; duplicate basenames receive shortest-unique display labels such as `org-a/api` and `org-b/api`.
- **AI authorship**: detected from a `Co-authored-by` trailer or an AI author identity, including GitHub-noreply agent accounts (`Copilot`, `claude[bot]`, `devin-ai-integration[bot]`, …); extensible via `ai_identities` (exact email or `@domain`). Surfaced as a first-class metric (leaderboard `AI%`, per-month/per-repo share).
- **Bots are counted, not excluded**: bot identities (`[bot]` names/emails, builtin roster, `bot_identities` config) get `AuthorStats.Bot` and a leaderboard `BOT` tag; the `b` key toggles visibility (default shown). Agents that do their own work rank like any contributor.
- **Worktree detection**: compare Git directory and common directory to skip linked worktrees while retaining separate-Git-directory repositories. Symlinked directories are followed and deduplicated.
- **Counting**: deduplicate object IDs globally after repository/time filtering; repository subtotals overlap. Prefer known counts from full copies over unknown shallow boundaries. Shallow counts use `LinesUnknown` / `unknown_line_commits` and visible `?` qualifiers. Scans never lazily fetch missing objects. Cached remote default history precedes local history.
- **Dates and retained behavior**: use `time.Local` for all calendar grouping and display. Future-dated commits remain counted, with no upper time cutoff. Keep the current churn formula and its zero-additions `0.00` convention. Keep the existing UI, JSON export, and merge-commit exclusion.
- **Git isolation**: resolve branch OIDs, parse NUL-delimited paths, pin diff settings, clear repository-local environment overrides, and retain specific-agent AI matching with explicit user overrides.
- **Worklist awareness**: `tui/worklist*.go` renders a horizontal repo/area → last author + others → concrete commit → age list. At 110×28+ selected evidence fills the pane underneath; narrower terminals use multiline list rows and Enter replaces the list with detail. Recent is the default overview sort, independent of statistics sort. Ages use each row repository's own retained scan timestamp. Latest author, subject, and time come from the same deterministic commit (date, then object ID). All modes share a high-contrast blue-gray canvas; statistics retain their arrangement.

## Build & Test

```bash
go build ./cmd/bigboard
go test ./...
```

## CI

- GitHub Actions: `go test` (+ `-race`), `go vet`, `gofmt -l .` check, golangci-lint v2, `staticcheck`, `govulncheck`, `gosec`.

> Note: golangci-lint's bundled staticcheck enables the `QF*` quickfix checks that the standalone `staticcheck` binary leaves off by default — the Lint job is stricter than the Staticcheck job. Run `make lint` locally before pushing.

## Style Notes

- All visible UI strings use "contributor" (not "operative")
- Impact bars use gradient trailing glow: `████████▓▒░`
- Top 3 ranks styled gold/silver/bronze
- Negative net values rendered in red
- Worklist uses restrained rules and one selected row; retained statistics and inspector section headers use `──╸ LABEL ╺──` styling
- Use terminal-cell widths for truncation and alignment; preserve explicit stale/partial/unknown labels before clipping
- No animation: the separator is a static rule, and the loading screen uses plain language (no sci-fi flavor)

## Automatic PR context

GitHub PR context loads automatically with existing gh authentication for supported origins. There is no feature toggle. Missing gh/authentication leaves local history usable with a visible status. `github/` owns a bounded read-only provider through existing gh authentication, with strict github.com origins and no credential reads. `tui/pullrequests.go` owns in-memory async snapshots and the p/P overlay. PR handles/data never enter CommitRecord, contributor stats, or export. Initial loading and explicit `R` request GitHub context; active remote refreshes coalesce and completed snapshots are reused for at least one minute. Excluded repositories are skipped, each refresh shares a 128-call cap, and provider-wide quota cooldown survives cancellation. Retry-After/reset times are respected; missing timing uses bounded exponential cooldown. No polling, automatic retries, persistent cache, or credential storage. Local refreshes do not cancel or restart GitHub requests. Complete empty success clears while failures/partial snapshots retain visible stale evidence. `stats.WorkAreaDefinition.ClassifyPaths` maps PR evidence without synthetic commits.

## Progressive disclosure

`tui/worklist*.go` owns awareness presentation; `tui/glance*.go` owns focused Activity/People/Related/
Subareas navigation. `s` cycles Recent/Name/Activity (commits) overview or Subareas sorts, separate from
aggregate contributor statistics. Scope/person/commit identity survives navigation
and sorting; stale or filtered-out detail must not silently switch to a new area.
`stats.Subareas` refines automatic areas by literal child directories and direct
files, without changing configured grouping or canonical identities. Area/subarea
commit counts overlap. `tui/prglance.go` summarizes only explicit PR evidence;
p/P keeps the parent area/all-open scope, independent of local filters.
