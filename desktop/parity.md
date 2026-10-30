# Bigboard workflow coverage

This checklist describes the GUI’s supported workflows. The detailed behavior
is documented in the [user reference](../docs/reference.md).

| Workflow | Behavior |
| --- | --- |
| Initial view | Activity at 14 days; honors configured `since` and contributor `sort` |
| Shared scope | 1, 7, 14, 30, 90, 365 days, All time, custom 1–3650 days, Since last look, and repository selection |
| Board state | Separate search, bot visibility, sort/reverse, selection, navigation, and scroll; Back preserves applied filters |
| Filters | Draft/apply/cancel; OR within categories, AND between them; independent board/shared resets |
| Activity | Repository → directory → file → commits; scoped counts and contributors; quiet repositories remain visible |
| Secondary evidence | Commits, People, Related, and Heat lenses; shared commits, contributor overlap, and configured named work areas |
| Repository links | Verify paths against the retained scan revision before opening GitHub |
| Contextual PRs | Repository PR view temporarily omits saved PR facets, then restores the normal PR board |
| Contributors | Canonical identities; metric sorts; repository distribution; daily/weekly/monthly timeline; calendar; AI/net/churn metrics |
| Identities | Rename or merge emails, choose primary, review before saving, then rescan cached history |
| PR triage | Author/review/check/state facets; Needs my attention, Authored by me, Review requested from me, Updated since my review |
| PR details | Five sorts and reverse; own-login highlight; inactivity/bookmark markers; mergeability, files, reviewers, and coverage |
| Data collection | Cache first; manual refresh; rediscovery; verified branch probes; unchanged-file reuse; shared request budgets |
| Failures | Retain usable evidence; report stale/partial state; distinguish unknown inventory and file evidence from zero |
| Repository management | Saved workspace, session tracking, named groups, cache usage, confirmed removal of unlisted owned caches |
| Bookmarks | Existing state file; separate global/group timestamps; fixed session baseline; normal-exit saves after usable fetched snapshots |
| Export | Current view as Markdown; all-time tracked contributor statistics as JSON through the native Save dialog |

Calendar grouping uses local time. Date ranges and ages use retained snapshot
times. Shared Git object IDs count once globally while repository associations
remain available. Unknown line counts remain qualified, and future-dated commits
remain included. Both runtime modes use actual GitHub sources or owned caches.

## Verification scope

Backend regressions cover configuration, cache-first startup, failures, probes,
PR reuse and coverage, groups, bookmarks, identity editing, repository controls,
export, and retained-revision links. Frontend model tests cover date boundaries,
canonical attribution, large histories, facets, ordering, timelines, and related
evidence. Isolated browser checks exercise the real controls, draft and
confirmation flows, loading/empty/error states, and narrow layouts; fixtures
exist only in the test runner.

The macOS bundle contains the backend and frontend assets. Git is the external command-line
dependency declared by the release cask. Native GitHub sign-in uses PKCE and
Keychain; API and owned-cache Git authentication do not depend on GitHub CLI. Release signing and
notarization are handled by the tag workflow; local builds do not establish that
a release has passed those checks. See the [development guide](README.md).
