# Bigboard user reference

[README](../README.md) · [Workflow guide](visual-guide.md)

## Setup and settings

Choose **Sign in with GitHub** and authorize Bigboard in your browser. In
**Settings → Repositories**, **Manage access…** opens GitHub's app
installation page. Select the repositories it may read, return to Bigboard, and
choose **Refresh repository list**. Repository access is limited by both your
GitHub permissions and the installation; organization approval and SSO still
apply. GitHub CLI and personal access tokens are not required. History collection
requires Git, available through Homebrew or Xcode Command Line Tools.
Bigboard discovers the Git executable from PATH and standard Homebrew/system locations.

Select repositories individually or use **Select all**, then **Apply**. The
list supports search, recent-push and name sorting, and visible last-push dates.
Push dates can reflect branches outside the default-branch history shown on the
boards. Apply saves the workspace and closes Settings; collection continues in
the main window. Errors keep the draft open. Cancel discards the draft. Newly
available repositories remain unchecked until you select and apply them.
Repository exclusion patterns in Advanced can further narrow the saved selection.

Open Settings from the app menu, with Command-comma, or with `,` outside a text
field. Its sections are:

| Section | Purpose |
| --- | --- |
| Repositories | GitHub account, available repositories, and saved collection choices. |
| Groups & areas | Named repository groups and named sets of paths within a repository. |
| General | Startup date range, contributor sorting, and cached-history mode. |
| Advanced | Generated-file counting, PR request budget, repository exclusion patterns, and extra AI/bot identities. |
| Storage | Inspect owned caches and remove eligible unavailable repositories. |

Repository IDs entered in groups and work areas are saved in lowercase, matching
GitHub discovery. Existing mixed-case settings remain openable for repair; edit
and update those entries before applying settings. Repository names beginning
with a hyphen, such as `owner/-repo`, are supported.

Area paths are repository-relative prefixes, such as `src/ui` or `docs`. One
trailing slash is optional, so `src` and `src/` cannot be assigned separately.
Paths containing `.` components or repeated slashes are rejected when saving.
Previously saved dot components, repeated slashes, or conflicting prefixes remain
openable for repair.

To rename a repository group, choose **Edit**, change its name, then **Update
group**. This replaces the original group in the draft. An existing group name
is rejected without overwriting either group. **Cancel edit** discards the edit;
**Apply** saves the settings draft.

To rename or move a work area, choose **Edit**, change its name or repository,
then **Update area**. This replaces the original entry in the draft; an existing
destination area is preserved and reported as a conflict. **Cancel edit** leaves
the draft unchanged. **Apply** saves the complete settings draft.

Credentials are stored in macOS Keychain. Sign out removes the local credential;
if removal fails, the account remains signed in and Settings stays open so you
can retry. GitHub authorization is revoked separately in GitHub's application settings.
There is no import of settings or history from previous Bigboard versions. A
compatible existing Keychain sign-in can be reused, but keep the old app closed
because both versions share rotating credentials.

## Navigation and scope

The sidebar contains **Activity**, **Contributors**, and **Pull requests**.
The toolbar contains date scope, repository viewing scope, and Refresh.
Data Status opens from the combined status and timestamp at the bottom right.
A configured repository group adds a group picker. Each board retains
its own search, sorting, filters, and selection while you move between boards.

The toolbar's **Repository filter** is one checklist, initially showing all
loaded repositories checked. It stays open while changes apply immediately
across all boards. Reopening it shows the current selection. Search narrows the
list; **Select all loaded** checks every loaded repository. When only one
repository remains checked, its checkbox is disabled until you check another.
Closing the picker keeps the selection already applied.

This filter does not fetch history, make network requests, change saved
collection choices, or unload caches. Use Settings to change what the app
collects. Changing a named group narrows collection to its saved, selected
repositories and refreshes; it never adds unchecked repositories.

The date menu offers 1, 7, 14, 30, 90, and 365 days, All time, Since last look,
and a custom range through Filters. The default is 14 days unless changed in
Settings. Custom days accepts 0 for all time, up to 3650 days. Since last look
requires a saved bookmark. Filters stages shared scope and the current board's
search, sorting, and applicable people/bot filters until Apply.

Activity and Contributors use Git author dates; PRs use update times. Date
filtering is relative to the retained snapshot and uses an exclusive lower
boundary. Unknown dates match only All time. Changing viewing scope makes no
network request. Heat has a separate historical window, described below.

## Activity

Activity opens on **Heat**. Its lenses are Heat, Directories, Commits, People,
and Related. Explore a repository, directory, or file to narrow the evidence.
Selecting a file opens its commits. Breadcrumbs and Back move to broader scopes;
board filters remain applied. Directory totals include only files in that
directory. Additions and removals stay separate even when rows are sorted by
commits, recency, or name.

**Heat** shows 26 weekly buckets. Each week is compared with that row's preceding
12 weeks; the Change badge compares its latest four weeks with the 12 before
them. Amber means busier than usual, blue means quieter, and cream marks
activity after a quiet baseline. Choose Lines, Commits, or People. Heat keeps
its historical window regardless of the shared date range, while repository,
contributor, and bot scope still apply. It measures change relative to the
area's own history, not productivity or importance.

**Commits** shows commit subjects, authors, dates, and measured line changes.
Commit links open GitHub. Lists show an initial batch and offer more as needed.
**People** shows contributors to the current scope and their commits.
**Related** compares shared commits or historical contributor overlap with
other areas. Open a related result to inspect the evidence; overlap is not
ownership or current presence.

**Open path on GitHub** checks the selected path against the retained revision
in the local cache before opening it. Missing paths report an error. Repository
PR context opens Pull requests for the selected repository; **Back to Activity**
restores the previous PR filters and Activity location.

## Contributors and identities

Select a contributor to inspect totals, contribution rhythm, the daily calendar,
repository distribution, and recent commits. Sorting includes line changes,
commits, additions, removals, net lines, AI share, name, and recency. Bot and
people filters are independent of Activity's filters.

Contribution rhythm uses daily, weekly, or monthly buckets according to the
range. The calendar displays a fixed past-year grid using the selected evidence;
dates outside the selected range are empty. Calendar grouping uses local time.
The charts and totals describe recorded changes, not productivity.

**Identities** links email addresses belonging to the same person. Choose the
emails, a primary email, and an optional display name; **Stage identity**, then
**Review changes** and **Save identities**. Existing rules can be edited or
unlinked. Names alone never merge people. Repository `.mailmap` rules apply
before saved aliases. Saving identities updates settings and rescans cached
history without fetching. Conflicting external edits are rejected rather than
silently overwritten.

## Pull requests

This board lists open PRs within the shared date and repository scope. Search
matches repository, title, author, or number. Select a PR to inspect review and
check states, requested reviewers, changed files, latest reviews, and reported
merge conflicts. **Open on GitHub** opens the actual PR. These details are
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

Missing reviewer or review history can omit matches. A count with `+ ?` is
incomplete; an empty incomplete inventory is not a verified zero. Cached-history
mode has no live PR context. Use Data Status to inspect coverage.

## Data status, offline work, and storage

Click the status and timestamp at the bottom right to open Data Status. This
single footer control shows a spinner during refresh; the progress display at
the top of the main screen remains visible. The Boards menu and `e` also open
Data Status.

The sheet shows refresh phase, snapshot time, last-look
baseline, request counts, per-repository timestamps and coverage, errors, and
storage locations. History, PR inventory, changed files, and reviewer evidence
have separate completeness signals. Explicit refresh preserves usable data on
transient failure and labels stale or incomplete evidence.

**Settings → General → Use cached history only** reads eligible owned history
without GitHub refreshes. After verified access, a restart with the same account
can use its saved offline grant. Quitting while an access check is pending keeps
that previously verified offline access. New sign-in and account changes require
live verification. Confirmed access denials hide affected evidence; a failed save
of the replacement access list may require live verification again.

Durable account-scoped JSON lives in Application Support; rebuildable Git
mirrors and parsed scans live in Caches. The app never scans developer checkouts.
**Settings → Storage** offers cache removal only after a live refresh verifies
that a repository is unavailable. Removal affects the owned local mirror, not
GitHub. Settings discovery and cached-history mode alone do not authorize it.

Since last look uses a fixed baseline for the session. A normal quit saves
the latest eligible live snapshot time for the workspace/group as the next baseline.
Every repository in that collected scope must have current Git history and a
complete, nonstale PR list, with some commit history loaded. An incomplete refresh
keeps the previous eligible time so recovered activity stays in the catch-up range.
PR file and reviewer details can remain partial; those limits stay visible in Data
Status and do not block the bookmark. Viewing filters do not narrow this requirement.
Offline scans, identity rescans, and export do not advance it.

## Counting and export

Commits shared across repositories are deduplicated by object ID while keeping
their repository associations. Complete line counts take precedence over
unknown counts. Generated, vendored, and bulk-data changes are excluded from
line totals by default, but their commits remain visible. Advanced settings can
include those lines. Missing line counts are shown as unknown, not measured zero.

The copy button or `y` copies the current view as Markdown. **Export JSON** on
Contributors opens a native Save dialog and exports all loaded tracked history,
ignoring viewing filters. Neither export fetches data.

## Keyboard controls

Letter shortcuts apply when you are not editing text. Standard macOS controls
handle focus, selection, and window navigation. The Help menu opens Keyboard
Guide; the Boards menu provides the main commands.

| Control | Action |
| --- | --- |
| `a`, `c`, `p` | Activity, Contributors, Pull requests |
| `/` | Focus the current board's search |
| `f`, `t` or Command-Shift-F | Filters |
| `s`, Shift-S | Cycle sorting, reverse sorting |
| `x` | Reset current-board filters/search, retaining shared scope |
| `r` or Command-R | Refresh repository evidence |
| `e`, `i` | Data Status, Repository Filter |
| `,` or Command-comma | Settings |
| `y` | Copy current view as Markdown |
| `?`, F1 | Keyboard Guide |
| `b` | Toggle bot visibility |
| `1`, `2`, `3`, `4`, `5` | Activity: Commits, People, Related, Directories, Heat |
| Return / right arrow | Directories: explore the selected row |
| Escape | Close the open panel or move back through Activity |
| Left arrow | Move back through Activity when no panel is open |
| `o` | Open the selected path or PR on GitHub |
| `v` | Activity: open repository PRs |
| `m` | Activity: switch related evidence; Contributors: identities; PRs: attention |
| Shift-M, `n` | PRs: authored by me, review requested |
