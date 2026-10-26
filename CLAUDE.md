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
tui/worklist*.go        Responsive Activity rows, selected evidence, and compact list→detail
tui/canvas.go         Shared high-contrast canvas for retained views
tui/frame.go            Shared title band, scope/freshness rows, pinned contextual footer
tui/components.go       Shared UI: banner, stat boxes, impact bars, help bar, footer, table state
tui/help.go             Shared contextual help modal, keyboard capture, and terminal-sized overlay
tui/aggregate.go        Contributors table (scrollable, AI% column, BOT tag)
tui/operativeview.go    Per-contributor detail: repo breakdown, gap-aware monthly timeline, neon heatmap, derived metrics
tui/repooverlay.go      Repo inclusion/exclusion toggle overlay
```

## Data Flow

1. `main.go` loads config (all preferences are config-only; the CLI has exactly 4 flags), picks scan paths (`--group` / args / config), then `git.DiscoverReposDepth(paths, depth)` (skips worktrees, follows symlinked dirs, dedupes on resolved path).
2. `--export` runs the pipeline headlessly (JSON, ALL window, 8-way concurrent scans) and exits; otherwise the TUI launches.
3. `Model.Init` streams bounded fetch-and-scan commands once on launch. Only explicit `r` requests start subsequent refreshes; there is no periodic refresh timer. Generation-tagged `RepoLoadedMsg` results accumulate into a pending snapshot, committed atomically when the batch finishes. Background updates preserve the visible last-good data and navigation; startup and `r` fetch included repositories from `origin` before scanning. Export uses cached history.
4. `recomputeAuthors()` → `filteredRecords()` (`FilterByRepo` → `FilterByTime`) → `Aggregate` (tags bots) → optional bot filter (`b`) → `Sort` (with ascending toggle).
5. Activity renders ranked repositories and literal directories/files through `worklist*.go`; scoped Commits/People/Related lenses share identity-preserving navigation in `glance*.go`. Statistics renders the scroll window of `displayedAuthors()` (sorted, optionally `/`-filtered).

## Key Design Decisions

- **Git CLI integration**: there is no Git library dependency; operations invoke the `git` executable, with timeouts and bounded concurrent repository scans. `git log` output is parsed as a stream (pipe + scanner), never buffered whole.
- **TUI-first CLI**: exactly 4 flags (`--version`, `--config`, `--group`, `--export`); every preference lives in the config file. `since` accepts only the TUI preset labels (1d/7d/14d/30d/90d/1y/all) so the initial window always matches a picker state. The `t` range picker also supports session-only custom day counts (1–3650), with Enter apply/Esc cancel; it filters in memory without scans or fetches. Left/right arrows navigate back/drill in, never change time; back at any board root stays there. Navigation uses arrows, Enter/Esc, Home/End and PageUp/PageDown where supported; there are no Vim movement aliases. Lowercase `c`, `a`, and `p` directly select Contributors, Activity, and all-included PR board from any page. These keys close help, inspectors, and dialogs; pending repository and range choices cancel on departure. Searches retain literal letters while editing, and board selections/filters survive switching.
- **In-memory filtering**: git log is collected once; all time/repo/search filtering is in-memory.
- **Identity merging**: canonical emails after Git `.mailmap` establish identity; names never merge contributors. Missing-email identities are repository-scoped. `fuzzy` is accepted for old configs but does not affect aggregation. Contributor selection follows the stable identity, not its current display name.
- **Path filtering**: generated/vendored files (lockfiles, `vendor/`, `node_modules/`, `*.min.*`, `go.sum`, …) are excluded from line counts by default; the `"all_files": true` config setting includes them.
- **Repository identity**: repositories are keyed by absolute path; duplicate basenames receive shortest-unique display labels such as `org-a/api` and `org-b/api`.
- **AI authorship**: detected from a `Co-authored-by` trailer or an AI author identity, including GitHub-noreply agent accounts (`Copilot`, `claude[bot]`, `devin-ai-integration[bot]`, …); extensible via `ai_identities` (exact email or `@domain`). Surfaced as a first-class metric (Contributors `AI%`, per-month/per-repo share).
- **Bots are counted, not excluded**: bot identities (`[bot]` names/emails, builtin roster, `bot_identities` config) get `AuthorStats.Bot` and a Contributors `BOT` tag; the `b` key toggles visibility (default shown). Agents that do their own work rank like any contributor.
- **Worktree detection**: compare Git directory and common directory to skip linked worktrees while retaining separate-Git-directory repositories. Symlinked directories are followed and deduplicated.
- **Counting**: deduplicate object IDs globally after repository/time filtering; repository subtotals overlap. Prefer known counts from full copies over unknown shallow boundaries. Shallow counts use `LinesUnknown` / `unknown_line_commits` and visible `?` qualifiers. Scans never lazily fetch missing objects. Cached remote default history precedes local history.
- **Dates and retained behavior**: use `time.Local` for all calendar grouping and display. Future-dated commits remain counted, with no upper time cutoff. Keep the current churn formula and its zero-additions `0.00` convention. Keep the existing UI, JSON export, and merge-commit exclusion.
- **Repository refresh**: Startup and `r`, including in the PR board, fetch remote-tracking branches only with existing Git credentials, no tags, submodules, local branch updates, or working-tree changes. Fetches share the eight scan slots, have a two-minute timeout, and disable Git/SSH credential prompts. Repositories without `origin` stay local-only; excluded repositories are never fetched. A fetch failure still runs the offline scan and marks its result stale with visible diagnostics.
- **Contextual help**: `?` or `F1` opens a centered, scrollable help modal over every screen. Search input retains literal `?` and uses `F1` for help. Help owns its scrolling/dismissal keys; `c`/`a`/`p` leave help for a board. Ordinary dismissal preserves the underlying selection, filters, and overlays.
- **Shared screen frame**: Activity, Contributors/detail, and PR list/detail use the same compact title band, scope/search row, local/PR freshness rows, and pinned three-row footer. Main lists align their heading (sort, summary, visible range), column labels, and first row. Contributors totals use the heading, and narrow contributor rows stack metrics. PR rows reserve review/check/updated fields and stack at 80/40 columns. At 70×24+ a selected-PR preview above the footer shows wrapped title, full repo identity, author, draft, and review/check states; wide tables move the author there. Short repo labels retain owners when ambiguous; Activity area rows label their known-PR column; repository and directory rows use a full-height ranking with added/removed bars and no selected preview. Footer hints omit whole optional commands; coverage contains no commands. Wide titles include repository counts and version. The status rows report global included-repository freshness on every screen; selected repository timestamps stay in Activity evidence. The title count includes failed repositories, and tables share full-width gutters and selection rows. The oldest included local scan and stale/missing coverage stay explicit. The PR board follows the global range using UpdatedAt, while local person/bot filters remain independent. Work-area PR summaries and snapshot counts retain all open PRs. Contributor paging scrolls only the body, preserving its frame, and `r` refreshes without leaving detail.
- **Git isolation**: resolve branch OIDs, parse NUL-delimited paths, pin diff settings, clear repository-local environment overrides, and retain specific-agent AI matching with explicit user overrides.
- **Activity awareness**: Repository and directory rows use the full list height to show where change occurred, defaulting to added + removed lines with split bars. Bars retain that meaning when sorting by commits, recency, or name; there is no measure toggle. Enter follows literal directories and files, then commit evidence. Directory totals use retained per-file Git numstat counts, excluding generated paths according to scan options; whole-commit totals must not be repeated for every touched directory. Quiet repositories remain visible and unknown line counts retain `?` qualifiers. Date and bot filters apply at each scope. Secondary commit, People, and Related evidence remains available. Configured named areas retain their existing meaning separately from the literal directory hierarchy. Focused commit evidence fills the available height and pages by visible records. Ages use each row repository's retained scan timestamp. All modes share a high-contrast blue-gray canvas.


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
- Activity uses restrained rules and one selected row; retained statistics and inspector section headers use `──╸ LABEL ╺──` styling
- Use terminal-cell widths for truncation and alignment; preserve explicit stale/partial/unknown labels before clipping
- No animation: the separator is a static rule, and the loading screen uses plain language (no sci-fi flavor)

## Automatic PR context

GitHub PR context loads automatically with existing gh authentication for supported origins. There is no feature toggle. Missing gh/authentication leaves local history usable with a visible status. `github/` owns a bounded read-only provider through existing gh authentication, with strict github.com origins and no credential reads. `tui/pullrequests.go` owns in-memory async snapshots and the all-included PR board. PR handles/data never enter CommitRecord, contributor stats, or export. Initial loading and explicit `r` request GitHub context; active remote refreshes coalesce and completed snapshots are reused for at least one minute. Excluded repositories are skipped, each refresh shares a 128-call cap, and provider-wide quota cooldown survives cancellation. Retry-After/reset times are respected; missing timing uses bounded exponential cooldown. No polling, automatic retries, persistent cache, or credential storage. Local refreshes do not cancel or restart GitHub requests. Complete empty success clears while failures/partial snapshots retain visible stale evidence. `stats.WorkAreaDefinition.ClassifyPaths` maps PR evidence without synthetic commits.

## Progressive disclosure

`tui/worklist*.go` owns repository and directory rankings. Enter follows literal child directories, then files and commit evidence. `s` cycles lines changed, commits, recency, and name; bars always show added/removed lines. Tab or `1`–`3` opens the current scope's Commits/People/Related evidence; `4` returns to its directory ranking. Esc clears a search before returning to the parent with selection preserved. Directory line totals are computed from retained per-file counts. Commit counts can overlap across directories. Configured named areas remain secondary related evidence and never replace the literal directory hierarchy. `p` opens all included PRs; `v` opens PRs for the selected repository.

## Related evidence

`worklist_related.go` separates exact nonempty shared commit IDs from canonical
contributor overlap. Related Enter opens shared commits; `m` exposes a scrollable
name-overlap list whose Enter opens identity-filtered People/Activity. These views show existing people, area, time,
and commit evidence. Shared PR unavailability belongs once in the
header; retained scope-specific PR signals remain available. Commit previews show
titles first, then author/time/hash and affected areas. No extra scans or requests.

## Shared command contract

Lowercase `c`/`a`/`p` select peer boards; `p` clears PR repository scope. Back
clears filters or unwinds one level and stays at roots. `q` always quits outside
text entry; Ctrl+C always quits. Enter/right opens; left/Esc backs out. All
scrollable lists and details support arrows, paging, Home/End. Contributor-detail
arrows scroll without switching identity. Searches reopen their existing text.
`i` opens global inclusion from all boards and details; Enter applies and Esc
cancels. Pending dialogs cancel on board switches. `r` refreshes from every
board/detail, including commit inspection, preserving navigation.
Uppercase `R` remains a refresh compatibility alias. Footers group board keys,
primary contextual actions, and help/refresh; complete shortcuts stay in `?` help.
Repository errors use the shared centered modal surface over the retained board;
`d` toggles full diagnostics beneath per-repository summaries, with scrolling,
`r` retry, and Esc dismissal preserving board state.

PRs use `f` repository, `u` author, `v` review, `k` checks, and `d` draft/open
filter modals over the retained PR board/detail. Tabs highlight the active category;
the cursor and applied-value checkmark are separate. Paging uses the modal viewport.
Filters compose with `/` repo/title/author/number search; picker
`/` searches options, Tab switches categories, Enter applies, and Esc cancels.
`x` clears all PR filters and search. Wide rows separate number/state/title;
`◇` and subdued titles mark drafts, `●` marks non-drafts, and approved/passing
are green without implying merge readiness.
PR details are centered modals over the retained list, with labeled status and
evidence sections. Identity, snapshot warnings, browser errors, and controls stay
pinned; scrolling and End use the modal's wrapped body budget.
`s` cycles Updated/Repository/Author/Title and `S` reverses. Activity's
`v` enters the same PR board scoped to its selected repository without altering
inclusion. PR scope and search never enter local statistics. `t` changes the shared range from PR list/detail too; presets and custom days filter UpdatedAt in memory, with unknown dates visible only in All time. Range changes preserve PR identity when visible and close detail if its PR leaves the range. Activity overview
and Subareas also support `S`; Related uses `m` to switch shared commits and
contributor overlap. `o` remains the external PR-opening action.
