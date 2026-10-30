# Bigboard workflow guide

[README](../README.md) · [User reference](reference.md)

Screenshots show live data from the public
[richhaase/bigboard](https://github.com/richhaase/bigboard) repository, captured on
October 29, 2026. The selected date range is visible in each board.

## Start with a question

| Question | Where to start |
| --- | --- |
| Where did change happen? | Activity |
| What did one person contribute? | Contributors |
| Which open PRs need attention? | Pull requests |

The sidebar switches boards. Date and repository controls at the top apply to
all three, while each board keeps its own search, sort, filters, and selection.
The initial date range is 14 days unless your configuration specifies another.

## Follow activity into its evidence

![Activity board with repository change bars and a scoped contributor inspector](images/activity.png)

On **Activity**, select a repository to inspect its contributors and recent
history. Use the row’s arrow, double-click, or Enter to explore directories and
files. Added and removed lines share one split bar; sorting by commits or recency
does not change what the bar measures.

![Directory activity with file-scoped change totals and contributor evidence](images/directories.png)

Activity opens on the **Heat** tab, followed by **Directories**, **Commits**,
**People**, and **Related**, each showing evidence for the current scope. Heat
colors each of the last 26 weeks against that row’s own previous 12 weeks, so
rising, falling, and new areas stand out. People opens a contributor’s scoped
commits. Related can compare exact shared commits or historical contributor
overlap; open a result to see its matching evidence. Breadcrumbs return to
broader scopes without clearing filters.

The inspector includes related open PRs and links to GitHub. **View repository
PRs** temporarily opens that repository’s PR view; **Back to Activity** restores
the normal PR filters. A `?` count means missing or stale evidence, not zero.

## Inspect a contributor

![Contributors board with repository distribution, timeline, and activity calendar](images/contributors.png)

On **Contributors**, select a person from the list. The detail pane shows totals,
repository distribution, contribution rhythm, a calendar, and recent commits.
Use the repository breakdown to jump into Activity. Bot visibility is separate
from Activity’s bot filter.

**Identities** lets you review a display-name change or merge email identities.
**Export JSON** saves all-time statistics for the currently tracked repositories;
the visible date and search filters do not limit that export. The copy button
instead copies the current view as Markdown.

## Triage pull requests

![Pull requests board with review and check evidence beside the selected PR](images/pull-requests.png)

On **Pull requests**, select a row to inspect checks, reviews, changed files,
requested reviewers, and merge-conflict evidence. **Open on GitHub** opens the
actual PR. Search and facets narrow the list; the Mine filter offers attention,
authored, review-requested, and updated-since-my-review views.

Sort by Updated, Repository, Author, Title, or Attention and use the arrow button
to reverse it. Your author login is highlighted. An age marked `!` has no update
for at least 14 days at the recorded check time; `+` means updated since your
last-look bookmark. Neither marker means the data itself is stale.

## Adjust scope and inspect gaps

![Filters dialog with shared viewing scope and board-specific choices](images/filters.png)

**Filters** stages shared dates/repositories alongside the current board’s
facets. Choose **Apply filters** or **Cancel**. **Custom…** adds custom days and
Since last look. **Reset this board** and **Reset shared** affect different parts
of the draft.

![Data status showing refresh coverage, storage, and bookmark information](images/data-status.png)

**Data status** explains refresh failures, coverage, cache storage, and bookmarks.
Use **Refresh** after resolving a problem; there is no periodic remote refresh.
**Settings** saves collection choices, while **Repository controls** changes
tracking only for this session. The shared repository picker only changes what
you see.

## Find keyboard controls

![Bigboard keyboard guide](images/help.png)

Use the keyboard guide (`?` or F1) for shortcuts. Normal Tab navigation and
buttons work throughout the app; F1 also works while typing.
