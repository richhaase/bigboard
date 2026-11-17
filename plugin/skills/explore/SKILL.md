---
name: explore
description: Open and discuss a BigBoard MCP activity board, explain a selected interval, or change its filters and period comparison. This milestone uses synthetic fixture records.
---

# BigBoard exploration

Use the bundled BigBoard MCP tools for this workflow. This milestone contains
synthetic data only; describe findings as fixture activity.

## Open or resume

Call `open_board` with no arguments to create an independent board. To reopen
an existing board, pass its exact `board_id`. Retain the returned board ID,
revision, snapshot ID, resolved scope, and coverage for follow-up.

## Discuss a selection

The app supplies selection context containing its board ID, revision, snapshot
ID, query, scope, bucket start, and record IDs. Use that board and selection
when the user refers to "this", "the spike", or "the selection". If the context
does not identify a board, ask the user to select it or supply its board ID.

Call `read_board` to verify the latest state, then `query_evidence` with its
`board_id`, `snapshot_id`, and the selected `bucket_start` when present. If
the snapshot changed, explain the change and refresh the selection before
attributing records. Ground explanations in returned fixture record IDs and
scope. Distinguish unavailable prior-period evidence from zero activity.

## Change the view

Call `read_board`, then `update_board` with that exact board ID and its
`expected_revision`. Supported controls are `days` (7 or 14), `repository`
(`all`, `sample/api`, or `sample/web`), and `compare` (true or false).
On a revision conflict, read the board again and reconcile the user's request
with its current state. A filter change applies to that board only.

The open app reads the same saved board to display agent changes. Subsequent
reads and updates do not need to open a second panel. Report the resulting
scope and revision; do not claim the host rendered a change without observing
it. Call `open_board` without `board_id` only when the user wants another board.
