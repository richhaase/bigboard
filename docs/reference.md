# Bigboard user reference

[Installation and overview](../README.md)

## Setup and settings

Bigboard uses the active [GitHub CLI](https://cli.github.com) account on
github.com. Run `gh auth login` before opening Bigboard; if the CLI is missing or
signed out, Bigboard explains what to run and offers **Check again**.
Bigboard has no default source folders. On first launch it asks you to choose
the folders that hold your Git clones; change them at any time with **Add
folder…** and the remove buttons in **Settings → Repositories**, which lists the
GitHub clones found up to four levels deep in those folders. A clone maps to `owner/name` through its
`upstream` remote, otherwise `origin`, otherwise its first GitHub remote.
Worktrees of one repository count once; if you have several clones of the same
repository, the first by path is used. Choose **Refresh repository list** after
cloning. Repositories without a local clone cannot be collected.

History comes from each clone's remote-tracking default branch, such as
`origin/main`. Refresh fetches only that ref from the clone's own remote, the
same way `git fetch` does in Terminal: your SSH keys, credential helper, and Git
config apply. It never prompts; a fetch that would need a password, passphrase,
or host-key confirmation fails, and the repository keeps its local history. It
never changes your local branches, the index, working files, or Git config, and
it never runs maintenance in your repository. Local commits and uncommitted work
are not counted until they reach the default branch on GitHub. Shallow clones
show only the history they contain, and partial (blobless) clones show commits
with unknown line counts and paths. Pull request evidence uses the GitHub CLI
token; organizations that require SSO must authorize it (`gh auth refresh` or
GitHub's SSO prompt). History collection requires Git,
available through Homebrew or Xcode Command Line Tools.
Bigboard discovers the Git executable from PATH and standard Homebrew/system locations.

Select repositories individually or use **Select all**, then **Apply**. The
list supports search and sorting by latest commit or name. Each row shows the
date of the latest commit on the clone's remote-tracking default branch, the
clone's path, and whether it is shallow or partial. Apply saves the workspace and closes Settings; collection continues in
the main window. Errors keep the draft open. Cancel discards the draft. Newly
available repositories remain unchecked until you select and apply them.
Repository exclusion patterns in Advanced can further narrow the saved selection.

Open Settings from the app menu or with Command-comma. Escape cancels the draft.
Its sections are:

| Section | Purpose |
| --- | --- |
| Repositories | GitHub CLI account, source folders, the clones found in them, and saved collection choices. |
| Groups & areas | Named repository groups and named sets of paths within a repository. |
| General | Startup date range, contributor sorting, automatic refresh interval, and cached-history mode. |
| Advanced | Generated-file counting, PR request budget, repository exclusion patterns, and extra AI/bot identities. |
| Storage | Show where settings and parsed history are stored. |

Repository IDs entered in groups and work areas are saved in lowercase, matching
the IDs of discovered clones.

Area paths are repository-relative prefixes, such as `src/ui` or `docs`. One
trailing slash is optional, so `src` and `src/` cannot be assigned separately.
Paths containing `.` components, repeated slashes, or conflicting prefixes are
rejected when saving.

To rename a repository group, choose **Edit**, change its name, then **Update
group**. This replaces the original group in the draft. An existing group name
is rejected without overwriting either group. **Cancel edit** discards the edit;
**Apply** saves the settings draft.

To rename or move a work area, choose **Edit**, change its name or repository,
then **Update area**. This replaces the original entry in the draft; an existing
destination area is preserved and reported as a conflict. **Cancel edit** leaves
the draft unchanged. **Apply** saves the complete settings draft.

Bigboard stores no credentials. It reads the GitHub CLI token when it needs one
and keeps it only in memory. To switch accounts, run `gh auth switch` and choose
**Check again**; settings and caches are shared across accounts. To disconnect,
run `gh auth logout`.

## Navigation and scope

The sidebar contains **Heat Maps**, **Activity**, **Contributors**, and **Pull requests**, in that order.
The toolbar's **Repositories** control combines group selection and repository
filtering. Its label shows the group whose repositories are checked, or how many
repositories are checked when the selection matches no group.
**Manage Groups…** inside that popover opens Groups & areas in Settings.
Groups are also available in **Boards → Group**.
The toolbar contains repository viewing scope and, on historical boards, the
date range. Data Status opens from the status and timestamp at the bottom right.
Each board retains its own applicable search, sorting, filters, and selection.

The toolbar's **Repository filter** is one checklist, initially showing all
loaded repositories checked. It stays open while changes apply immediately
across all boards. Reopening it shows the current selection. Search narrows the
list; **Select all loaded** checks every loaded repository. When only one
repository remains checked, its checkbox is disabled until you check another.
Closing the picker keeps the selection already applied.
Press Return in the search field to move to the results, use the arrow keys to
select a repository, and press Space to toggle it.

This filter does not fetch history, make network requests, change saved
collection choices, or unload caches. Use Settings to change what the app
collects. Choosing a named group checks that group's collected repositories in
the viewing checklist and nothing else; it never refreshes or changes what is
collected. Groups with no collected repositories are unavailable. The
`bigboard` command's `--group` option still limits what a command collects.

The date menu offers the last 1, 4, 12, or 24 hours, 7, 14, 30, 90, and 365 days,
All time, and **Custom range…**. The default is 14 days unless
changed in Settings. The custom-range sheet offers **Recent days** (0 for all
time, up to 3650 days) or **Between dates**, which covers both chosen days. It
changes only the date range when you choose Apply. A custom range is shown in
the date menu by its dates and checked there; choosing a preset replaces it.
Custom ranges stay fixed when data refreshes.
Heat Maps, Activity, and Contributors share that range. Pull requests shows
current open work regardless of age and does not display the date control.

Each historical board has a **People** control for contributor selection and
**Hide bots**; changes apply immediately and remain independent across boards.
Search appears for commits, people, and PRs. Heat Maps, directory browsing,
and Related use drill-down instead of text search. Commit and people searches
within Activity are independent.

Recent ranges roll forward with the displayed snapshot when data refreshes. Refreshing
keeps the selected duration. Each session starts with the configured default range.

Historical boards use Git author dates. Date filtering is relative to the
retained snapshot and uses an exclusive lower boundary. Changing the date range
or viewing checklist makes no network request.

## Heat Maps

Heat Maps shows actual activity in the selected date range. Choose Lines,
Commits, or People. Totals cover the whole range; People counts distinct
contributors rather than adding the cell counts.

Cells adapt to the selected duration: hourly through one day, daily through 31
days, weekly through 180 days, and monthly for longer ranges. Calendar boundaries
use your local time zone and first weekday. Edge cells may be partial. All time
includes all loaded history.

Darker blue means more activity, in four shades plus empty. By default each area
row is shaded against its own busiest cell, shown as its peak beside the row's
total, so a quiet repository still shows its pattern next to a busy one.
Compare rows by their totals, or turn on **Shared scale** to shade every row
against the busiest cell in any row; the legend then shows the count where each
shade starts. The overview strip always uses its own scale, since it totals
every row. Shades follow the square root of each count. Hover to see the
interval and actual count. Missing, incomplete, or stale history is marked with
dashed outlines and known-activity labels; a zero in those cells does not establish
that no activity occurred.

Select a repository or directory name to explore it within Heat Maps. Select a
cell to explore that interval in Activity's **Directories** view: the cell's
interval becomes the custom date range shown in the toolbar for every
historical board, and Back returns to the previous range. Choose another range
in the date menu to widen it again. The arrow beside an area opens Activity for
the whole selected range.
Switching boards preserves each board's state.

## Activity

Activity opens with **Directories** for additions and removals in the selected
range. You can start here directly or arrive from Heat Maps. Select a file to see
its **Commits**, use **People** to inspect contributors to the current scope, or
use **Related** to find shared commits or contributor overlap with other areas.
Open repository PRs to inspect current review and check evidence.

Breadcrumbs and **Go → Enclosing Scope** (Command-Up Arrow) move to broader
scopes while keeping board filters applied.
Directory totals include only files in that directory. Additions and removals
stay separate even when rows are sorted by commits, recency, or name.

**Commits** shows commit subjects, authors, dates, and measured line changes.
Commit subjects marked with an arrow open GitHub. Right-click a commit to copy
its link, SHA, or subject, or to show its author in Contributors. Lists show an
initial batch and offer more as needed.
**People** shows contributors to the current scope and their commits.
**Related** compares shared commits or historical contributor overlap with
other areas. Open a related result to inspect the evidence; overlap is not
ownership or current presence.

Right-click a directory, file, or repository row on Activity or Heat Maps, or an
area under a contributor's **Where work happened**, to explore it, view it in
Activity, open it on GitHub, or copy its link or path. Double-click or press
Return on a directory row to explore it. Right-click a contributor to show their
work in Activity or copy their name or email.

**Open path on GitHub** checks the selected path against the scanned revision
in your clone before opening it. Missing paths report an error. Repository
PR context opens Pull requests for the selected repository with its own filters;
**Show All Repositories** or Back restores the previous PR filters.

## Contributors and identities

Select a contributor to inspect totals, contribution rhythm, the daily calendar,
repository distribution, and recent commits. Contributors are sorted by most
recent contribution unless you choose another default under **Settings →
General**. Sorting includes recency, line changes, commits, additions, removals,
net lines, AI share, and name. Bot and
people filters are independent of Activity's filters.
Select a repository under **Where work happened** to continue exploring in Activity.

Contribution rhythm uses daily, weekly, or monthly buckets according to the
range. The calendar shows daily activity in the selected range through the
snapshot date. All time starts with the earliest contribution in view. Weekly
buckets and the calendar grid use local time and your first weekday.
Hover over a calendar day to see its date and commit count.
The charts and totals describe recorded changes, not productivity.

**Identities** links email addresses belonging to the same person. Choose the
emails, a primary email, and an optional display name; **Stage identity**, then
**Review changes** and **Save identities**. Existing rules can be edited or
unlinked. Names alone never merge people. Repository `.mailmap` rules apply
before saved aliases. Saving identities updates settings and rescans cached
history without fetching. Conflicting external edits are rejected rather than
silently overwritten.

## Pull requests

This board lists current open PRs, including drafts, within the repository
viewing scope. Historical dates do not hide old open PRs,
including those linked from Activity. Search
matches repository, title, author, or number.

The table shows each PR's number, title, labels, repository, author, review and
check states, size in added and removed lines, when it was opened, and when it
was last updated. A dot marks PRs that need your attention, a count beside
Checks gives failing checks, and an hourglass marks PRs with no updates in 14
days. Click a column header to sort. Double-click a row, or press
Command-Return, to open the PR on GitHub; right-click to copy its link.

Selecting a PR opens a details pane below the table; drag the divider to resize
it, and close it with its close button or Escape. It shows dates, size, reported
merge conflicts, failing checks by name, latest reviews with dates, reviewers
still requested, and changed files. These details are
evidence, not branch-protection checks or merge authorization.

Author, Review, Checks, State, and Mine facets combine. Values within a facet
match any selected value; different facets combine with AND. Mine offers:

- **Needs my attention:** your PRs with failures or requested changes, your
  approved non-draft PRs with passing checks, individual requests for your
  review, and other authors' changes since your latest known review.
- **Authored by me:** the signed-in GitHub login is the PR author.
- **Review requested:** the signed-in login is individually requested; team
  membership is not inferred.
- **Updated since my review:** updates after your latest known submitted review.

Missing reviewer or review history can omit matches. A count marked
"(incomplete)" may be low; an empty incomplete inventory is not a verified zero. Cached-history
mode has no live PR context. Use Data Status to inspect coverage.

## Data status, offline work, and storage

Bigboard refreshes automatically every 15 minutes while visible. Choose Manual,
5, 15, 30, or 60 minutes under **Settings → General → Automatically refresh**.
The interval starts when the previous refresh finishes, including a manual
refresh. Automatic refresh pauses while Bigboard is out of view or a sheet is
open, then refreshes if overdue when you return. Cached-history mode disables
automatic refresh. Command-R and **Boards → Refresh** still refresh immediately.

Click the status and timestamp at the bottom right to open Data Status. During
refresh, this control shows the spinner and the current collection step. The
Boards menu and Command-Shift-D also open Data Status.

The sheet shows refresh phase, snapshot time, request counts,
per-repository timestamps and coverage, errors, and
storage locations. History, PR inventory, changed files, and reviewer evidence
have separate completeness signals. Explicit refresh preserves usable data on
transient failure and labels stale or incomplete evidence.

**Settings → General → Use cached history only** reads your clones without
fetching or contacting GitHub. Clones are found again on every refresh, so
cached history opens offline after a restart. Connecting a new GitHub CLI
account requires live verification. If a fetch fails, the repository is marked
stale with Git's message and its local history is still shown.

Settings live in `~/.config/bigboard/config.json` (or under `$XDG_CONFIG_HOME`)
and can be edited by hand at any time. A running Bigboard notices the change,
reloads it, and refreshes. If Settings or Identities is open with unsaved
edits, applying them is refused until you reopen it. Rebuildable parsed history
lives in `~/.cache/bigboard/` (or under `$XDG_CACHE_HOME`). Bigboard Dev uses
`bigboard-dev` for both. Git history itself stays in your clones.

## Command line

`bigboard` is the app's own executable, installed on your PATH by the Homebrew
cask. With no arguments it prints help, including notes for scripts and agents;
`bigboard open` opens the app. `bigboard help COMMAND` or `bigboard COMMAND
--help` shows a command's options. Commands use the active GitHub CLI account and
the same settings and caches as the app, so they can run while the app is open.

| Command | Result |
| --- | --- |
| `bigboard`, `bigboard help [COMMAND]` | Show general or command help. |
| `bigboard open` | Open the app. |
| `bigboard status [--json]` | Show the GitHub CLI account, data locations, and collected repositories. |
| `bigboard activity [options]` | Summarize recorded change in your clones: totals, repositories, top paths, and people. |
| `bigboard prs [options]` | List open pull requests with review and check state and what needs your attention. |
| `bigboard refresh [--repo OWNER/NAME] [--group NAME]` | Fetch each collected clone's remote-tracking default branch with your Git setup. |
| `bigboard config [get\|set\|add\|remove\|unset ...]` | Show or change settings, checked like Settings in the app. |
| `bigboard version` | Show the version. |

`activity` options:

- `--days N`: days to include. Defaults to the app's range; `0` includes all history.
- `--repo OWNER/NAME`: limit to a collected repository; repeat for several.
- `--group NAME`: limit to a saved repository group.
- `--top N`: rows per section (default 10).
- `--path PATH`: narrow everything to one file or folder in a single repository
  (requires exactly one `--repo`). Totals, excluded lines, people, and commits
  count only changes inside it, and the path section lists what is below it.
  Commits with unknown paths (partial clones) cannot be placed and are left out.
- `--since TIME`, `--until TIME`: an explicit interval instead of `--days`. Each
  takes a date (`2026-10-01`, local midnight) or an ISO 8601 time
  (`2026-10-01T12:00:00Z`). The interval starts just after `--since` and
  includes `--until`, matching Activity and Heat Maps cells, so a Heat Maps cell
  for Oct 1 maps to `--since 2026-10-01 --until 2026-10-02`. Without `--since`,
  it starts at the beginning of history; without `--until`, it runs through now
  and includes future-dated commits.
- `--commits`: list the commits behind the numbers, newest first and limited by
  `--top`, with lines added and removed within the selected path. JSON entries
  include the full `sha`, a GitHub `url`, `date`, `author` (name only), `subject`,
  `added`, `removed`, and `lines_unknown`.
- `--depth N`: group the path section `N` folders deep (1–10, default 1). At
  depth 2, a monorepo's `packages/` row becomes one row per `packages/<name>/`.
  Files shallower than `N` stay as file rows. Counts match Activity's
  Directories view.
- `--compare`: also measure the previous window of the same length, from `2N`
  days ago (exclusive) to `N` days ago (inclusive). The text adds a "Previous N
  days" line and, for repositories and paths, previous commits and a change
  column: `new` (no earlier activity), `quiet` (no current activity), or a
  percentage of commits. Repositories active only in the previous window are
  still listed. People are not compared. Requires `--days` above 0 or `--since`;
  with `--since`, the previous window has the same length and ends at `--since`. In JSON,
  `comparison` holds the previous window's `start`, `end`, and `totals`, and each
  repository and path row has `previous` totals.
- `--people-sort name|lines|commits`: order of the people section. The default,
  `name`, lists everyone active with the repositories they changed, without
  counts. `lines` and `commits` rank people with their counts, limited by `--top`.
- `--person WHO`: narrow everything to one person, as Contributors' person
  filter does. Give an email address (any of their aliases works) or their exact
  name, ignoring case; repeat for several people. A name shared by more than one
  person is refused with their emails so you can choose. Their bot activity is
  included. `scope.people` lists who the numbers cover.
- `--timeline`: add commits and lines added and removed per hour for ranges up
  to 2 days, per day up to 30 days, per week up to 180 days, and per month
  beyond that or for all history, the same buckets as the Activity board. Text
  shows a bar for each bucket's lines; JSON has `timeline` with a `unit`
  (`hour`, `day`, `week`, or `month`)
  and `points` with each bucket's `start`, `commits`, `added`, and `removed`.
- `--include-bots`: include bot accounts, which are hidden by default.
- `--json`: print JSON with snake_case keys. People are identified by name only;
  email addresses are omitted.

Totals are followed by the lines excluded from them, by reason: `generated`,
`vendored`, `fixture`, and `bulk` (`excluded` in JSON, omitted when nothing was
excluded).

`activity` reads your clones and never fetches. Like `status`, it confirms the
GitHub CLI account with GitHub, or uses the last confirmed account when offline.
Run `refresh` first,
or let the app refresh, for current evidence. Repositories without a local clone,
with shallow or partial history, or whose scan failed are listed under incomplete
evidence and the command exits with status 2. `status` lists each collected
repository's clone. Usage errors exit with status 64.

Every totals object in the JSON includes `unknown_line_commits`, the number of
commits whose line counts are unknown (partial clones). When it is above zero,
line totals are a lower bound. The JSON also has:

- `evidence`: one entry for every requested repository, including those with no
  activity in the range or a failed scan. Each entry has the `branch`, `remote`,
  scanned `revision`, `scanned_at` (when that history was parsed), and
  `fetched_at` (the last successful fetch by Bigboard or `bigboard refresh`;
  `null` if unknown, for example after fetching outside Bigboard). `history` is
  `complete`, `shallow`, `missing` (no local clone), or `failed`; `line_counts`
  and `paths` are `complete` or `unknown`. Its `problems` list each issue as
  `kind` and `message`.
- `problems`: every issue across repositories, as `repository`, `kind`, and
  `message`. Kinds are `no_clone`, `scan_failed`, `shallow`, `partial`, and
  `skipped_commits`.

`scope` echoes what the numbers cover: `repositories`, `group`, `path` and its
`kind` (`file` or `directory`), the effective `since` and `until` (`null` when
open), and `include_bots`. `generated_at` is when the report ran, not when the
history was fetched.

`prs` lists open pull requests, including drafts, in your collected repositories,
using the same facets, sort orders, and attention rules as the Pull requests
board. Pull requests are not cached, so it asks GitHub each time, within the
request budget in Settings → Advanced. Options:

- `--repo OWNER/NAME`, `--group NAME`: limit the repositories.
- `--mine attention|authored|requested|updated`, `--author LOGIN`,
  `--review approved|changes_requested|review_required|unknown`,
  `--checks success|failure|error|pending|expected|unknown`, `--state open|draft`:
  facets. Repeat an option to match any of its values; different options combine
  with AND.
- `--search TEXT`: match repository, title, author, or number.
- `--sort updated|attention|repository|author|title`: order (default `updated`).
- `--top N`: pull requests to list (default 25).
- `--files`: include changed file paths in JSON.
- `--json`: print JSON.

Text output marks pull requests that need your attention with `!` and says why.
The JSON has `viewer`, `filters`, `counts` (`open`, `matching`,
`needs_attention`), and `pull_requests`. Each entry has its review, checks
(with `failing_checks`), mergeability, size, labels, requested reviewers, latest
reviews, attention flags with an `attention_reason`, and `complete` flags. The
JSON also has `evidence` per repository (`open`, `inventory_complete`,
`files_complete`, `reviewers_complete`, `stale`) and `problems`. Incomplete
evidence exits with status 2; a rejected GitHub CLI token exits with status 1.

`config` shows or changes the settings file, so everything in Settings can be
scripted. It does not need GitHub. With no action it lists every setting;
`config get SETTING` prints one value per line. Changes are checked like
Settings: the file must still validate, repositories need a local clone in a
source folder, and at least one repository stays collected. A running app
reloads the change and refreshes.

- Lists: `sources` (folders that hold your clones; `~` and relative paths are
  expanded), `repositories`, `exclude`, `ai-identities`, `bot-identities`.
  `set` replaces, `add` and `remove` change entries, and `unset` clears.
- Named entries: `group NAME REPOSITORY...`, `work-area REPOSITORY NAME PATH...`,
  and `identity EMAIL NAME [ALIAS...]`. `add` and `remove` change a group's
  repositories, a work area's paths, or an identity's aliases; `unset` removes
  the entry. Identities that share an email are merged, as in the app.
- Values: `since-days`, `contributor-sort`, `refresh-interval`,
  `request-budget`, `cached-only`, `all-files`. `unset` restores the default.

```sh
bigboard config add sources ~/src
bigboard config add repositories owner/name
bigboard config set group Team owner/one owner/two
bigboard config set identity me@example.com "My Name" me@old.example
bigboard config set refresh-interval 30
```

`--json` prints the setting (or every setting) as JSON. Groups, work areas, and
identities are arrays of objects (`name`, `repositories`; `repository`, `name`,
`paths`; `email`, `name`, `aliases`). Rejected changes exit with status 1 and
leave the file unchanged.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| A repository is missing from Settings | Confirm its clone is within four folder levels of a source folder and has a GitHub remote, then choose **Refresh repository list**. Hidden folders and build or dependency folders such as `node_modules`, `build`, `vendor`, and `Library` are skipped. |
| A repository stays stale after refresh | Run `git fetch` in that clone in Terminal. Bigboard fetches the same way but never prompts, so load SSH keys into your agent and accept new host keys first. |
| A selected repository is missing from a board | Check the toolbar repository filter, selected group, and Advanced exclusion patterns. Settings controls collection; viewing filters narrow the results. |
| A view is empty or a count includes `+ ?` | Check the date range and board filters, then open Data Status. Incomplete coverage does not establish that no activity exists. |
| Data stays stale | Check **Use cached history only** in Settings → General. Data Status identifies access errors, request limits, and unfinished collection. |
| Git is unavailable | Install Git through Homebrew or Xcode Command Line Tools, then reopen Bigboard. |
| Settings will not save | Resolve the validation error shown in the sheet. If settings changed externally, cancel and reopen before editing again. |

## Counting and export

Commits shared across repositories are deduplicated by object ID while keeping
their repository associations. Complete line counts take precedence over
unknown counts. Generated, vendored, fixture, and bulk-data changes are excluded
from line totals by default, but their commits remain visible. Fixture changes
are data files (such as JSON, YAML, or CSV) inside folders named `fixtures`,
`fixture`, `__fixtures__`, `testdata`, `test_data`, `snapshots`, or
`__snapshots__`. Bulk data is a single data file with 2,000 or more changed lines. Advanced settings can
include those lines. Missing line counts are shown as unknown, not measured zero.

The copy button or Command-Shift-C copies the current view as Markdown without
fetching data. For JSON, use the `bigboard` command's `--json` reports.

## Navigation history

Back and Forward work like Finder: the toolbar arrows, **Go → Back**
(Command-[), and **Go → Forward** (Command-]) step through every board switch
and drill-down, including jumps between boards. Going back restores that
location's scope, view, selection, board filters, and date range; the
repository filter stays as it is. Selecting a row does not add a step, and
navigating somewhere new clears Forward. History lasts for the session, keeps
the last 50 steps, and drops locations for repositories that are no longer
collected.

## Keyboard controls

The Go and Boards menus list available commands and their shortcuts. Open Keyboard
Guide from the Help menu. Use Tab to move focus, arrows to move through lists,
and Space to toggle a focused checkbox.

| Control | Action |
| --- | --- |
| ⌘1, ⌘2, ⌘3, ⌘4 | Heat Maps, Activity, Contributors, Pull requests |
| ⌘F | Find commits, contributors, or PRs where search is available |
| ⌘⇧F | Open the board’s People or Mine filter |
| ⌘⌥D | Custom date range on historical boards |
| ⌘⇧I | Open the Repository Filter popover |
| ⌘⇧D | Open Data Status |
| ⌘R | Refresh repository evidence |
| ⌘, | Settings |
| ⌘[, ⌘] | Back, Forward through boards and drill-downs |
| ⌘↑ | Up to the enclosing repository, folder, or list |
| ⌘⇧C | Copy current view as Markdown |
| ⌘Return | Open selection on GitHub |
| ⌘⌥C | Copy GitHub link to the selection |
| ⌘⌥1–4 | Activity: Directories, Commits, People, Related |
| ⌘⌥M, ⌘⌥A, ⌘⌥R | PRs: needs my attention, authored by me, review requested |
| Return or right arrow | Explore the selected directory row |
| Escape | Dismiss the active popover or sheet |
