---
name: explore
description: Open and discuss a BigBoard MCP activity board backed by local Git history and GitHub PRs, explain selected evidence, configure collection sources, or change viewing filters and comparisons.
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
board, snapshot, and optional selected bucket. If the intended board is
ambiguous, ask the user to select it or supply its ID.

Call `read_board`, then `query_evidence` with the exact board and snapshot IDs
and selected `bucket_start` when present. If the selection's snapshot differs
from the current board, disclose that change and refresh the selection before
attributing records. Follow `nextCursor` with the same snapshot and bucket to
inspect more evidence. Shortened record IDs or file lists are not exhaustive.

Explain commits using their author dates and exact links. Merged PRs follow
the selected interval and default branch; open PRs remain current context as
of collection time, including older drafts. Keep incomplete coverage and
unknown line counts visible; zero under incomplete coverage is not absence.

## Change the view

Call `read_board`, then `update_board` with its `board_id` and current
`expected_revision`. Controls are `days` (7 or 14), `repository` (`all` or an ID
returned by `list_sources`), and `compare`. These do not change collected
sources or start network requests. Reconcile revision conflicts with the
latest state before retrying.

The existing panel reads the same saved board. Subsequent reads and updates
need no new panel. Report the resulting scope and revision without claiming a
rendered change unless observed. Open a new board only when requested.
