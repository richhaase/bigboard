# Bigboard user reference

[Installation and overview](../README.md)

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

Open Settings from the app menu or with Command-comma. Escape cancels the draft.
Its sections are:

| Section | Purpose |
| --- | --- |
| Repositories | GitHub account, available repositories, and saved collection choices. |
| Groups & areas | Named repository groups and named sets of paths within a repository. |
| General | Startup date range, contributor sorting, and cached-history mode. |
| Advanced | Generated-file counting, PR request budget, repository exclusion patterns, and extra AI/bot identities. |
| Storage | Inspect owned caches and remove eligible unavailable repositories. |

Repository IDs entered in groups and work areas are saved in lowercase, matching
GitHub discovery.

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

Credentials are stored in macOS Keychain. Sign out removes the local credential;
if removal fails, the account remains signed in and Settings stays open so you
can retry. GitHub authorization is revoked separately in GitHub's application settings.

## Navigation and scope

The sidebar contains **Heat Maps**, **Activity**, **Contributors**, and **Pull requests**, in that order.
The toolbar's **Repositories** control combines group selection and repository
filtering. Its label shows the active group and any narrower repository selection.
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
collects. Changing a named group narrows collection to its saved, selected
repositories and refreshes; it never adds unchecked repositories. Choosing a group
resets the viewing checklist to all repositories in that group.

The date menu offers the last 1, 4, 12, or 24 hours, 7, 14, 30, 90, and 365 days,
All time, and **Custom range…**. The default is 14 days unless
changed in Settings. Custom days accepts 0 for all time, up to 3650 days.
The custom-range sheet changes only the date range when you choose Apply.
Heat Maps, Activity, and Contributors share that range. Pull requests shows
current open work regardless of age and does not display the date control.

Each historical board has a **People** control for contributor selection and
**Hide bots**; changes apply immediately and remain independent across boards.
Search appears for commits, people, and PRs. Heat Maps, directory browsing,
and Related use drill-down instead of text search. Commit and people searches
within Activity are independent.

Ranges roll forward with the displayed snapshot when data refreshes. Refreshing
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

Darker blue means more activity. The overview and area rows use the same linear
scale, from zero to the largest cell count in the current view. Hover to see the
interval and actual count. Missing, incomplete, or stale history is marked with
dashed outlines and known-activity labels; a zero in those cells does not establish
that no activity occurred.

Select a repository or directory name to explore it within Heat Maps. Select a
cell to explore that interval in Activity's **Directories** view. The arrow beside an area
opens Activity for the whole selected range. Activity labels the interval carried from Heat Maps and offers
**Back to Heat Maps** and **Use shared date range**. Changing the shared date range
also clears that interval. Switching boards preserves each board's state.

## Activity

Activity opens with **Directories** for additions and removals in the selected
range. You can start here directly or arrive from Heat Maps. Select a file to see
its **Commits**, use **People** to inspect contributors to the current scope, or
use **Related** to find shared commits or contributor overlap with other areas.
Open repository PRs to inspect current review and check evidence.

Breadcrumbs and Back move to broader scopes while keeping board filters applied.
Directory totals include only files in that directory. Additions and removals
stay separate even when rows are sorted by commits, recency, or name.

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

This board lists current open PRs, including drafts, within the active group
and repository viewing scope. Historical dates do not hide old open PRs,
including those linked from Activity. Search
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

**Settings → General → Use cached history only** reads eligible owned history
without GitHub refreshes. After verified access, a restart with the same account
can use its saved offline grant. Quitting while an access check is pending keeps
that previously verified offline access. New sign-in and account changes require
live verification. Confirmed access denials hide affected evidence; a failed save
of access settings may require live verification again.

Durable account-scoped JSON lives in Application Support; rebuildable Git
mirrors and parsed scans live in Caches. The app never scans developer checkouts.
**Settings → Storage** offers cache removal only after a live refresh verifies
that a repository is unavailable. Removal affects the owned local mirror, not
GitHub. Settings discovery and cached-history mode alone do not authorize it.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| A repository is missing from Settings | Use **Manage access…** to check the GitHub App installation, including organization approval and SSO. Return and refresh the repository list. |
| A selected repository is missing from a board | Check the toolbar repository filter, selected group, and Advanced exclusion patterns. Settings controls collection; viewing filters narrow the results. |
| A view is empty or a count includes `+ ?` | Check the date range and board filters, then open Data Status. Incomplete coverage does not establish that no activity exists. |
| Data stays stale | Check **Use cached history only** in Settings → General. Data Status identifies access errors, request limits, and unfinished collection. |
| Git is unavailable | Install Git through Homebrew or Xcode Command Line Tools, then reopen Bigboard. |
| Settings will not save | Resolve the validation error shown in the sheet. If settings changed externally, cancel and reopen before editing again. |

## Counting and export

Commits shared across repositories are deduplicated by object ID while keeping
their repository associations. Complete line counts take precedence over
unknown counts. Generated, vendored, and bulk-data changes are excluded from
line totals by default, but their commits remain visible. Advanced settings can
include those lines. Missing line counts are shown as unknown, not measured zero.

The copy button or Command-Shift-C copies the current view as Markdown. **Export JSON** on
Contributors opens a native Save dialog and exports all loaded tracked history,
ignoring viewing filters. Neither export fetches data.

## Keyboard controls

The Boards menu lists available commands and their shortcuts. Open Keyboard
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
| ⌘[ | Back in Heat Maps or Activity |
| ⌘⇧C | Copy current view as Markdown |
| ⌘Return | Open selection on GitHub |
| ⌘⌥1–4 | Activity: Directories, Commits, People, Related |
| ⌘⌥M, ⌘⌥A, ⌘⌥R | PRs: needs my attention, authored by me, review requested |
| Return or right arrow | Explore the selected directory row |
| Escape | Dismiss the active popover or sheet |
