# Desktop feature parity

The desktop uses the terminal's product behavior as its baseline: the
[user reference](../docs/reference.md), the Go aggregation code, and the PR
triage changes on `main`. Native controls replace terminal-specific layouts
and key sequences; the contributor detail layout remains graphical.

| Workflow | Desktop behavior |
| --- | --- |
| Initial view | Activity at 14 days; honors configured `since` and contributor `sort` |
| Shared scope | 1, 7, 14, 30, 90, 365 days, All time, custom 1–3650 days, Since last look, repository selection |
| Board state | Separate search, bot visibility, sort/reverse, selection, navigation, and scroll; Back preserves applied filters |
| Filters | Draft/apply/cancel; OR within categories, AND between them; independent board/shared resets |
| Activity | Repository → directory → file → commits; scoped counts and people; quiet repositories; contributor filter |
| Secondary evidence | Commits, People, Related; shared commits and contributor overlap; configured named work areas |
| Repository links | Verify paths against the retained scan revision before opening GitHub |
| Contextual PRs | Repository PR view temporarily omits saved PR facets, then restores the normal PR board |
| Contributors | Canonical email/mailmap identities, all metric sorts, repository distribution, daily/weekly/monthly timeline, calendar, AI/net/churn metrics |
| Identities | Rename or merge emails, choose primary, review before save; offline rescan; shared configuration with external-edit protection |
| PR triage | Author/review/check/state facets; Needs my attention, Authored by me, Review requested from me, Updated since my review |
| PR ordering/details | Updated, Repository, Author, Title, Attention, full reverse; own-login highlight, inactivity/bookmark markers, reviews/files/mergeability and coverage |
| Data collection | Existing cache first, manual refresh, rediscovery, verified branch probes, unchanged-file reuse, shared request budgets, one-minute PR reuse |
| Failures | Retain usable evidence, report partial/stale state, distinguish inventory/file/reviewer completeness; unknown evidence never becomes zero |
| Repository management | Persistent workspace settings, session tracking controls, named groups, cache usage, confirmed removal of unlisted owned caches |
| Bookmarks | Shared terminal state file, separate global/group timestamps, immutable session baseline, eligible normal-exit saves |
| Clipboard/export | Current view as Markdown; all-time contributor JSON through the terminal's aggregation/schema |

Calendar grouping uses local time. Range comparisons and visible ages use the
retained snapshot time. Shared Git object IDs count once globally while their
repository associations remain available. Unknown line counts are qualified;
future-dated commits remain included. Neither runtime mode supplies fabricated
records: both use the user's GitHub sources or owned Git caches.

## Verification

- Backend regression tests cover configuration, cache-first startup, failures,
  probes, PR reuse and coverage, groups, bookmarks, identity editing, repository
  controls, and retained-revision links.
- Frontend model tests cover date boundaries, canonical shared-commit identity,
  large histories, facets, ordering, timelines, and related evidence.
- Isolated browser checks exercise the real UI controls, confirmation/cancel
  behavior, board retention, loading/empty/error states, and narrow layouts.
  Those tests use intercepted fixtures only inside the test runner.
- A real-data check on October 29, 2026 compared the same cached snapshot with
  terminal JSON export: all-time totals and each contributor's name, commits,
  additions, removals, AI count, and bot classification matched across 23
  repositories and 139 contributors. All seven preset ranges also matched the
  Go aggregation when evaluated at the same snapshot time.

## Packaging boundary

`make gui-build` produces `build/bin/Bigboard.app` on macOS, with the Go backend
and web assets embedded. No separate server, Node, or Go installation is needed
at runtime. Git and an authenticated `gh` installation are still required for
live data; cached Git analysis still uses Git. This is one GUI application but
is not yet a distribution with all external tools included.

Release signing/notarization, installers, and automatic updates remain release
work. This pass validates macOS; it does not certify Windows/Linux packages.
The terminal's command-line flags and terminal theme controls remain available
in the terminal executable.
