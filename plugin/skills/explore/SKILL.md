---
name: explore
description: Open and discuss BigBoard Activity, People, and Pull requests views, inspect supporting evidence, discuss or review selected PRs, or explore repository filters and comparisons.
---

# BigBoard exploration

Invoke this workflow as `bigboard:explore`, or ask naturally to open or discuss
BigBoard.

Use the bundled BigBoard tools. The collection selection is shared; each board
keeps independent viewing filters. Git activity, PR merges, and current open
PRs describe different events. Merged does not establish deployed.

## Sources and freshness

Use `list_sources` to inspect the saved collection. For configuration requests,
use [`bigboard:configure`](../configure/SKILL.md).
It handles shared source selection and per-board viewing settings, including
natural requests to change BigBoard's configuration.

The rendered Pull requests view calls `refresh_sources` automatically when it
becomes active, including direct opening. It reuses an in-flight refresh. Do
not start a second refresh just to open that view. Activity and People read
saved data; call `refresh_sources` when fresh data is requested there or no
collection exists. It can take up to 150 seconds; allow sufficient tool timeout.
It reads existing remote-tracking default branches without fetching or modifying the
clones and collects GitHub open and merged PRs with GitHub CLI authentication.
Review its coverage result before describing findings as complete. Do not
repair authentication or modify clones as a recovery step.

GitHub repositories collect with bounded concurrency. Supply a progress token
when the host supports MCP progress notifications. An open app also shows the
saved `refresh` status returned by `read_board`, including separate Git and
GitHub repository counts. Counts describe work processed, including failures;
inspect coverage before claiming collection is complete. Polling progress does
not start another refresh. A stale running status expires after three minutes.

After server restart, a refresh must revalidate GitHub access before using
cached PR evidence. Activating the PR view does this automatically. Displayed
Git activity still comes from the saved local scan. Collection time and scanned
revisions are part of the answer.

## Open, inspect, and discuss

Call `open_board` without `board_id` for a new independent board, or with an
exact existing ID to reopen it. Set `view` to `activity`, `people`, or
`pull_requests` to open directly into that view. These are views of the same
board and share its repository and date filters. Use app selection context to
identify the board, snapshot, active view, and selected interval, person, or PRs. If the intended board is
ambiguous, ask the user to select it or supply its ID.

Call `read_board`, then `query_evidence` with the exact board and snapshot IDs.
For Activity use `kind:activity` and its selected `bucket_start` when present.
For Pull requests use `kind:pull_requests`, its `pull_request_state` (open or
merged), and selected `pull_request_id` when present. Do not carry a hidden
Activity interval into a PR query. For People, use `query_people` with its
`source` (`git` or `github`); inspect a selected person's evidence with
`query_evidence` and its exact `person_id`, without a hidden Activity interval.
Git contributors and GitHub authors are separate identities; do not link them
by similar names. Git statistics and merged PRs follow the board period, while
open PRs are current as of collection time. If no view context exists, use the user's
question to choose the evidence kind. If the selection's snapshot differs
from the current board, disclose that change and refresh the selection before
attributing records. Follow `nextCursor` with the same snapshot, bucket, kind,
and filters to inspect more evidence. PR sorting uses `pull_request_sort`
(`updated`, `created`, `number`, `title`, `author`, or `repository`) and
`sort_direction` (`asc` or `desc`). Sorting applies before paging. Shortened
record IDs or file lists are not exhaustive.

Explain commits using their author dates and exact links. Merged PRs follow
the selected interval and default branch; open PRs remain current context as
of collection time, including older drafts. Keep incomplete coverage and
unknown line counts visible; zero under incomplete coverage is not absence.

## Selected PRs and code reviews

Checkbox selections are in `selectedPullRequests` in the app context. Each
record identifies its repository, number, URL, and collected head SHA. A
separate `pullRequestId` is the row whose details are open; it does not replace
the checkbox selection. Use the explicit batch when the user says “these PRs.”
Do not expand it to every loaded PR or the whole board.

App navigation updates background model context; it does not send a chat turn.
Explicit app actions send brief requests such as “Explain the selected Activity
interval” or “Review the 3 pull requests selected in BigBoard.” Resolve their
exact scope from the latest app context. Do not ask the user to paste the JSON
or repeat it into the chat. Retrieve supporting evidence through the tools.

The PR table shows author, draft/open/merged state, GitHub's review decision,
pending review, and account relationships. Optional `review` metadata travels
with each selected PR. A missing review object or `decision:unknown` means the
review state is unavailable; `decision:none` means GitHub returned no decision.
Requested users, teams, and assignees have explicit completeness flags. Do not
turn incomplete empty lists into claims that nobody needs to act. Account
badges distinguish author, assignee, and direct reviewer; team membership is
not inferred. Review status is as of collection time and may need a refresh.

Selecting checkboxes updates context without requesting reviews. **Share
selection** sends the selection for discussion; **Request code reviews** sends
the user's review request for that exact set. When reviews are requested,
follow the host agent's applicable review workflow and repository instructions.
BigBoard supplies context; it does not execute reviews. Check current PR state
and head before reviewing, since the collected snapshot may be older. If the
selection is missing or its snapshot changed, ask for a fresh selection rather
than guessing the batch.

## Change the board scope

Call `read_board`, then `update_board` with its `board_id` and current
`expected_revision`. Controls are `days` (7 or 14), `repository` (`all` or an ID
returned by `list_sources`), and `compare`. These do not change collected
sources or start network requests. Reconcile revision conflicts with the
latest state before retrying.

Activity, People, and Pull requests are display tabs in the app. `update_board` changes
the shared board query, not the active tab; do not claim to switch tabs with
that tool. The existing panel reads the same saved board. Subsequent reads
and updates need no new panel. Report the resulting scope and revision without claiming a
rendered change unless observed. Open a new board only when requested.
