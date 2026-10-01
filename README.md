# Big Board

A terminal situational-awareness board for seeing who has worked where across your Git repositories, how that work connects, and the commits behind it. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

![Worklist at 120 columns: work areas, latest authors and commits, with selected-area evidence below](docs/images/worklist-wide.png)

*Worklist at 120 × 36, with `tui` selected in Bigboard's own public Git history.
The lower pane shows the contributors, commits, and related areas behind that
selection. GitHub context is rate-limited in this capture, so PRs are shown as
unavailable rather than zero. Local and PR snapshots refresh on launch or `R`.*

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

The time range is chosen interactively (`←/→`) and defaults to 14 days. Set
`since` in the config to choose the initial range. `--export` always covers
all time; per-contributor first/last commit dates are included for reference.
Those dates alone cannot reconstruct totals for a narrower time window.

## Work relationships

Worklist puts each repository or area on one reading line: **where → latest
contributor + other contributors → latest commit → age → open PR context**.
At 110 columns × 28 rows and above, a spacious evidence pane below the list shows
contributors with scoped commit counts, recent commit subjects, PRs, and related
areas. Moving the selection updates the evidence immediately. Recent is the
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
exact shared-contributor counts; open one to inspect those identities and their
evidence. These are historical associations, including shared Git history across
clones or forks, not online presence or proof of collaboration.

Subareas refines automatic path areas by one literal directory level at a time.
Direct files are a separate leaf; multi-path commits can appear in several children.
Configured named areas keep their explicit meaning and are not automatically
reinterpreted. `Esc` returns through each level, restoring the prior selection.
`/` searches the current list, `Enter` accepts the query, and `Esc` clears it before
leaving. `?` shows all controls. `v` switches to contributor statistics. Time ranges,
bot hiding, repository inclusion, and manual refresh apply to both views.

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
refresh for the selected repository; “Updating…” keeps the existing view usable during a refresh. `R`
refreshes local history immediately and requests GitHub PR context subject to
the remote cooldown described below. Big Board never runs `git fetch`;
fetch remote history separately when you want to update your local Git cache.
A failed refresh retains that repository's last successful data with
a STALE marker; a successful scan, including an empty one, replaces it. The board
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
never signs in, requests access, creates credentials, fetches Git objects, or
changes pull requests. Headless `--export` stays local and unchanged.
Only a supported GitHub `origin` is used, with no upstream guessing.

PR summaries appear beside repositories and work areas when width permits. From
Worklist, press `p` for the selected
scope or `P` for all included repositories, then Enter for a scrollable detail view
and Esc to return. Details show the canonical link, GitHub author and reviewers,
aggregate review decision, check rollup, mergeability and update time. GitHub
handles remain separate from local Git contributor identities. `UNKNOWN` is not
approval or merge readiness; “updated” includes any PR activity.

Changed file paths map to the same configured prefixes and automatic Work areas
as local history, including new PR-only areas. Generated files follow `all_files`.
A PR touching multiple areas appears in each; the overall count deduplicates it,
including across duplicate local clones. PRs remain independent of local date,
contributor and bot filters and never enter commit totals or the JSON export.

The initial remote refresh runs after the local scan without blocking navigation.
`R` refreshes local history and PR context; inside the PR overlay it refreshes PRs
only. Repeated refreshes reuse in-memory snapshots for at least one minute and
join an in-flight remote refresh rather than canceling and restarting it. Excluded
repositories are not fetched. The entire remote refresh shares a 128-request cap;
repositories left unfetched retain explicitly stale/partial evidence. Rate limits
pause all remaining requests, honor GitHub's reset/Retry-After time, and use bounded
exponential cooldowns when no retry time is available. The PR overlay status shows when `R`
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
origins are reported in the PR overlay without suppressing local history.

## Accuracy notes

- **Author identity** is resolved by Git's native `.mailmap`, then grouped by canonical email. Shared or similar names do not merge people. Use `.mailmap` to combine aliases with different emails. Missing-email identities are scoped to the repository and exact name. The legacy `fuzzy` preference is accepted but no longer changes identity matching. Selection follows the identity when its display name changes across time ranges.
- **AI authorship** is detected from specific known agent authors and `Co-authored-by` identities, including agent accounts that commit via GitHub (`Copilot`, `claude[bot]`, `devin-ai-integration[bot]`, `google-labs-jules[bot]`, …). Working at an AI company does not classify someone as an agent. Add your own agents' emails or explicit `@domains` under `ai_identities`. Sorting compares exact AI ratios; percentages are rounded only for display.
- **Bots are counted, not hidden.** Bot accounts (`dependabot[bot]`, `renovate[bot]`, your own agents via `bot_identities`) rank on the leaderboard with a `BOT` tag; press `b` to toggle them out of view.
- **Generated & vendored files** (lockfiles, `vendor/`, `node_modules/`, `dist/`, `*.min.*`, `*.snap`, `go.sum`, …) are excluded from line counts by default so they don't inflate scores. Set `"all_files": true` to count everything.
- **Scope** is each repo's default branch, excluding merge commits. Cached remote default history is preferred over the local branch; local `main`/`master`, then `HEAD`, provide fallbacks. No fetch is performed. Locally divergent commits outside that selected history are excluded. Branches are resolved to commit IDs so same-named tags cannot change the history. Rename/copy changes use `-M -C` with explicit diff settings and literal, NUL-delimited filenames.
- **Shared commits** count once across selected repositories, by Git object ID. Repository subtotals retain every association and can overlap; do not sum them to obtain board totals. When copies have different mailmaps, attribution comes from a complete copy first, then the lowest repository ID, deterministically. Filtering a repository out also removes it as a source of attribution/counts.
- **Calendar dates** use your local timezone for active days, monthly totals, first/last dates, and heatmaps. Future-dated commits remain counted; ALL includes every collected commit and recent windows retain their existing lower-bound filtering. Churn remains removals/additions, with `0.00` when additions are zero.
- **Incomplete history** is flagged for shallow clones. Boundary line counts are unknown, shown as `?` or a known subtotal followed by `?`. A complete copy of the same commit can supply its missing counts. JSON export keeps numeric known subtotals and includes `unknown_line_commits` on contributor/repository objects when nonzero; stderr reports shallow repositories. Partial clones with missing objects are reported as scan failures instead of downloading data.

## Controls

Keys depend on the active view. While typing a search, ordinary characters
(including `q`) enter text; `Enter` accepts, `Esc` clears, and `Ctrl+C` quits.

### Worklist overview and area detail

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Select a repository, area, commit, person, or related/subarea row |
| `PgUp` / `PgDown` | Page through the current list |
| `g` / `G`, `Home` / `End` | First / last row |
| `←/→` or `h/l` | Change local range: 1d / 7d / 14d / 30d / 90d / 1y / all |
| `Enter` | Repository → areas → detail → full commit; in People, filter Activity; in Related/Subareas, open that row |
| `Tab`, `1`–`4` | From an area list, open Activity / People / Related / Subareas |
| `Tab` / `Shift+Tab`, `1`–`4` | Switch lenses once area detail is open |
| `s` | Sort overview or Subareas: Recent → Name → Activity (commits); other detail lenses keep their order |
| `/` | Search only the current list or lens |
| `b` | Show/hide bot contributors in local history |
| `p` / `P` | Selected repository/parent-area PRs / all included repositories |
| `r` | Open repository inclusion controls |
| `R` | Refresh local history and request PR context, subject to remote cooldown |
| `e` | Open scan-error details when errors exist |
| `v` | Open the contributor-statistics leaderboard |
| `?` | Show Worklist help |
| `Esc` | Clear a search/person filter, unwind detail/subareas, then return to repositories; at the unfiltered repository list, quit |
| `q` / `Ctrl+C` | Quit, except ordinary `q` is text during search entry |

The full **commit inspector** has its own keys: `↑/↓` or `j/k` scrolls lines;
`PgUp/PgDown`, `g/G`, or `Home/End` moves farther. `Enter` or `Esc` returns to
Activity. `Tab/Shift+Tab` leaves the inspector for the next/previous lens. Return
to the list or lens before changing range, toggling bots, or refreshing.

### PR and repository overlays

In the **PR overlay**, `↑/↓` or `j/k` selects PRs or scrolls the open PR detail.
`Enter` toggles detail; `Esc` or `p` backs out one level. `R` requests **PR data
only**. `q` or `Ctrl+C` quits. Page and Home/End keys are not used here.

In **repository controls**, `↑/↓` or `j/k` selects and `Space` toggles inclusion.
Both `Enter` and `Esc` apply the current choices and close the overlay; `Esc`
does not cancel toggles. These choices affect the current session, not the config
file. Excluding a repository also removes it from later PR refresh requests.

### Contributor statistics

On the **leaderboard**, `↑/↓` or `j/k` selects a contributor and `Enter` opens
their detail. `s` cycles metric columns; `S` reverses the metric sort. `/` filters
contributors by name, `b` toggles bots, `r` opens repository controls, `R` refreshes,
and `v` returns to Worklist. With an accepted name filter, `q` or `Esc` clears it
first; otherwise it quits. The leaderboard scrolls as selection moves; page and
Home/End keys are not used there.

In **contributor detail**, `↑/↓` or `j/k` switches contributors. `PgUp/PgDown`
scrolls content and `Home/End` jumps to its first/last page. `Esc` returns to the
leaderboard, where filtering, bot toggles, sorting, repository controls and refresh
are available. `q` or `Ctrl+C` quits. Both statistics views support `←/→` or `h/l`
for the local time range. `p/P` and Worklist's `1`–`4` lenses are not statistics keys.

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
