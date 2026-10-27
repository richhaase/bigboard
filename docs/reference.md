# Big Board user reference

[README](../README.md) · [Visual guide](visual-guide.md)

## Setup and settings

Run `bigboard` with Git and the signed-in `gh` CLI available. First-run setup
lets you select your personal GitHub account, organizations, or both, then
repositories. Review and save to create the configuration. Personal-only
selection is supported; it must match the signed-in account. Discovery includes
accessible public and private repositories owned by those sources, omitting
archived repositories. It does not include repositories merely shared with the
personal account by another owner.

Press `,` on any board for Settings. Space toggles choices; Enter advances to
review and then saves; Esc returns or cancels. You can add an organization by
name. Repositories are listed by last push, newest first, with unknown dates
last. Last push covers activity across branches; the boards use default-branch
history. Saving reloads repository selection and preserves unrelated settings.
A discovery error leaves setup open with a retry action.

Settings saves account, organizations, and repository exclusions for future
sessions. Ordinary board filters and Repository controls are session-only.
Settings and contributor identity saves reject external configuration edits
made during the session, leaving the draft available to correct or cancel.

## Navigation and common controls

Lowercase `a`, `c`, and `p` resume Activity, Contributors, and PRs. Each remembers
its filters, sort, selection, scroll, and drill-down location. Switching boards
closes temporary dialogs and cancels unapplied edits. Back preserves applied
filters; at a board root it stays there.

| Key | Action |
| --- | --- |
| `↑` / `↓` | Select a row or scroll detail |
| `PgUp` / `PgDn`, `Home` / `End` | Page, or go to the first/last entry |
| `Enter` / `→` | Open the selected item or drill down |
| `Esc` / `←` | Close or return one level |
| `/` | Edit the current list's search |
| `s` / `S` | Cycle sort / reverse order where available |
| `f` / `t` | Open Filters / Date range options |
| `x` | Clear this board's filters and search; keep shared scope and location |
| `r` | Refresh Git history and PR context without leaving your location |
| `e` | Open Data status; `d` there reveals diagnostics |
| `i` | Repository controls: session inclusion and unlisted-cache removal |
| `,` | Settings: saved repository selection |
| `y` | Copy a supported current list as Markdown using terminal OSC 52 clipboard support |
| `?` / `F1` | Contextual help |
| `q` / `Ctrl+C` | Quit outside text entry; Ctrl+C always quits |

During text entry, letters and `?` enter text. Enter accepts the edit; Esc
restores its previous value; Backspace edits; Ctrl+U clears the buffer. F1 opens
help. Long input keeps the cursor and editable tail visible. If a dialog cannot
fit, enlarge the terminal before continuing edits or confirmation.

Help presents commands before explanatory legends. Arrows, page keys, and
Home/End scroll it; Esc, Enter, `?`, or F1 closes it while preserving the screen
underneath. Board keys can leave help directly. Uppercase `R` remains a refresh
compatibility alias, and `l` remains an alias for Contributors. Current help uses
`r` and `c`.

## Filters and date ranges

`f` opens a category overview grouped into Shared scope and the current board.
`t` opens Date range options directly. Wide layouts show categories beside
options; narrow layouts show the focused pane.

| Control | In Filters |
| --- | --- |
| `Tab` / `Shift+Tab` | Change pane focus |
| `←` / `→` | Focus categories / options |
| Arrows, page keys, `Home` / `End` | Move within the focused pane |
| `Space` | Open a category's options, or stage an option |
| `/` | Find options |
| `Enter` | Apply the entire draft from either pane |
| `Esc` | Cancel the draft |
| `x` / `X` | Stage Reset this board / Reset shared; Enter still applies |

Selection summaries describe each category; `*` marks pending edits. Multiple
values within one category match any selected value; categories combine with
AND. No selection means All. While editing a search or custom range, Enter
stages only that edit and Esc cancels only that edit. Apply the whole draft
with Enter after leaving the editor.

Shared scope is **date plus repository viewing selection**. It affects every
board without changing which repositories refresh. Reset shared selects All
time and all tracked repositories. Reset this board clears that board's filters
and search without changing shared scope or navigation.

Date presets are 1d, 7d, 14d, 30d, 90d, 1y, and All time; Custom days accepts
1–3650. Since last look is available when there is a saved bookmark. Activity
and Contributors use Git author dates; PRs use last-updated time. Unknown PR
dates appear only in All time. Filtering is in memory, with no scans or requests.

Activity has Git contributor and bot filters. Contributors has independent bot
visibility and name search. PRs has Mine, GitHub author, review, checks, state,
and search filters. Git contributor identities and GitHub accounts are separate.

## Activity

Activity ranks repositories by added + removed lines in the selected period.
The bars split added and removed lines. `s` cycles lines changed, commits,
recency, and name; changing sort does not change what the bars measure. Quiet
repositories remain visible. Short repository labels omit an unambiguous owner;
repository identity and filter behavior remain unchanged.

Enter follows repositories → directories → files → commits. Directory line
counts include only the files inside that directory. The lower contributor
pane follows the highlighted repository, directory, or file. Tab or Shift+Tab
moves focus between the ranking and contributor pane; arrows, paging, and
Home/End browse that pane. Esc/left returns focus to the ranking before moving
back. A highlighted heading and side marker identify the focused pane. In very
small windows only the ranking is shown.

With the ranking focused, `o` opens the selected repository on GitHub, or the
selected directory/file at the retained scan revision. Paths are checked in the
local cache first; deleted or unavailable paths show a message without opening
a page. These links stay at the scan revision even if the local branch moves.

`1`, `2`, and `3` open Commits, People, and Related evidence for the current
scope; `4` returns to its directory ranking. Tab cycles these detail lenses.
People opens a contributor's scoped commits. Related shows shared commits;
`m` switches to contributor overlap, where Enter opens matching people and
their evidence. Shared contributors indicate historical overlap, not ownership
or online presence. `o` opens a selected commit on GitHub where available.

`v` opens PRs for the selected repository using shared scope, temporarily
omitting saved PR-board filters. Back from that list returns to Activity and
restores the normal PR view; `p` resumes the normal PR board directly.

Activity PR columns show open, failing-check, and changes-requested counts when
space allows. Narrower views show only open counts or omit the columns. A muted
dash means no open PRs; blank warning cells mean zero. Failing checks and changes
requested can overlap. `?` means incomplete/stale evidence; directory counts
also require complete current file lists for relevant PRs. Counts follow shared
scope and are independent of Activity's Git contributor filters and saved PR
board filters. They do not change commit totals or ranking.

## Contributors and identities

Contributors starts with who; Enter shows where that contributor worked, their
timeline, activity matrix, and derived metrics. Detail uses daily timeline
buckets through 30 days, weekly through 180 days, and monthly for longer ranges.
Calendar displays use local time and include quiet dates in the selected window.
Paging scrolls the detail while retaining its frame. `b` toggles bots on this
board independently of Activity.

`m` on the Contributors list opens all collected identities, independently of
view filters. With one identity selected, `n` sets a display name without
merging; a blank name restores automatic naming. To merge a person's emails,
Space stages additional identities, `k` chooses the primary email, and `n`
sets the resulting display name. `/` finds names or emails. Enter reviews the
change; a second Enter saves. Esc returns or cancels.

Names never automatically merge people. Git's `.mailmap` is applied first,
then saved email aliases. Saving rescans cached history without fetching and
keeps the mapping for future sessions. A failed save preserves the draft and
existing identities. Missing/invalid emails cannot be saved as identity mappings.

## Pull requests

PRs lists currently open PRs updated within shared scope. Enter opens a detail
modal; `o` opens the selected PR in a browser. `/` searches repository, title,
author, or number. `u`, `v`, `k`, and `d` open Authors, Review, Checks, and State
options in Filters.

Mine offers Authored by me, Review requested from me, and Updated since my
review. The first two match your signed-in GitHub login; team membership is not
inferred. Updated since my review means any PR activity after your latest known
submitted review, independently of the Since last look bookmark. Missing review
timestamps do not match. Bounded reviewer coverage can omit matches; Data status
and PR details report that limitation. If the account cannot be resolved, selected
Mine filters show no results and explain why.

`s` cycles Updated, Repository, Author, Title, and Attention; `S` reverses the
order. Updated starts newest first. Attention starts with review requests to
you, then failing checks, changes requested, approved open PRs, and other PRs.
Within each category, oldest updates come first and unknown dates follow dated
PRs. Reverse reverses the entire order. Labels describe the direction explicitly.

`◇` marks a draft and `●` an open non-draft PR. Green means an approved review
or passing reported checks; it is not a branch-protection or merge-readiness
verdict. An amber `!` on age marks no update for at least 14 days at the snapshot
check time. `+` marks an update since your bookmark. Both differ from stale data.

## Refresh, coverage, and bookmarks

Startup discovers repositories, scans existing cached Git history, then fetches
in the background and rescans changed revisions. With no cache, initial cloning
must finish before data is shown. The spinner indicates working; Git progress
also reports completed/total repositories where space permits. Each scan pass
publishes a complete snapshot. There is no periodic refresh; use `r`.

Before the startup network pass and each `r`, a bounded GitHub probe checks the
default branch name/revision and open-PR count for up to 100 repositories per
request. A matching branch/revision skips Git fetch only when the owned cache
passes its ownership and layout checks and holds exactly the expected
default-branch refs at that revision; any other refs force a normal refresh.
Missing caches and caches that fail these checks still use the normal
clone/repair path. Git fetch checks the objects it receives. When a Git scan
fails because objects were later lost or corrupted, the cache is marked damaged
and its parsed scan is discarded; the repository is reported stale, and the next
startup fetch pass or `r` rebuilds it with a fresh clone even when the probe
matches. `--export` reports the damage without repairing it.
Unknown probe results fall back to normal refresh behavior.
Parsed Git scans retain their existing fingerprint checks, including on `r`.
When a PR refresh is admitted, an explicit zero count clears retained PRs without
an inventory request. Positive counts still refresh PR checks and reviews even
when the branch revision has not changed. The existing one-minute reuse and
in-flight coalescing rules still apply.

The shared Git/PR status row describes all tracked repositories, independently
of viewing filters. Failed refreshes retain last-good evidence where available
and remain visibly stale/partial. `e` shows timestamps, startup progress, cache
location/size, unlisted repositories, bookmark state, and coverage. `d` adds
diagnostics. Home-directory paths are displayed with `~` for readability.

PR refreshes reuse completed in-memory snapshots for at least one minute and
join in-flight work. Up to four repository inventories run concurrently; all
inventories finish before additional file pages are fetched newest-PR-first.
The first refresh inlines each PR's first page of changed files. Later refreshes
request only file counts and reuse file lists for PRs whose update time,
head/base revisions, and file count are unchanged; first pages for the remaining
PRs are fetched together, up to 50 PRs per request for each repository.
Results retain repository order. Rate limits pause further requests until the
reported cooldown; no automatic retry runs. There is no persistent PR cache. Inventory
coverage is separate from file/reviewer coverage: missing files can make
**directory associations** incomplete without making the list of PRs incomplete.
Data status reports these separately, along with requests spent on probes, the
account lookup, inventories, and file pages. Probes share `pr_request_budget` and
the provider cooldown with PR work. Unknown evidence is never reported as zero.
Inventories are bounded to 200 open PRs per repository and 1,000 current file
paths per PR. A repository can use at most half the configured request budget
(minimum one request). Its 90-second allowance counts active request time,
excluding time queued behind other repositories, within the two-minute overall
refresh deadline. Individual requests remain limited to 15 seconds. Canonical
repository aliases share allowances and preserve complete inventory evidence;
already-started alias lookups count toward the budget. Reviewer lists are also
bounded. PR bodies, comments, diffs, and check logs are not requested.

Big Board saves the time of the last fetched snapshot you saw, separately for
the full view and each `--group`. The bookmark advances on normal exit after a
fetch pass publishes usable history. Offline-only scans, canceled startup, and
export do not advance it. First visits have no bookmark; `e` explains the state.
Since last look in Date range uses the saved timestamp as its lower bound. This
is not a full PR event history: merged/closed PRs are not part of the open-PR list.

## Repository controls and storage

Settings (`,`) saves repository choices. Filters (`f`) only narrow the view.
Repository controls (`i`) instead changes this session's tracking and refresh
coverage. Space stages inclusion, Enter applies, Esc/left cancels. Excluding a
repository here does not delete its cache or write configuration.

Big Board owns full bare caches of default-branch history under
`$XDG_CACHE_HOME/bigboard/repos/github.com/owner/name.git` (default cache home
`~/.cache`). Developer checkouts are never scanned or modified. Each verified
repository also stores disposable parsed Git records in `bigboard-scan`. Later
launches reuse them when the branch revision, analysis options, identities,
mailmap inputs, Git configuration/version, and object-store metadata match.
Schema or counting changes invalidate them; missing or damaged scan files and
cache-write failures fall back to a full local scan. A `GIT_ATTR_SOURCE` override
also uses a full scan. The file is removed with
its repository cache. Reuse does not certify remote freshness or advance the
last-look bookmark; the remote verification/fetch pass still runs. Cached repositories
no longer listed by discovery are retained. Select one in Repository controls,
press `d`, then Enter to explicitly confirm removal after refresh has finished.
A rebuilt cache deletes the previous copy once the replacement is published;
leftover temporary copies from interrupted refreshes are removed automatically.
A group selection or exclusion does not make a listed repository removable.

Configuration defaults to `$XDG_CONFIG_HOME/bigboard/config.json`, or
`~/.config/bigboard/config.json`. Bookmarks live at
`$XDG_STATE_HOME/bigboard/last-seen.json`, or
`~/.local/state/bigboard/last-seen.json`. Bookmarks contain timestamps only.

## Configuration reference

Use Settings and the contributor identity dialog for routine configuration.
Advanced preferences are read from JSON at startup. Invalid JSON, unknown
fields, and unsupported values report errors. External edits during a session
are rejected by later Settings/identity saves rather than overwritten.

```json
{
  "personal_account": "example-user",
  "organizations": ["example-org"],
  "exclude": ["example-org/legacy-api", "vendor-*"],
  "since": "14d",
  "sort": "total",
  "groups": {"backend": ["example-org/api", "example-org/workers"]},
  "all_files": false,
  "pr_request_budget": 128
}
```

| Field | Meaning |
| --- | --- |
| `personal_account` | Owned repositories of the signed-in GitHub account; optional if organizations are configured |
| `organizations` | Organizations to discover; optional if a personal account is configured |
| `exclude` | Basename, `owner/name`, or glob patterns; Settings replaces them with exact unchecked identities |
| `groups` | Named lists of `owner/name` identities within the configured sources; select with `--group` |
| `since` | Startup preset: `1d`, `7d`, `14d` (default), `30d`, `90d`, `1y`, or `all` |
| `sort` | Contributors: `commits`, `added`, `removed`, `net`, `ai`, or `total` (default); `impact` aliases `total` |
| `theme` | `auto`, `light`, or `dark` terminal-background hint; Activity retains its high-contrast canvas |
| `all_files` | Include generated/vendored paths in line counts; default false |
| `pr_request_budget` | Positive request cap per remote refresh; default 128; does not bypass cooldowns |
| `ai_identities` | Additional AI author/co-author emails or `@domain` suffixes |
| `bot_identities` | Additional bot emails, `@domain` suffixes, or exact author names |
| `identities` | Canonical emails with alias lists and optional display names, managed by `m` on Contributors |
| `work_areas` | Optional named cross-directory groups, used in secondary related evidence |
| `fuzzy` | Accepted for compatibility; does not change contributor aggregation |

Identity mapping example:

```json
{"identities":{"dev@example.com":{"aliases":["dev@personal.example"],"name":"Example Developer"}}}
```

Emails are trimmed/lowercased and must be unique across the mapping. Aliases apply
after `.mailmap`, before AI/bot classification, across boards and JSON export.
Prefer the in-app dialog so names and emails can be reviewed before saving.

Named work areas use literal repository-relative file/directory prefixes:

```json
{"work_areas":{"example-org/api":[{"name":"Authentication","paths":["services/auth","apps/web/auth"]}]}}
```

The longest matching prefix wins; unmatched paths keep automatic groups. Names
must be unique per repository and a prefix cannot belong to multiple areas.
Named areas do not replace Activity's literal directory hierarchy.

## Counting and interpretation

- Git data covers default-branch history, excluding merge commits and developer-only branches.
- Added/removed lines exclude generated and vendored files by default. Binary changes can be activity without line counts.
- Directory line counts belong to files inside the directory. Commit counts can overlap across directories.
- Shared Git object IDs count once across selected repositories; repository subtotals retain associations and can overlap.
- Contributor identity is based on canonical email, never similar names. Missing-email identities remain repository-scoped.
- AI percentages describe detected authors/co-authors, not inferred tool usage. Bots are included by default and can be hidden per board.
- Dates use your local timezone. Future-dated commits remain counted. Ages reflect retained snapshot times, not live polling.
- `?`, STALE, and PARTIAL qualify incomplete evidence. Inspect Data status before treating a count as complete.

## Command line

```bash
bigboard
bigboard --config ./bigboard.json
bigboard --group backend
bigboard --export > board.json
bigboard --version
```

These are the four supported flags. Positional repository paths and the old
`paths`/`depth` configuration fields are unsupported. First-run setup can create
a missing explicit `--config` file only after confirmation.

`--export` requires valid existing configuration and a populated cache. It does
no discovery, clone, or fetch; it exports all-time contributor statistics as JSON,
using the same identity/counting rules and no PR data or session filters. It may
reuse or populate the disposable parsed Git scan cache. First
and last commit dates do not let you reconstruct shorter-range totals from that
aggregate export.

## Troubleshooting

- **Repository discovery fails:** check `gh auth status` and repository access, then retry in setup.
- **No rows after filtering:** inspect Shared and Filters; `x` clears only this board, while Filters → Reset shared restores All time/all tracked repos.
- **Data is stale or partial:** open `e`, then `d`; refresh with `r` after resolving the reported problem or cooldown.
- **A shortcut types a letter:** finish or cancel the active text editor. F1 still opens help.
- **A dialog asks for more room:** resize it before editing or confirming; its pending choices are preserved.
- **A save reports an external change:** another process edited the configuration. Cancel and restart to load it before saving again.
