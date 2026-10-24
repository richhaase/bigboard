# Big Board

A terminal situational-awareness board for seeing who has worked where across your Git repositories, how that work connects, and the commits behind it. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

![Worklist at 120 columns: work areas, latest authors and commits, with selected-area evidence below](docs/images/worklist-wide.png)

*Worklist at 120 × 36, with `tui` selected in Bigboard's own public Git history.
The lower pane shows the contributors, commits, and related areas behind that
selection. The capture omits `gh`, so PR context is
shown as unavailable rather than zero. Local and PR snapshots refresh on launch or `R`.*

[Take the visual tour](docs/visual-guide.md) for the views, navigation, and how to
read the counts. [Controls](#controls) · [Configuration](#config-file) ·
[GitHub PR context](#automatic-github-pr-awareness)

## Install

Big Board requires Git 2.31 or newer on `PATH`. Partial clones require Git
2.45.1 or newer so scans can reliably disable automatic object fetching.

### Homebrew

```bash
brew install --cask richhaase/tap/bigboard
```

### From Source

```bash
go install github.com/richhaase/bigboard/cmd/bigboard@latest
```

## Usage

Big Board is TUI-first: preferences live in the config file, and the CLI has exactly four flags.

```bash
# Analyze current directory
bigboard

# Analyze specific repos or scan directories for repos
bigboard ~/src/repo1 ~/src/repo2
bigboard ~/src/

# Use a named group from your config
bigboard --group backend

# Print contributor stats as JSON and exit (all-time window)
bigboard --export ~/src/ > board.json

# Use an alternate config file
bigboard --config ./bigboard.json

# Print version and exit
bigboard --version
```

The visible `Range: 14 days · t to change` control opens with `t`. Choose a
preset or enter a custom range of 1–3650 days; `Enter` applies and `Esc` cancels.
Range changes filter the loaded history in memory, with no scan or fetch.
The initial range defaults to 14 days. Set
`since` in the config to choose the initial range. `--export` always covers
all time; per-contributor first/last commit dates are included for reference.
Those dates alone cannot reconstruct totals for a narrower time window.

## Work relationships

Worklist puts each repository or area on one reading line: **where → latest
contributor + other contributors → latest commit → age**.
At 110 columns × 28 rows and above, a spacious evidence pane below the list shows
named contributors with scoped commit counts, recent titles, authors, times, hashes,
and affected areas. Shared PR availability appears once in the header; retained
area-specific PR evidence stays beside the relevant area. Moving the selection updates the evidence immediately. Recent is the
default sort; `s` cycles to Name and Activity (commits) without changing the selected
identity. Commit volume is not priority or productivity.

`+N` beside the latest contributor counts the **other distinct contributors** in
the current local range and bot filter. It is not an online-presence count. The
evidence preview is ranked by scoped commit count; `+N more` means that preview is
bounded. People retains the full alphabetical list. GitHub handles remain separate
from canonical Git identities. Ages are fixed at each repository's last successful
local scan; future author dates are labeled `future`.

Press `Enter` on a repository to open its work areas. A single included repository
opens directly into work areas after scanning. `Enter` on an area opens a dedicated
detail view, moving keyboard focus from the list into the selected lens.
`Tab` or `1`–`4` also opens a lens directly from the area inventory. In detail,
`Tab` / `Shift+Tab` or `1`–`4` switches Activity, People, Related, and Subareas;
only one detail surface is expanded at a time. Activity shows real subjects with
author-date evidence, contributor identity, and short object IDs. `Enter` opens a
scrollable inspector with the
full subject, canonical contributor identity, timestamp, object ID, and all changed
paths. People is alphabetical; selecting a person filters Activity. Related lists
exact commits touching both areas, their counts, and the latest shared title;
`Enter` opens those commits and then their full changed paths. Contributor overlap
is separate: it lists names appearing in both areas in the selected range. Press
`c` in Related for its scrollable overlap list, then `Enter` for names and their
identity-filtered evidence.

Subareas refines automatic path areas by one literal directory level at a time.
Direct files are a separate leaf; multi-path commits can appear in several children.
Configured named areas keep their explicit meaning and are not automatically
reinterpreted. `Esc` returns through each level, restoring the prior selection.
`/` searches the current list, `Enter` accepts the query, and `Esc` clears it before
leaving. `?` opens help for the current screen. `l` switches to the contributor-statistics
leaderboard. Time ranges, bot hiding, repository inclusion, and manual refresh
apply to both views.

Work areas are path-based groups, not inferred features or ownership. Automatic
grouping expands containers such as `src`, `services`, `packages`, `apps`, `cmd`,
and `internal`, so `services/auth` and `services/payments` appear separately.
Definitions use the full scan and stay stable while changing the time range.
Root files, excluded-file-only commits, empty commits, and unknown shallow-boundary
paths have explicit groups. Binary files count as activity even without line counts.
A commit can appear in several areas; area counts overlap and must not be summed.
Only destination paths assign rename/copy pairs to areas; the previous path is
shown as evidence, without treating an unchanged copy source as work.

For meaningful cross-directory groups, optionally configure `work_areas`:

```json
{
  "work_areas": {
    "my-monorepo": [
      {"name": "Authentication", "paths": ["services/auth", "apps/web/src/auth"]},
      {"name": "Payments", "paths": ["services/payments"]}
    ]
  }
}
```

The repository key is its unique display name or absolute path (absolute path
wins). Prefixes are literal repository-relative files or directories, not globs.
The longest matching prefix wins; unmatched paths keep automatic groups. Names
must be unique within a repository, and one prefix cannot name two areas.
The existing `depth` setting still controls repository discovery only.

The board updates local repository data only on launch and when you press `R`.
There is no periodic auto-refresh. “Local” shows the last successful local
refresh for the selected repository; “Updating…” keeps the existing view usable during a refresh.
Startup and `R` fetch included repositories from `origin`, then scan local history
and request GitHub PR context subject to the PR cooldown described below. Export
scans cached history without fetching. Fetches update remote-tracking branches only;
local branches, tags, working trees, and submodules stay untouched. Repositories
without `origin` remain local-only. Fetches use existing Git credentials, disable
interactive prompts, and run at most eight repositories at once with a two-minute
fetch timeout per repository. A failed fetch still allows the local scan, with a
STALE marker and diagnostics under `e`; a failed scan retains that repository's
last successful data. A successful fetch and scan, including an empty one, clears
the stale state. The board
requires at least 40 columns by 18 rows. Narrow or short terminals keep one primary
surface with the same controls. At 80 columns, two-line rows keep names and
subjects readable. `Enter` replaces the list with detail; `Esc` restores its
selection and scroll position. At 40 columns, each row uses three lines. Shallow,
stale, failed, partial, and unknown data remain visibly qualified.
Statistics retain their wide layout; narrow contributor detail uses labeled
repository metrics so every value remains readable. On short terminals, contributor detail
uses `PgUp` / `PgDn` to scroll and `Home` / `End` for the first or last page;
`↑` / `↓` still switches contributors. Its navigation remains visible while
scrolling through the timeline and activity matrix.

## Automatic GitHub PR awareness

Read-only open pull request context loads automatically for supported GitHub
origins using an already installed and signed-in `gh` CLI. No config setting is
needed.
Missing `gh` or authentication is shown without blocking local history. Bigboard
never signs in, requests access, creates credentials, or changes pull requests.
Headless `--export` stays local and unchanged.
Only a supported GitHub `origin` is used, with no upstream guessing.

PR summaries appear beside repositories and work areas when width permits.
Press lowercase `p` for all included repositories, `f` within PRs to choose one
repository, or `v` from Worklist for its selected repository. Enter opens a
scrollable detail view; left / Esc returns one level. Details show the canonical link, GitHub author and reviewers,
aggregate review decision, check rollup, mergeability and update time. GitHub
handles remain separate from local Git contributor identities. `UNKNOWN` is not
approval or merge readiness; “updated” includes any PR activity.

Changed file paths map to the same configured prefixes and automatic Work areas
as local history, including new PR-only areas. Generated files follow `all_files`.
A PR touching multiple areas appears in each; the overall count deduplicates it,
including across duplicate local clones. PRs remain independent of local date,
contributor and bot filters and never enter commit totals or the JSON export.

The initial remote refresh runs after the local scan without blocking navigation.
`R` fetches repository history and refreshes PR context, including inside the PR
board. Repeated PR refreshes reuse in-memory snapshots for at least one minute and
join an in-flight remote refresh rather than canceling and restarting it. Excluded
repositories are not fetched. The entire remote refresh shares a 128-request cap;
repositories left unfetched retain explicitly stale/partial evidence. Rate limits
pause all remaining requests, honor GitHub's reset/Retry-After time, and use bounded
exponential cooldowns when no retry time is available. The PR board status shows when `R`
can request data again; no background retry runs. There is no polling or disk cache,
so a new process starts a fresh remote refresh. Failed or incomplete inventories retain
last-good evidence with visible stale/partial labels; a complete open inventory
clears missing PRs even when supporting context is partial. Matching PRs retain
old path evidence when their new file list is incomplete. Each repository refresh is bounded to 200 open PRs,
1,000 file paths per PR, 64 requests and 90 seconds, within a two-minute overall
refresh. Reviewer lists are also bounded and visibly partial when truncated.
No PR descriptions, comments, diffs or check logs are requested. GitHub's GraphQL
file list does not expose rename origins, so that limitation is labelled rather
than guessed. Authentication, rate limits, unavailable repositories and unsupported
origins are reported in the PR board without suppressing local history.

## Accuracy notes

- **Author identity** is resolved by Git's native `.mailmap`, then grouped by canonical email. Shared or similar names do not merge people. Use `.mailmap` to combine aliases with different emails. Missing-email identities are scoped to the repository and exact name. The legacy `fuzzy` preference is accepted but no longer changes identity matching. Selection follows the identity when its display name changes across time ranges.
- **AI authorship** is detected from specific known agent authors and `Co-authored-by` identities, including agent accounts that commit via GitHub (`Copilot`, `claude[bot]`, `devin-ai-integration[bot]`, `google-labs-jules[bot]`, …). Working at an AI company does not classify someone as an agent. Add your own agents' emails or explicit `@domains` under `ai_identities`. Sorting compares exact AI ratios; percentages are rounded only for display.
- **Bots are counted, not hidden.** Bot accounts (`dependabot[bot]`, `renovate[bot]`, your own agents via `bot_identities`) rank on the leaderboard with a `BOT` tag; press `b` to toggle them out of view.
- **Generated & vendored files** (lockfiles, `vendor/`, `node_modules/`, `dist/`, `*.min.*`, `*.snap`, `go.sum`, …) are excluded from line counts by default so they don't inflate scores. Set `"all_files": true` to count everything.
- **Scope** is each repo's default branch, excluding merge commits. Cached remote default history is preferred over the local branch; local `main`/`master`, then `HEAD`, provide fallbacks. Startup and `R` fetch included repositories before scanning; export uses cached history. Locally divergent commits outside that selected history are excluded. Branches are resolved to commit IDs so same-named tags cannot change the history. Rename/copy changes use `-M -C` with explicit diff settings and literal, NUL-delimited filenames.
- **Shared commits** count once across selected repositories, by Git object ID. Repository subtotals retain every association and can overlap; do not sum them to obtain board totals. When copies have different mailmaps, attribution comes from a complete copy first, then the lowest repository ID, deterministically. Filtering a repository out also removes it as a source of attribution/counts.
- **Calendar dates** use your local timezone for active days, monthly totals, first/last dates, and heatmaps. Future-dated commits remain counted; ALL includes every collected commit and recent windows retain their existing lower-bound filtering. Churn remains removals/additions, with `0.00` when additions are zero.
- **Incomplete history** is flagged for shallow clones. Boundary line counts are unknown, shown as `?` or a known subtotal followed by `?`. A complete copy of the same commit can supply its missing counts. JSON export keeps numeric known subtotals and includes `unknown_line_commits` on contributor/repository objects when nonzero; stderr reports shallow repositories. Partial clones with missing objects are reported as scan failures instead of downloading data.

## Controls

BigBoard has three peer boards. Lowercase `l` opens the contributor leaderboard,
`w` opens Worklist, and `p` opens PRs across all included repositories. Switching
uses cached data and keeps selections, searches, and sorts. Plain `p` clears PR
repository scope. Worklist retains its repository, area, lens, and person context.
Pending dialog choices cancel when switching boards.

| Key | Shared behavior |
|-----|-----------------|
| `l` / `w` / `p` | Open Leaderboard / Worklist / all-repository PRs |
| `↑` / `↓` | Select rows in lists; scroll lines in details |
| `PgUp` / `PgDown` | Move a page in lists and details |
| `Home` / `End` | First / last row or detail page |
| `→` / `Enter` | Open the selected item or drill down; does not toggle detail closed |
| `←` / `Esc` | Clear a current filter or return one level; stay at board roots |
| `/` | Search the current board list or Worklist lens; reopen the existing query for editing |
| `s` / `S` | Change sort / reverse order in sortable lists |
| `r` | Open global repository inclusion controls from any board or detail |
| `R` | Fetch included repositories and request PR context, subject to PR cooldown; preserve location |
| `e` | Open repository update errors when present |
| `?` / `F1` | Open contextual help |
| `q` / `Ctrl+C` | Quit outside text entry; `Ctrl+C` always quits |

During search entry, ordinary characters, including shortcut letters and `?`,
enter text. `Enter` accepts the filter, `Esc` clears it, and `Backspace` edits.
`F1` opens help while typing. Help is a centered, scrollable modal: arrows,
paging, and Home/End scroll; `Esc`, `Enter`, `?`, or `F1` dismiss it. Board keys
can leave help directly. Dialogs own their other keys while open.

### Worklist

Worklist shows included repositories and their default-branch history in the
selected local time range. `→` / `Enter` drills through repositories, areas,
lenses, and commit details. `←` / `Esc` clears a current search/person filter or
returns to the parent. At the repository root it stays there; use `q` to quit.

| Key | Worklist behavior |
|-----|-------------------|
| `Tab`, `1`–`4` | From an area list, open Activity / People / Related / Subareas |
| `Tab` / `Shift+Tab`, `1`–`4` | Switch lenses in area detail |
| `s` / `S` | Change Recent / Name / Activity sort, or reverse it, in overview and Subareas |
| `t` | Change the local history range |
| `b` | Show / hide bot contributors in local history |
| `v` | Open the PR board scoped to the selected repository, including from area or commit detail |
| `c` | In Related, switch between shared commits and contributor overlap |

People, Activity, and Related keep their meaningful fixed ordering. In People,
Enter opens that contributor's Activity. Related and Subareas open the selected
evidence. The full commit inspector uses arrows and paging to scroll; `←` / `Esc`
returns to Activity. `Tab` / `Shift+Tab` leaves it for the next / previous lens.
`R` refreshes directly from the inspector without changing the selected commit.

### PRs

The PR board defaults to all open PRs across included repositories, independent
of local time, person, and bot filters. `f` opens a repository scope picker:
choose All included repositories or one repository, then Enter to apply or Esc
to cancel. Scope is a view filter; it never changes inclusion or fetch coverage.
Worklist's `v` enters this same board with explicit repository scope. Plain `p`
returns to all included repositories while preserving the selected PR if visible.

`/` searches repository, title, author, or `#number`. `s` cycles Updated,
Repository, Author, and Title; `S` reverses the order. Updated defaults newest
first. The heading shows the active scope and sort; accepted search text stays
visible. Enter / right opens PR details; left / Esc returns to the list, clears
an active search, then clears repository scope. Back at the all-repository root
stays on PRs. All lists and details support arrows, paging, and Home/End.

`o` opens the selected PR in your default browser from the list or detail when
its canonical GitHub URL is valid. Launch errors appear on the board. `R`
refreshes repository history and PR context from list or detail. `r` changes
global inclusion and returns to the same PR view.

### Leaderboard

The leaderboard aggregates included repositories in the selected local history
range. `/` filters contributor names; `s` selects the metric and `S` reverses it.
`b` toggles bots and `t` changes the range. Enter / right opens contributor detail.
In detail, arrows and paging scroll the same contributor; left / Esc returns to
the list. Select another contributor there to open their detail. `r` and `R`
remain available in detail. At the list root, Esc clears a search if present,
otherwise stays there; `q` quits immediately.

### Dialogs

Repository inclusion is session-wide and separate from PR repository scope.
Use arrows, paging, and Home/End to select a repository, and Space to toggle it.
Enter applies choices; Esc / left cancels. Exclusion removes a repository from
all boards and subsequent fetch/PR requests. It does not write the config file.

`t` opens the local range picker on Worklist and statistics list/detail views.
Choose 1d / 7d / 14d / 30d / 90d / 1y / all / Custom days. Home selects the first
preset; End or `c` selects Custom. Enter applies a preset or opens day entry.
Custom entry accepts 1–3650 days; Backspace edits and Ctrl+U clears it. Enter
applies a valid range; Esc cancels. Custom ranges are session-only. Time and bot
filters affect Worklist and statistics; PRs remain independent.

## Config file

Optional, at `$XDG_CONFIG_HOME/bigboard/config.json` when `XDG_CONFIG_HOME` is set,
otherwise `~/.config/bigboard/config.json`. Override either with `--config`.
Malformed JSON, unknown fields, and unsupported `sort`, `since`, or `theme` values
report an error. Settings are read at startup;
interactive filters and inclusion changes do not rewrite the file.

```json
{
  "paths": ["~/src"],
  "exclude": ["vendor-*", "org-a/api"],
  "sort": "net",
  "since": "90d",
  "theme": "dark",
  "fuzzy": false,
  "all_files": false,
  "depth": 2,
  "groups": {
    "backend": ["~/src/api", "~/src/workers"],
    "frontend": ["~/src/web"]
  },
  "ai_identities": ["my-agent@example.com", "@agents.example.com"],
  "bot_identities": ["my-agent@example.com"]
}
```

- `paths` supplies scan roots when no positional paths or `--group` are given; without any of these, Bigboard scans the current directory.
- `depth` controls recursive repository discovery, with a default of `1`; values below `1` also use `1`. It does not change work-area grouping.
- `sort` sets the statistics metric: `commits`, `added`, `removed`, `net`, `ai`, or `total` (`impact` is an alias for `total`). The default is `total`. Worklist starts in Recent regardless of this setting.
- `theme` accepts `auto`, `light`, or `dark` as a terminal-background hint; the current Worklist canvas uses its fixed high-contrast palette.
- `all_files` includes generated/vendored paths in line totals; it defaults to `false`.
- `work_areas` defines optional cross-directory groups using the [rules above](#work-relationships).
- `since` takes a preset label: `1d`, `7d`, `14d`, `30d`, `90d`, `1y`, or `all`, with a default of `14d`.
- `fuzzy` is retained for config compatibility; use `.mailmap` for identity aliases.
- `exclude` entries match a repo basename, a unique display name like `org-a/api` (for duplicate basenames), or a glob of either.
- `ai_identities` marks commits by those authors (or co-authors) as AI-assisted; entries are exact emails or `@domain` suffixes.
- `bot_identities` tags contributors as bots; entries are exact emails, `@domain` suffixes, or exact author names.
- Select a group with `--group backend`. Author identities can also be canonicalized with a standard git `.mailmap` in each repo.

## Features

- Relationship-first repository and work-area views, cross-scope contributor focus, and scrollable commit/path evidence
- Local updates on launch or `R`, separate local/PR freshness, and retained last-good data on refresh failures
- High-contrast Worklist with responsive full-width evidence and compact list-to-detail navigation
- Streaming repository-scan loader that surfaces unreadable repos as they load
- Gradient impact bars with trailing glow; gold/silver/bronze rank styling
- AI-authorship as a first-class metric: leaderboard `AI%` column, per-month AI share, per-repo AI %
- Bot contributors counted and tagged (`BOT`), with a one-key toggle to hide them
- Per-contributor drill-down: repo breakdown, gap-aware monthly timeline, neon contribution heatmap, and derived metrics (active days, first/last commit, churn)
- Scrollable, height-aware leaderboard with incremental `/` search — every contributor reachable
- Headless `--export` (JSON, same pipeline and identity policy as the TUI), config file with named `--group`s, glob excludes, and recursive scan depth
- Accurate-by-default: native `.mailmap`, generated/vendored files excluded, deterministic ordering, rename/copy-aware churn
- Git worktree detection (linked worktrees are skipped); separate-Git-directory repositories are supported, and symlinked repo directories are followed and deduplicated

## License

MIT
