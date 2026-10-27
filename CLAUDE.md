# Big Board - Claude Code Context

## Project Overview

High-contrast terminal dashboard for visualizing contributor statistics across multiple git repositories. Built with Go, Bubbletea, and Lipgloss.

## Architecture

```
cmd/bigboard/main.go    CLI entry (4 flags: --version/--config/--group/--export), config resolution, repo discovery
cmd/bigboard/config.go  JSON config (~/.config/bigboard/config.json): personal account, organizations, excludes, owner/name groups, sort/since, identities
cmd/bigboard/repositories.go  Repository-source validation, discovery, group selection, offline cache inventory
cmd/bigboard/settings.go  Setup/settings callbacks, saved repository selection, synchronized cache inventory
cmd/bigboard/identity_config.go  Shared atomic config store for settings and identity mappings
cmd/bigboard/export.go  Headless --export (JSON only, ALL window, concurrent scans)
git/git.go              Git ops: branch detection, streaming commit collection, path filtering, AI detection
git/cache.go            Owned full bare default-branch cache: clone/fetch, integrity/ownership checks, inventory and removal
git/scan_cache.go       Disposable parsed Git scans keyed by revision, analysis options, identity/configuration inputs and object metadata
github/discovery.go    Bounded organization repository discovery through gh; archived repositories omitted
stats/stats.go          Aggregation, identity merging, bot tagging, time/repo filtering, sorting, derived metrics
stats/selection.go      Canonical commit selection with original repository membership and identity filtering
stats/calendar.go       Pure daily/monthly activity projections with explicit timezone inputs
tui/app.go              Root Bubbletea model, command orchestration, view routing, keyboard handling, scroll/search state
tui/local_scan.go       Committed local snapshots and bounded scan batches: admission, retention, atomic publication
tui/styles.go           Color palette and lipgloss style definitions
tui/worklist*.go        Responsive Activity rows, selected evidence, and compact list→detail
tui/canvas.go         Shared high-contrast canvas for retained views
tui/frame.go            Shared title band, scope/freshness rows, pinned contextual footer
tui/board_layout.go     Shared headings and width-aware fitting of explicit footer hints
tui/components.go       Shared UI: banner, stat boxes, impact bars, help bar, footer, table state
tui/help.go             Shared contextual help modal, keyboard capture, and terminal-sized overlay
tui/aggregate.go        Contributors table (scrollable, AI% column, BOT tag)
tui/operativeview.go    Per-contributor detail: repo breakdown, range-aware timeline, heatmap, derived metrics
tui/repooverlay.go      Centered repository controls and explicit unlisted-cache removal confirmation
tui/repository_cache.go  Cache status and ownership-checked removal command orchestration
```

## Data Flow

1. `main.go` loads config (all preferences are config-only; the CLI has exactly 4 flags). At least one of `personal_account` or `organizations` is required; `groups` contains canonical `owner/name` identities within that combined owner scope. Legacy `paths`, `depth`, path-based groups, and positional paths are rejected without fallback. Interactive startup discovers accessible non-archived repositories owned by the selected personal account and organizations with the existing authenticated `gh` CLI, then applies group selection and excludes. Missing configuration or repository sources opens required first-run setup; discovery failures open setup with a retryable error. Missing explicit `--config` paths can be created only by confirmed setup saves. Invalid and legacy configurations remain errors.
2. `--export` requires an existing valid configuration and skips discovery, clone, and fetch, selects verified existing cached repositories, and runs the pipeline headlessly (JSON, ALL window, 8-way concurrent scans), then exits. Empty cache selection reports an error. Without `--export`, the TUI launches.
3. `Model.Init` first scans existing cached history offline and publishes that snapshot before fetching. Verified owned caches reuse persisted parsed records when scan inputs match; misses and cache failures use the existing local scan. This changes neither snapshot check times nor fetch/bookmark rules. Before the second pass, bounded aliased GitHub probes check default branch names/OIDs and open PR counts, sharing the PR request budget and cooldown. Matching branches skip fetch only when the owned cache passes ownership/layout checks and holds exactly the expected default-branch refs at the probed revision; unknown results retain normal refresh/repair. The second bounded pass clones/fetches tracked repositories when needed and rescans only moved default-branch revisions or entries without a usable scan; unchanged entries reuse their offline records and scan times. With no usable caches, the boot screen stays visible through cloning. `scanBatch` admits generation-tagged `RepoLoadedMsg` results and publishes one atomic `localSnapshot` per pass. Only explicit `r` starts subsequent fetch-then-scan refreshes; there is no periodic refresh timer. The batch owns result admission and retention rules; the model owns commands, lifecycle, and navigation reconciliation. Refreshes preserve visible last-good data and navigation.
4. `recomputeAuthors()` → `filteredRecords()` (`FilterByRepo` → `FilterByTime`) → `Aggregate` (tags bots) → optional bot filter (`b`) → `Sort` (with ascending toggle).
5. Activity renders ranked repositories and literal directories/files through `worklist*.go`; scoped Commits/People/Related lenses share identity-preserving navigation in `glance*.go`. Statistics renders the scroll window of `displayedAuthors()` (sorted, optionally `/`-filtered).

## Key Design Decisions

- **Git CLI integration**: there is no Git library dependency; operations invoke the `git` executable, with timeouts and bounded concurrent repository scans. `git log` output is parsed as a stream (pipe + scanner), never buffered whole.
- **TUI-first CLI**: exactly 4 flags (`--version`, `--config`, `--group`, `--export`); every preference lives in the config file. `since` accepts only the TUI preset labels (1d/7d/14d/30d/90d/1y/all) so the initial window always matches a picker state. The shared filter modal, opened by `f` or directly at Range by `t`, also supports session-only custom day counts (1–3650), with Space staging choices and Enter applying all pending changes; Esc cancels; it filters in memory without scans or fetches. Left/right arrows navigate back/drill in outside dialogs; back at any board root stays there. Navigation uses arrows, Enter/Esc, Home/End and PageUp/PageDown where supported; there are no Vim movement aliases. Lowercase `c`, `a`, and `p` resume Contributors, Activity, and the normal PR board from any page. These keys close temporary dialogs and cancel pending choices on departure while preserving stable board detail locations. Searches retain literal letters while editing, and board selections/filters survive switching.
- **In-memory filtering**: git log is collected once; all time/repo/search filtering is in-memory.
- **Identity merging**: canonical emails after Git `.mailmap` and configured `identities` aliases establish identity; names never merge contributors. The config maps canonical emails to aliases and optional display names, validates unique normalized emails, and applies mappings once during collection before author AI classification. Co-author trailer evidence is preserved, and bot classification sees the canonical identity. TUI and export share these records. Missing-email identities are repository-scoped. `fuzzy` is accepted for old configs but does not affect aggregation. Contributor selection follows the stable identity, not its current display name.
- **Path filtering**: generated/vendored files (lockfiles, `vendor/`, `node_modules/`, `*.min.*`, `go.sum`, …) are excluded from line counts by default; the `"all_files": true` config setting includes them.
- **Repository identity**: the application keys repositories by canonical GitHub `owner/name`, independently of their cache paths. Excludes match a basename, `owner/name`, or a glob of either; a basename applies across personal and organization repositories. Work-area config uses the same `owner/name` identity.
- **AI authorship**: detected from a `Co-authored-by` trailer or an AI author identity, including GitHub-noreply agent accounts (`Copilot`, `claude[bot]`, `devin-ai-integration[bot]`, …); extensible via `ai_identities` (exact email or `@domain`). Surfaced as a first-class metric (Contributors `AI%`, per-month/per-repo share).
- **Bots are counted, not excluded**: bot identities (`[bot]` names/emails, builtin roster, `bot_identities` config) get `AuthorStats.Bot` and a Contributors `BOT` tag; the `b` key toggles visibility (default shown). Agents that do their own work rank like any contributor.
- **Owned repository cache**: `$XDG_CACHE_HOME/bigboard/repos/github.com/owner/name.git`, defaulting to `~/.cache/bigboard/repos`, contains full bare default-branch-only repositories. Developer repositories and worktrees are never discovered, scanned, or modified. Cache verification requires the ownership marker, matching GitHub origin, and bare layout; it rejects symlinks, alternates, shallow/partial clones, and unsafe repository configuration. There is no `git fsck`: `git fetch` checks connectivity of received objects, and scans never lazily fetch, so a Git scan that reads later missing or corrupt objects fails. A narrow classifier recognizes Git object-damage errors (not cancellation, timeouts, or unrelated failures), writes an ownership-checked `bigboard-damaged` marker, drops the parsed scan cache, and reports the repository stale as damaged. Verification fails on the marker, so the next startup fetch pass or `r` rebuilds the cache through the verified replacement clone, even when the probe matches; inventory, export, and removal still accept a marked cache, and `--export` only reports the damage. A rebuild removes its replacement backup after publishing; inventory deletes `.bigboard-replaced-*`/`.bigboard-clone-*` directories under owner directories once they are older than twice the Git timeout and otherwise ignores them. Data status reports location, size, unlisted cache entries, and problems. Unlisted entries are retained until Repository controls `d` then Enter explicitly removes a verified cache entry after refresh finishes. Group selection and exclusions do not orphan listed repositories.
- **Counting**: deduplicate object IDs globally after repository/time filtering; repository subtotals overlap. Prefer known counts from full copies over unknown shallow boundaries. Shallow counts use `LinesUnknown` / `unknown_line_commits` and visible `?` qualifiers. Scans never lazily fetch missing objects. Cached remote default history precedes local history.
- **Dates and retained behavior**: use `time.Local` for all calendar grouping and display. Future-dated commits remain counted, with no upper time cutoff. Keep the current churn formula and its zero-additions `0.00` convention. Keep the existing UI, JSON export, and merge-commit exclusion.
- **Repository refresh**: Startup and `r`, including in the PR board, clone/fetch only default-branch history in owned bare cache entries through existing `gh` credentials. No tags, other branches, or submodules are fetched. Clone/fetch work shares eight repository scan slots, has a two-minute timeout per repository, and disables interactive prompts. Excluded repositories are not refreshed. A failed startup fetch retains its offline scan; manual clone/fetch failure still attempts an offline scan. Failed results remain stale with visible diagnostics. `--export` never invokes discovery, clone, or fetch.
- **Contextual help**: `?` or `F1` opens a centered, scrollable help modal over every screen. Search input retains literal `?` and uses `F1` for help. Help owns its scrolling/dismissal keys; `c`/`a`/`p` leave help for a board. Ordinary dismissal preserves the underlying selection, filters, and overlays.
- **Shared screen frame**: Activity, Contributors/detail, and PR list/detail use the same compact title band, separate shared-scope and board-filter rows, one compact Git/PR status row, and pinned three-row footer. Main lists align their heading (sort, summary, visible range), column labels, and first row. Contributors totals use the heading, and narrow contributor rows stack metrics. PR rows reserve review/check/updated fields and stack at 80/40 columns. At 70×24+ a selected-PR preview above the footer shows wrapped title, full repo identity, author, draft, and review/check states; wide tables also include a dedicated author column. Short repo labels retain owners when ambiguous; Activity repository labels use the full configured/local repository set so filtering does not rename them. Activity area rows label their known-PR column; repository and directory rows use a ranking with added/removed bars and a compact contributor breakdown for the highlighted scope. Its fixed responsive height is reserved in both rendering and keyboard paging; very small terminals keep the ranking alone. Footer hints omit whole optional commands; coverage contains no commands. Wide titles include repository counts and version. The status row reports global tracked-repository freshness on every screen; selected repository timestamps stay in Activity evidence. The title count includes failed repositories, and tables share full-width gutters and selection rows. Stale/missing coverage stays visible; Data status provides the oldest retained scan and per-repository details. The PR board follows the global range using UpdatedAt, while local person/bot filters remain independent. Work-area PR summaries obey shared date/repository scope. Cached snapshot inventory remains explicitly labeled and independent of view counts. Contributor paging scrolls only the body, preserving its frame, and `r` refreshes without leaving detail.
- **Git isolation**: resolve branch OIDs, parse NUL-delimited paths, pin diff settings, clear repository-local environment overrides, and retain specific-agent AI matching with explicit user overrides.
- **Activity awareness**: Repository and directory rows show where change occurred, defaulting to added + removed lines with split bars. The contributor panel shows who changed the selected scope with the same per-file line attribution and Activity filters. Contributors detail shows the inverse repository breakdown. Bars retain that meaning when sorting by commits, recency, or name; there is no measure toggle. Enter follows literal directories and files, then commit evidence. Directory totals use retained per-file Git numstat counts, excluding generated paths according to scan options; whole-commit totals must not be repeated for every touched directory. Quiet repositories remain visible and unknown line counts retain `?` qualifiers. Date and bot filters apply at each scope. Secondary commit, People, and Related evidence remains available. Configured named areas retain their existing meaning separately from the literal directory hierarchy. Focused commit evidence fills the available height and pages by visible records. Ages use each row repository's retained scan timestamp. All modes share a high-contrast blue-gray canvas.


Parsed scan-cache maintenance: bump `scanCountingVersion` in `git/scan_cache.go` when parsing, identity/AI classification, counting, or default generated-path filters change. Bump `scanCacheSchema` when the persisted format changes. These explicit versions invalidate records created with older behavior.

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
- The separator is a static rule, and the loading screen uses plain language. Active Git/PR refreshes have a UI-only spinner; Git also shows completed/total repositories where space permits. Animation stops when refreshes finish and never triggers data requests.

## Automatic PR context

GitHub PR context loads automatically with existing gh authentication for discovered repositories. There is no PR feature toggle. Interactive startup uses setup when repository discovery fails; subsequent PR failures leave already-loaded cached history usable with a visible status. `github/` owns a bounded read-only provider through existing gh authentication, with strict github.com origins and no credential reads. `tui/pullrequests.go` owns in-memory async snapshots and the all-included PR board. PR handles/data never enter CommitRecord, contributor stats, or export. Initial loading and explicit `r` request GitHub context; active remote refreshes coalesce and completed snapshots are reused for at least one minute. Excluded repositories are skipped. A probe batches up to 100 repositories; a verified zero clears retained PRs and skips inventory only when the existing PR refresh reuse/coalescing gate admits work. Positive counts always retain normal inventory refresh. Probe, account, inventory, and file calls share `pr_request_budget`, which defaults to 128 calls per refresh, with a per-repository cap of half that budget (minimum one). Up to four repository inventories run concurrently and return in input order. All repository inventories precede additional file pages. Without a prior in-memory snapshot, inventories inline each PR's first file page; with one, they request only file counts, and first pages for PRs without reusable file lists are batched per repository in one aliased request of up to 50 PRs. Remaining pages run newest-PR-first. Canonical aliases share request and active-time allowances; complete inventory evidence wins over incomplete duplicate responses. The 90-second repository timeout counts active request time, excluding queue waits, while the 15-second request and two-minute TUI refresh deadlines remain. File reuse requires matching updated time, head/base revisions, and file count. Inventory completeness drives repository PARTIAL status independently of current-file and reviewer coverage; Data status reports coverage and requests by phase. Provider-wide quota cooldown survives cancellation. Retry-After/reset times are respected; missing timing uses bounded exponential cooldown. No PR polling, automatic retries, persistent PR cache, or credential storage. Local refreshes do not cancel or restart GitHub requests. Complete empty success clears while failures/partial inventories retain visible stale evidence. `stats.WorkAreaDefinition.ClassifyPaths` maps PR evidence without synthetic commits.

## Progressive disclosure

Activity repository and literal directory/file rankings include PR counts when
space permits: open/failing-check/changes-requested on wide rows, open only on
smaller rows, omitted on the narrowest. Counts use shared repository/date scope
and PR update time, independently of Git contributor/bot filters or saved PR
board facets. Unknown/stale inventory qualifies repository counts; directory
counts also require current file coverage across all in-range PRs. PRs annotate
existing rows without changing Git ranking, line bars, or contributor totals.

Contributor detail uses daily timeline buckets through 30 days, Sunday-aligned
weekly buckets through 180 days, and the existing latest-12-month timeline for
longer ranges and All time. Short-range timelines fill quiet dates across the
selected rolling window, including partial boundary dates, while preserving
future records. The daily-cell matrix trims to that window through 180 days,
masks out-of-scope dates, and labels the visible interval when width limits it.
Filtering and chart bounds share a reference time; calendar markers preserve
local dates across DST. Line bars, unknown qualifiers, AI counts and heatmap
normalization retain their existing meanings.

`tui/worklist*.go` owns repository and directory rankings. Enter follows literal child directories, then files and commit evidence. `s` cycles lines changed, commits, recency, and name; bars always show added/removed lines. Tab/Shift+Tab moves focus between the ranking and its contributor pane; arrows, paging, Home/End scroll the focused pane, and Esc/left returns focus to the ranking before navigating back. The pane has no Enter action. `1`–`3` opens the current directory scope's Commits/People/Related evidence; `4` returns to its directory ranking. Detail lenses retain Tab cycling. Hidden panes cannot receive focus; changing the selected scope resets the pane offset. Esc navigates to the parent with applied searches and filters preserved; during text entry it restores the prior query. Directory line totals are computed from retained per-file counts. Commit counts can overlap across directories. Configured named areas remain secondary related evidence and never replace the literal directory hierarchy. `p` resumes the normal PR board; `v` opens a temporary repository PR context with a return to Activity.

## Related evidence

On Activity rankings, `o` opens the selected repository on GitHub, or the selected
directory/file at its retained scan revision. Local object lookup verifies the
literal path and expected tree/blob type; absent/deleted paths and unavailable
revisions produce a visible diagnostic. Links never follow a moving branch.
The contributor pane and modal/text contexts do not open ranking selections.
Every scan retains its revision only when the revision lookup succeeds unchanged
before and after scanning; ordinary and identity refreshes preserve this evidence.
Commit and PR browser actions keep their existing meanings.

On the Contributors list, `m` opens the identity merge modal over all collected
identities. With only the preselected contributor, `n` sets a display name
without merging; blank restores automatic naming and existing aliases remain
unchanged. Space stages additional emails to merge, `k` chooses the primary,
`n` edits the resulting name, and `/` searches. Enter reviews, then Enter explicitly saves; Esc edits or
cancels. Saving preserves unrelated config fields and rejects external changes.
Failure retains the draft and original mappings. Success remaps contributor
selections and starts a generation-isolated offline local rescan; existing PR
requests and fetch-stale coverage are preserved. Only this confirmed action
persists identity mappings; filters and Repository controls inclusion remain session-only.

Comma opens Settings on every board. First-run setup and Settings share personal-account
and organization selection, repository checklist, and explicit review/save steps.
The repository checklist sorts by GitHub `pushed_at` descending, unknown dates
last, with repository identity as the tie-breaker, and shows a LAST PUSH column.
This is GitHub push recency across branches, passed through discovery metadata;
it requires no extra requests or Git scans.
Account suggestions use the existing authenticated `gh` session; organization
names can also be entered. Personal-only selection is valid without any organization;
personal discovery requires the configured login to match the signed-in account.
Saving replaces `personal_account`, `organizations`, and `exclude` (with exact unchecked `owner/name`
identities), preserves unrelated configuration and named groups, and reloads the
board selection. Cancellation preserves the active configuration. Settings and
identity merges share one serialized atomic store and expected source, so repeated
saves coexist while external configuration changes are rejected. Cache status reads
a synchronized full discovery inventory, including excluded and out-of-group repos.

`worklist_related.go` separates exact nonempty shared commit IDs from canonical
contributor overlap. Related Enter opens shared commits; `m` exposes a scrollable
name-overlap list whose Enter opens identity-filtered People/Activity. These views show existing people, area, time,
and commit evidence. Shared PR unavailability belongs once in the
header; retained scope-specific PR signals remain available. Commit previews show
titles first, then author/time/hash and affected areas. No extra scans or requests.

## Shared command contract

`last-seen.json` under `$XDG_STATE_HOME/bigboard` (default `~/.local/state/bigboard`)
stores only bookmark timestamps for the full selection and each `--group`.
Interactive startup loads an immutable bookmark for the session; missing or
invalid state is explained in Data status and does not block the board. Normal
exit advances it to the latest committed fetch-pass snapshot time only when
usable history was published. Offline-only scans, canceled startup, abnormal
termination, and export never advance it. Settings reload requires its own
fetched snapshot. Activity LATEST and PR UPDATED use `+` for timestamps strictly
after the bookmark; Since last look in Date range applies that exact lower bound
across boards using Git author time and PR updated time. This is timestamp-only
state, not a persistent PR cache.

All boards use one shared Git/PR status line beneath Shared and Filters.
Healthy states stay quiet; stale, partial and unavailable states remain visible
during manual refresh. Startup's cached snapshot uses cyan `cached · fetching…`
until the fetched pass publishes; row markers are unchanged. `e` opens Data status
on every board, with current startup phase/progress, cached/fetched snapshot commit
times, retained scan times,
PR check times, tracked-repository coverage and failures, plus repository cache
location, size and unlisted entries; home-directory paths display as `~` without changing stored paths. `d` reveals diagnostics.
Status coverage always describes tracked repositories, independently of view filters.

Lowercase `c`/`a`/`p` resume peer boards with filters, sort, selection, scroll,
and stable drill-down state preserved. Board switching cancels pending choices
and closes temporary dialogs. Back closes details or moves to a parent; it never
clears applied filters. At a normal board root it stays there. Search Enter
accepts text; Escape restores the previous applied query. While typing, shortcut
letters are literal; F1 opens help and Ctrl+C quits.

`f` opens one shared filter modal at a vertical category overview, grouped into
Shared scope and the current board, with selection summaries and `*` for pending changes.
`t` opens its Date range options directly. Wide layouts place categories beside
options; narrow layouts show only the focused pane. Shared scope is date plus
repository viewing selection, applied in memory across all boards.
`i` separately manages repository tracking/refresh coverage and explicit removal
of cached repositories no longer listed by organization discovery. Board-specific
filters: Activity Git contributors/bots, Contributors bots/name search, PRs
Mine/GitHub authors/review/checks/state/search. Mine uses the signed-in gh account,
resolved once per refresh inside the shared request budget and cooldown. Its
authored-by-me and review-requested-from-me choices match user logins only;
team membership is never inferred. Updated since my review compares PR UpdatedAt
strictly after the signed-in user's latest known submitted review timestamp from
the existing latestReviews response. It means any PR activity, independently of
the session bookmark; missing timestamps do not match. Review timestamps require
no extra API calls, and bounded review coverage can omit matches. Missing login clears account evidence and
leaves selected Mine filters empty with a visible reason. Git identities are not
GitHub accounts.
Multiple values within a facet are OR; facets combine with AND. Empty selections
mean All. Tab/Shift+Tab changes pane focus; left focuses categories, right options.
Arrows, page keys and Home/End move within the focused pane. Space from categories
opens options; Space in options stages a choice. `/` finds options and focuses
their pane. Enter applies the entire draft from either pane; Escape cancels.
Custom-day and search editors keep an edit in the draft with Enter or cancel
only that edit with Escape. Inside the modal, `x` stages Reset this board and `X`
stages Reset shared; both require Enter to apply and are actions, not categories.
Reset shared means All time/all tracked repositories. Reset this board clears
board filters and searches without changing shared scope or navigation. Outside
the modal, `x` applies that board reset directly. Use copy-on-write for pending
selections so canceled edits cannot mutate the source model.

PR shortcuts `u`/`v`/`k`/`d` focus Authors/Review/Checks/State options in the common modal.
Activity `v` opens a contextual repository PR list using shared scope and no
unrelated saved PR filters. It snapshots only PR UI state, never fetched data.
`prUIState` groups the PR filters, search, sort, selection, and detail state;
context navigation clones that value while refresh data stays in `prState`.
Back from the contextual list returns to Activity and restores the saved normal
PR board; ordinary board switching also ends that temporary context. Repeated
`p` on the normal PR board preserves location. PR range uses last-updated time;
local history uses author dates. Unknown PR dates appear only in All time.
PR sorting adds Attention after the existing Updated/Repository/Author/Title
choices: review requested from the signed-in user, failing checks, changes
requested, approved open PRs, then other, oldest update first per bucket with
unknown dates last. Reverse applies to the complete order. Unknown status is
never approval or passing. An amber `!` on PR ages marks 14+ days without an
update as of the snapshot check time; preview and details explain inactivity.
It is distinct from stale cached data and the `+` bookmark marker.
Shared scope and board filters have separate visible rows; raw cache inventory
is labeled separately from result counts. PR summaries use shared scope so a
contextual jump is not silently narrowed by unrelated PR author/status filters.

Enter/right opens; left/Escape backs out. All scrollable lists/details support
arrows, paging, Home/End. `r` refreshes while preserving navigation; uppercase R
is a compatibility alias. `q` quits outside text entry. Footer hints prioritize
filters, contextual actions and help; full shortcuts stay in contextual help.
Help lists the active context commands first, shared filter commands once,
and board navigation before explanatory legends. Use Repository controls, Filters, Date range and Data status
consistently; modal help must describe only the actions its key handler accepts.
Repository controls, Data status and PR details use centered scrollable modals. Data status `d`
reveals diagnostics; `o` opens the selected PR externally. Related `m` switches
shared-commit and contributor-overlap evidence. Sorting remains `s`/`S`.
