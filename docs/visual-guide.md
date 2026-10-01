# Worklist visual guide

[Back to the README](../README.md)

Bigboard starts with repositories, path-based work areas, the contributors who
worked there, and concrete commit evidence. Run `bigboard ~/src/` for multiple
repositories or `bigboard /path/to/repo` for one. A single included repository
opens directly into its work areas.

These are real terminal captures of Bigboard's public Git history, using the
14-day range with bots shown. Paths that no longer exist may still appear because
history in the selected range touched them. GitHub PR context was unavailable in
the capture environment; local history remains usable.

## Scan work, then inspect its evidence

![Worklist at 120×36](images/worklist-wide.png)

The top list reads **area → latest contributor + others → latest commit → age →
PR context**. Recent is the default order. `s` cycles Recent, Name, and Activity
(commit count); volume is not priority or productivity.

- `↑/↓` or `j/k` selects a row; the evidence underneath follows that selection
- `Enter` opens repository → work areas → area detail → full commit evidence
- `+N` beside the latest author is the number of other distinct contributors in
  that local range and bot filter
- Contributor counts below the list are scoped commit counts. `+N more` means
  the preview is bounded; `2` opens the full alphabetical People list
- Ages use each repository's last successful local scan. They do not tick or
  imply fresh data. Future author dates say `future`
- `←/→` changes the time range; `b` toggles bots; `/` searches the current list

A commit can appear in several areas. Area counts overlap and must not be summed.
Shared contributors describe historical overlap, not live presence, ownership,
or proof of collaboration. GitHub handles are not merged with Git identities.

## Compact list → detail

![Compact area list at 80×30](images/worklist-compact.png)

At 80 columns, each item uses two lines plus breathing room, keeping both the
contributor and concrete subject visible. At 40 columns, it uses three lines.
`Enter` replaces the list with the selected area's evidence. `Esc` restores the
same list selection and scroll position.

![Compact area detail at 80×30](images/worklist-detail.png)

The wide evidence layout starts at **110 columns × 28 rows**. Smaller terminals
use one primary surface. Minimum size is **40 × 18**. `PgUp/PgDn` pages through
lists, and `g/G` jumps to the first/last item. `?` shows all controls.

## Follow people, related areas, and subareas

`Tab` / `Shift+Tab` or `1`–`4` switches the focused lens:

1. **Activity:** real commits with author dates, contributor identities, subjects,
   and short object IDs. `Enter` opens the full subject and all changed paths
2. **People:** alphabetical canonical identities. `Enter` filters Activity to
   that person; `Esc` clears the filter and returns to People
3. **Related:** exact shared-contributor counts. `Enter` opens the corresponding
   identities and their evidence
4. **Subareas:** one literal directory level deeper, with direct files as a leaf.
   Named configured groups keep their definitions

`Esc` unwinds navigation. If a search is active, it clears the search first.
The selected scope and contributor identities are retained across sorts,
resizing, refreshes, and detail round trips. Missing evidence never silently
retargets an open commit inspector to another commit.

## Local and PR freshness are separate

**Local** is the last successful scan for the selected repository. A failed scan
retains that repository's previous evidence with **STALE** shown before the name.
**Updating…** keeps the existing screen usable during a refresh.

**PRs** describes the selected repository's independent all-open snapshot. A
failed or incomplete request remains **STALE**, **PARTIAL**, or **unavailable**.
The checked time is an attempt time; retained evidence also reports its last
complete fetch when known. Unknown inventory is never shown as zero.

`p` opens the selected parent-area/repository PR inventory; `P` opens all included
repositories. PRs use **all open dates**, independent of local range, bot, person,
and commit-search filters. Subarea PR context is explicitly labeled parent-area.

GitHub context uses an already installed and authenticated `gh` CLI. Bigboard
never signs in, requests new access, or fetches Git objects. Both local and remote
loads happen on launch or explicit `R`, with existing remote budgets and cooldowns.
There is no timer refresh. Use Git separately to update cached remote history.

## Contributor statistics

`v` opens the statistics leaderboard; `Enter` opens contributor detail. `PgUp` /
`PgDn` scrolls that detail, `Home` / `End` jumps to an edge, and `↑/↓` switches
contributors. `Esc` returns to the leaderboard; `v` returns to Worklist.

## Reproducible stress capture

The opt-in `TestWorklistTerminalCapture` test runs the actual Bubble Tea model
with explicitly synthetic Unicode, long-name, 120-contributor and retained
partial-PR evidence. It is skipped by normal tests and is not a production mode:

```sh
go test -c -o /tmp/bigboard-tui.test ./tui
BIGBOARD_TEST_CAPTURE=stress /tmp/bigboard-tui.test -test.run '^TestWorklistTerminalCapture$'
# BIGBOARD_TEST_CAPTURE=detail starts in area detail
```

Use a real terminal at the size being checked; `q` exits. The README and the
screenshots above use real repository history, not this stress fixture.
