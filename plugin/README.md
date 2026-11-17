# BigBoard MCP app

Explore real Git history and GitHub pull requests in a shared activity board.
Select a day, discuss the supporting commits and PRs, and ask the agent to
change the same board's repository, range, or comparison.

## Integration branch

`codex/bigboard-mcp-app` is the virtual main for this rebuild. The managed
worktree is named `bigboard-mcp-app`. Follow-up work starts from this branch;
each installable update gets a version bump and rebuilt committed bundles.
The native app remains on `main` during this work.

## Install or update

Requires Node.js 24 or later, Git, and GitHub CLI on the desktop host's PATH.
Sign in to GitHub CLI with your usual account. The self-contained package
needs no `npm install` or build step when installed from the marketplace.

```sh
codex plugin marketplace add richhaase/bigboard --ref codex/bigboard-mcp-app
codex plugin add bigboard-mcp-app@bigboard-mcp
```

For later pushed versions:

```sh
codex plugin marketplace upgrade bigboard-mcp
codex plugin add bigboard-mcp-app@bigboard-mcp
```

Start a new local chat after upgrading and select **BigBoard MCP**, so its
server and skill use the installed version. Check that version with
`codex plugin list --marketplace bigboard-mcp`.

## Configure and collect

Ask the agent:

> Import my native BigBoard repository selection, refresh sources, and open an activity board.

`configure_sources` with `import_native:true` copies the selected repositories
from native BigBoard settings. It discovers their existing clones under the
native source folders and honors repository exclusions. Native settings and
clones are not modified. Missing selected clones cause an actionable import
error rather than quietly shrinking the selection.

Alternatively, provide absolute existing clone paths through
`configure_sources` with `paths`. `list_sources` returns the saved selection
and its revision. Pass that revision when changing sources. Source settings
are shared by boards; viewing filters remain independent for each board.

Use **Refresh sources**, or ask the agent to call `refresh_sources`, to collect
28 days of history and GitHub PR evidence. Opening, polling, and filtering a
board read the last completed collection without starting collection.

## What the data means

- Git activity uses non-merge commits reachable from each clone's
  remote-tracking default branch, with author dates in `[since, until)`.
  Local feature branches and uncommitted files are excluded. Collection never
  fetches or modifies clones. Update remote-tracking refs through your normal
  Git workflow when needed; the board shows the exact scanned revision.
  A commit present in multiple selected repositories counts once per repository.
- Date windows are rolling 7 or 14 days ending at collection time. Chart bars
  are consecutive 24-hour UTC intervals. A comparison uses the preceding
  equal-length interval. Local history traversal and line-detail limits are
  disclosed through repository coverage.
- Merged PRs use GitHub's merge date and target default branch. Current open
  PRs, including drafts, remain visible regardless of the historical window.
  A selected bucket narrows commits and merged PRs, while open PRs remain
  current context as of collection time. Merged does not establish deployed.
- Git author email identities use the scanned revision's mailmap. Different
  emails are not merged by display name. Line totals include tracked file
  changes; binary files and unavailable details have unknown line counts,
  and displayed totals are lower bounds when any counts are unknown.
- GitHub uses a token obtained from `gh auth token` only in memory. Request,
  page, response-size, and time limits bound collection. Failure, missing
  permissions, shallow or partial clones, and truncated results remain
  visible. Incomplete count totals carry a `+` marker and are lower bounds.
  A zero with incomplete coverage does not prove no activity.
- After a server restart, cached Git history is available; GitHub PR evidence
  stays hidden until an explicit refresh verifies access for the new session.
  Failed or denied refreshes do not keep old PRs under a different account.

**Board details** exposes per-repository coverage, refs, collection time, and
snapshot identity. Commit and PR links open their exact GitHub records.
Evidence is paged; the agent uses `nextCursor` with the same snapshot and
selected interval. Large file lists and selection ID lists are explicitly
marked when shortened.

## Interaction acceptance test

1. Configure sources, refresh, and open a board. Confirm selected repository
   IDs, scanned revisions, collection time, and any coverage gaps.
2. Select a daily bar and ask "What contributed to this change?" The agent
   should use its board ID, snapshot ID, and bucket start to query supporting
   records. **Explain selection** sends the same context into the conversation.
3. Ask for one configured repository and a previous-period comparison. The
   same panel should update within its next visible refresh, usually two
   seconds; collection settings must stay unchanged.
4. Check that older open PRs remain visible when selecting a recent bucket,
   and merged PRs follow the selected interval. Use **Load more evidence**
   when more pages are available.
5. Refresh sources and confirm the snapshot changes. Queries with an old
   snapshot must fail instead of mixing records from different collections.
6. Close and reopen the board using its existing `board_id`. Create a second
   board and confirm their viewing filters remain independent. After a server
   restart, refresh once to revalidate GitHub access.

Automated checks cover actual Git repositories created in temporary test
folders, mocked GitHub responses, the packaged stdio server, persistent state,
and the rendered web app through the official MCP Apps bridge. Desktop-host
acceptance is recorded separately from those tests.

## Build and verify

```sh
cd plugin
fnm install
fnm exec npm ci
fnm exec npm run check
```

`bundle/server.cjs`, `bundle/board.html`, and bundled third-party licenses are
committed artifacts. CI rebuilds them and rejects drift. The SDK pair remains
pinned to MCP SDK 1.32.2 and MCP Apps 1.7.5.

The server launches with `node bundle/server.cjs`. Its SQLite state is under
`$XDG_STATE_HOME/bigboard-mcp-app`, or `~/.local/state/bigboard-mcp-app` when
unset. `BIGBOARD_STATE_DIR` overrides it for isolated tests. It stores source
settings, collected evidence, and board queries, separately from native data.
The native settings import honors `BIGBOARD_CONFIG_ROOT` or the usual
`$XDG_CONFIG_HOME/bigboard/config.json`. Tokens are never persisted.

Version 0.1 demo boards migrate to the real-data model, retaining their board
IDs and date/comparison controls while clearing synthetic repository filters.
No fixture activity is returned by the production server.
