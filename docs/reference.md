# Bigboard user reference

[README](../README.md) · [Workflow guide](visual-guide.md)

## Setup and settings

Open Bigboard from Finder, Spotlight, or the Dock. Git 2.31 or newer is required
for repository history. Live GitHub data also needs GitHub CLI (`gh`) installed
and signed in. Run `gh auth login` to authenticate, then return to Bigboard;
the app uses that authentication rather than providing a separate sign-in flow.

Open **Settings** in the sidebar or press `,`. Choose **GitHub** for live data or
**Cached history** to read existing owned repositories without network requests.
Enter a personal account, organizations, or both for live discovery. Personal
repositories must belong to the signed-in account; accessible non-archived
repositories from the selected organizations are also included.

Click **Discover repositories** to load the full list for the current sources.
Repositories are ordered by last push, newest first; this can include activity
on branches outside the default-branch history shown on the boards. Select at
least one repository, choose **Review workspace**, then **Save workspace**.
Discovery is required each time Settings is opened and after changing sources.
Errors preserve the draft. Cancel closes the dialog without saving.

Settings saves sources and exclusions for later sessions. Newly discovered
repositories remain eligible unless excluded. The repository viewing picker
only narrows the boards; **Repository controls** changes tracking for this
session. Neither changes the saved workspace.

## Navigation and scope

Select **Activity**, **Contributors**, or **Pull requests** in the sidebar.
Each board remembers its search, filters, sort direction, selection, and position.
Activity also retains its drill-down location. Back preserves applied filters.

| Control | Action |
| --- | --- |
| `a`, `c`, `p` | Activity, Contributors, Pull requests |
| `/` | Focus the current board’s search |
| `f`, `t` | Filters, date range |
| `s`, `S` | Cycle sort, reverse order |
| `x` | Reset this board’s filters/search while keeping shared scope |
| `r` | Refresh repositories and PR evidence |
| `e`, `i` | Data status, Repository controls |
| `,` | Workspace Settings |
| `y` | Copy the current view as Markdown |
| `?`, F1 | Keyboard guide; F1 also works while typing |
| Tab / Shift+Tab | Move between controls |
| Arrows, Page Up/Down, Home/End | Navigate board rows |
| Enter / right arrow | Explore a focused Activity row |
| Esc / left arrow | Go back in Activity; Esc also closes dialogs |

Letters enter text while an input is focused. In board search, Enter accepts
and leaves the field; Esc restores the value from before editing. Quit through
the application menu or Command-Q on macOS.

Shared viewing scope is **date plus repositories**. The default range is
**14 days**, unless `config.json` specifies another startup range. Presets are
1, 7, 14, 30, 90, and 365 days plus All time. **Custom…** accepts 1–3650 days and
provides **Since last look** when a saved bookmark exists. A configured group
appears in the group picker and changes the tracked repository set.

**Filters** stages shared scope and current-board facets together. Multiple
values within a category match any selected value; categories combine with AND.
An empty facet means All. Choose **Apply filters** or **Cancel**.
**Reset this board** clears its filters and search; **Reset shared** stages All
time and all tracked repositories. These resets apply with the rest of the draft.

Activity and Contributors filter Git author dates; PRs filter update times.
The lower date boundary is exclusive. Unknown dates match only All time.
Filtering uses the retained snapshot time and makes no network requests.

## Activity

Activity ranks repositories by added plus removed lines. Its split bars always
show additions and removals, even when sorted by commits, recency, or name.
Quiet tracked repositories remain visible. Search, contributor selection, and
bot visibility apply to this board independently of Contributors.

Select a row to inspect its contributors and recent activity. Use the row arrow,
double-click, or Enter to follow repository → directory → file → commits.
Directory counts include only files inside that directory. Breadcrumbs and Back
return to broader scopes. Long file histories load additional commits in batches.

The **Commits**, **People**, and **Related** tabs show evidence for the current
scope; `1`, `2`, and `3` select them, and `4` returns to the ranking. People opens
a person’s scoped commits. Related switches between exact shared commits and
historical contributor overlap, including configured named work areas. Open a
result to inspect the matching commits or people. Overlap is not ownership or
online presence.

**Open path on GitHub** (or `o`) verifies the path in the local cache at the
retained scan revision before opening it. Missing or deleted paths report an
error. Commit links open their specific GitHub commit.

PR counts follow shared dates/repositories, independently of Git contributor
filters. Failing checks and changes requested can overlap. `?` means evidence is
incomplete or stale; loading is shown separately from a completed zero. Directory
associations also need complete changed-file evidence. **View repository PRs**
(or `v`) temporarily omits the normal PR board’s facets. **Back to Activity**
restores them; `p` resumes the normal PR board directly.

## Contributors and identities

Select a contributor to inspect repository distribution, a timeline, a calendar,
derived metrics, and recent commits. Sort by lines changed, commits, additions,
removals, net lines, AI share, name, or recency. Bot visibility is independent of
Activity; `b` toggles it here. The repository breakdown links back to Activity.

Timelines use daily buckets through 30 days, weekly through 180 days, and monthly
for longer ranges. Calendar grouping uses local time and includes quiet dates.
Longer-range calendars show the latest year while totals retain the selected
range. AI share, net lines, and churn describe the selected evidence.

Choose **Identities** (or `m`) to search all collected email identities. Select
one email to rename it, or several to merge them. Choose the primary email and
an optional display name; blank uses automatic naming. **Review changes** shows
the proposal before **Save identity**. Names alone never merge people.

Git’s `.mailmap` applies before saved aliases. Saving updates the mapping and
rescans cached history without fetching. Failed saves preserve the draft.
External configuration edits are rejected rather than overwritten; restart to
load the changed file before saving again.

## Pull requests

Pull requests lists currently open PRs updated within shared scope. Search by
repository, title, author, or number. Select a row for its details and use
**Open on GitHub** (or `o`) to open the PR. Review/check badges and reported
merge conflicts are evidence, not branch-protection or merge authorization.

Author, review, checks, and draft-state facets combine with Mine:

- **Needs my attention:** your PRs with failing/error checks or requested changes,
  plus your approved non-draft PRs with passing checks; for other authors, an
  individual review request or activity since your latest submitted review.
- **Authored by me:** PRs authored by the signed-in GitHub login.
- **Review requested from me:** individual requests matching that login; team
  membership is not inferred.
- **Updated since my review:** activity after the latest known submitted review,
  independent of the last-look bookmark. Missing review timestamps do not match.

`m`, `M`, and `n` toggle attention, authored, and requested respectively. Repeating
a shortcut clears that Mine selection; other filters remain. If account lookup
fails, Mine filters cannot match. Partial reviewer or review history can omit
matches and is identified in the coverage information.

Sort by Updated, Repository, Author, Title, or Attention; `S` reverses the whole
order. `A` selects author order and `z` opens the sort picker. Updated starts
newest first. Attention puts requested reviews first, then failing checks,
requested changes, approved open PRs, and other PRs; older updates come first
within a category, with unknown dates last.

Your author login is highlighted. A `!` beside the age means no update for at
least 14 days at the recorded check time. A `+` marks updates since last look.
These markers differ from stale data. Details include requested reviewers,
changed files, merge-conflict evidence, and completeness qualifications.

## Refresh, coverage, and bookmarks

Live startup loads existing cached history while GitHub refresh is pending.
Without a usable cache, history appears after the first clone and scan. Later
refreshes are manual through **Refresh** or `r`; there is no periodic polling of
GitHub. Refresh rediscovers sources so new repositories become eligible and
repositories no longer listed stop being tracked. Discovery failure preserves
the existing set where possible and appears in Data status.

Verified branch/revision probes can avoid unchanged Git fetches. Parsed scans
and unchanged PR file lists are reused when their inputs still match. PR data
is retained in memory with a one-minute reuse window, not persisted as an
offline PR cache. Rate limits pause further requests; no automatic retry runs.

**Data status** shows retained timestamps, request counts, errors, cache details,
and bookmark state. Failed refreshes preserve usable evidence and mark it stale
or partial. Inventory, changed files, and reviewer coverage are separate:
missing files can make directory associations unknown even with a complete PR
list. Unknown evidence is never a complete zero. Cached history mode has no PR
context.

PR collection is bounded to 200 open PRs per repository and 1,000 current file
paths per PR, with a shared request budget (default 128) and bounded request
deadlines. Bodies, comments, diffs, and check logs are not fetched. Git history
uses Bigboard-owned caches, authenticated through `gh` for remote operations.

Last-look bookmarks are separate for all repositories and each named group.
Their baseline stays fixed during a session. Normal native-app exit advances
eligible bookmarks after a fetched snapshot with usable history. Offline scans,
identity rescans, and export do not advance them. Since last look is an exclusive
timestamp filter, not a full PR event history; closed/merged PRs are absent from
the open-PR list.

## Storage and export

| Data | Default location |
| --- | --- |
| Analysis rules and initial sources | `~/.config/bigboard/config.json` |
| Saved workspace | `~/.config/bigboard/desktop.json` |
| Owned repository caches | `~/.cache/bigboard/repos/github.com/owner/name.git` |
| Last-look bookmarks | `~/.local/state/bigboard/last-seen.json` |

`XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, and `XDG_STATE_HOME` override the respective
base directories. Existing configurations, caches, and bookmark timestamps are
reused. Full bare caches cover default-branch history; developer checkouts are
not scanned or modified. Disposable parsed scans are rebuilt when their inputs
change or their files are damaged.

**Repository controls** stages session inclusion, then **Apply tracking** applies
it. Excluding a repository does not remove its cache. A cache can be removed
only when a successful discovery no longer lists its repository. **Remove cache**
requires a separate confirmation; group selection or filtering alone does not
make a cache removable.

The copy button (or `y`) copies the current view as Markdown. **Export JSON** on
Contributors opens a native Save dialog for all-time contributor statistics in
the currently loaded tracked snapshot. Viewing dates, search, and facets do not
limit it. Export makes no network requests and contains no PR data; partial
source evidence stays partial. The JSON includes contributor totals, date
bounds, AI/bot classification, and repository subtotals. Date bounds alone cannot
reconstruct shorter-period totals.

## Configuration reference

Use Settings and Identities for routine changes. Advanced analysis preferences
are loaded from `config.json` at startup. Invalid JSON, unknown fields, and
unsupported values report an error. The saved workspace in `desktop.json`
overrides initial sources without rewriting analysis rules. Identity editing
updates only the `identities` field.

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
| `personal_account`, `organizations` | Initial discovery sources before a workspace is saved |
| `exclude` | Initial basename, `owner/name`, or glob exclusions; saved workspace choices use exact exclusions |
| `groups` | Named `owner/name` lists within configured sources, selected through the group picker |
| `since` | Startup preset: `1d`, `7d`, `14d` (default), `30d`, `90d`, `1y`, or `all` |
| `sort` | Contributors: `commits`, `added`, `removed`, `net`, `ai`, or `total` (default); `impact` aliases `total` |
| `all_files` | Include generated/vendored paths in line counts; default false |
| `pr_request_budget` | Positive cap per remote refresh; default 128; does not bypass cooldowns |
| `ai_identities` | Additional AI author/co-author emails or `@domain` suffixes |
| `bot_identities` | Additional bot emails, `@domain` suffixes, or exact author names |
| `identities` | Canonical emails, aliases, and optional display names |
| `work_areas` | Named literal file/directory prefixes for related evidence |
| `theme`, `fuzzy` | Accepted for compatibility; they do not control GUI appearance or aggregation |

Identity example:

```json
{"identities":{"dev@example.com":{"aliases":["dev@personal.example"],"name":"Example Developer"}}}
```

Emails are trimmed/lowercased and must be unique across the mapping. Aliases
apply after `.mailmap`, before AI/bot classification. Missing-email identities
remain repository-scoped. Names never merge otherwise distinct identities.

Named work-area example:

```json
{"work_areas":{"example-org/api":[{"name":"Authentication","paths":["services/auth","apps/web/auth"]}]}}
```

The longest literal prefix wins. Names must be unique per repository and a
prefix cannot belong to multiple areas. Unmatched paths retain automatic groups;
named areas do not replace Activity’s directory hierarchy.

## Counting and troubleshooting

Git totals cover default-branch history and exclude merge commits. Generated and
vendored line changes are excluded by default. Unknown line counts still count
as commits without becoming measured lines. Shared Git object IDs count once
globally; repository associations and directory subtotals can overlap. Future
author dates remain included. AI percentages describe detected authors/co-authors,
not inferred tool use. Bots are included unless hidden on the current board.

- **Discovery or authentication fails:** run `gh auth status`, sign in if needed,
  check access, then retry Discover or Refresh.
- **No matching rows:** inspect shared scope and board filters. Reset this board
  preserves dates/repositories; Reset shared restores All time/all tracked repos.
- **Stale or partial data:** inspect Data status and refresh after resolving the
  reported failure or cooldown.
- **A shortcut enters text:** finish or leave the input field. F1 still opens help.
- **An external-edit error blocks saving:** cancel and restart to load the changed
  configuration before editing again.
