---
name: explore
description: Open and discuss BigBoard Activity and Pull requests views, explain a selected interval or PR, configure collection sources, or change repository filters and comparisons.
---

# BigBoard exploration

Use the bundled BigBoard tools. The collection selection is shared; each board
keeps independent viewing filters. Git activity, PR merges, and current open
PRs describe different events. Merged does not establish deployed.

## Sources and freshness

Call `list_sources` before changing collection settings. Use
`configure_sources` with either explicit absolute clone `paths` or
`import_native:true` when the user asks to reuse native BigBoard selection.
Pass the current `expected_revision`. This replaces saved sources and clears
collected evidence; filtering a board is a separate operation.

Call `refresh_sources` when current evidence is requested or no collection
exists. It can take up to 150 seconds; allow sufficient tool timeout. It reads
existing remote-tracking default branches without fetching or modifying the
clones and collects GitHub open and merged PRs with GitHub CLI authentication.
Review its coverage result before describing findings as complete. Do not
repair authentication or modify clones as a recovery step.

After server restart, refresh explicitly to revalidate GitHub access before
using cached PR evidence. Displayed Git activity still comes from the saved
local scan. Collection time and scanned revisions are part of the answer.

## Open, inspect, and discuss

Call `open_board` without `board_id` for a new independent board, or with an
exact existing ID to reopen it. Use app selection context to identify the
board, snapshot, active view, and selected interval or PR. If the intended board is
ambiguous, ask the user to select it or supply its ID.

Call `read_board`, then `query_evidence` with the exact board and snapshot IDs.
For Activity use `kind:activity` and its selected `bucket_start` when present.
For Pull requests use `kind:pull_requests`, its `pull_request_state` (open or
merged), and selected `pull_request_id` when present. Do not carry a hidden
Activity interval into a PR query. If no view context exists, use the user's
question to choose the evidence kind. If the selection's snapshot differs
from the current board, disclose that change and refresh the selection before
attributing records. Follow `nextCursor` with the same snapshot, bucket, kind,
and PR filters to inspect more evidence. Shortened record IDs or file lists are not exhaustive.

Explain commits using their author dates and exact links. Merged PRs follow
the selected interval and default branch; open PRs remain current context as
of collection time, including older drafts. Keep incomplete coverage and
unknown line counts visible; zero under incomplete coverage is not absence.

## Change the board scope

Call `read_board`, then `update_board` with its `board_id` and current
`expected_revision`. Controls are `days` (7 or 14), `repository` (`all` or an ID
returned by `list_sources`), and `compare`. These do not change collected
sources or start network requests. Reconcile revision conflicts with the
latest state before retrying.

Activity and Pull requests are display tabs in the app. `update_board` changes
the shared board query, not the active tab; do not claim to switch tabs with
that tool. The existing panel reads the same saved board. Subsequent reads
and updates need no new panel. Report the resulting scope and revision without claiming a
rendered change unless observed. Open a new board only when requested.
