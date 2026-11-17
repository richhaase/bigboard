# BigBoard MCP app

Milestone 1: a shared activity board using synthetic records. Select a day,
discuss its exact records with an agent, and ask the agent to change the same
board's repository, range, or comparison. No live GitHub account is needed.

## Integration branch

`codex/bigboard-mcp-app` is the virtual main for this rebuild. The managed
worktree is named `bigboard-mcp-app`. Follow-up work starts from this branch;
each installable update gets a version bump and rebuilt committed bundles.
The native app remains on `main` while this interaction is tested.

## Install from the branch

Requires Node.js 24 or later on the desktop host's PATH and a local Codex chat
in a host that renders MCP Apps. The package includes its server and UI, so
installation needs no `npm install` or build step.

```sh
codex plugin marketplace add richhaase/bigboard --ref codex/bigboard-mcp-app
codex plugin add bigboard-mcp-app@bigboard-mcp
```

Start a new local chat, select **BigBoard MCP**, and ask:

> Open a BigBoard activity board using the demo data.

The marketplace name is `bigboard-mcp`; the installable plugin is
`bigboard-mcp-app`. The catalog uses `./plugin` from the fetched branch.

To pull a later pushed version:

```sh
codex plugin marketplace upgrade bigboard-mcp
codex plugin add bigboard-mcp-app@bigboard-mcp
```

Open a new chat after upgrading so its tools and server use the refreshed
package. Check the version with `codex plugin list --marketplace bigboard-mcp`.

## Interaction acceptance test

1. Open a board. It must visibly identify its data as a fixture and show a
   board ID and revision. Its default view covers seven days ending November
   16, 2026, at 00:00 UTC. These dates are fixed demonstration data.
2. Select a daily bar, then type "What contributed to this spike?" The agent
   should receive the board ID, snapshot ID, selected interval, and record IDs,
   and use `query_evidence` to explain those records.
3. Try **Explain selection**. It should send the same scope into the
   conversation and request an explanation. Host capability errors stay visible.
4. Ask "Show only sample/api on this board and compare with the previous
   week." The agent should read the board and update that same board ID with
   the current revision. The open panel should update within its next visible
   refresh, usually two seconds. Confirm the board ID stays the same.
5. Close and reopen the board by asking the agent to call `open_board` with
   its existing `board_id`. The filter, comparison, and revision should remain.
6. Ask for a second independent board. Changing it should leave the first
   board's query intact. Select a bar before referring to "this" so the agent
   receives the intended board's context.
7. Choose 14 days and enable comparison. Prior-period evidence is unavailable
   for that range; the board should say so rather than inventing zero activity.

Automated tests cover the MCP stdio server, saved state and conflicts, and a
rendered app with a simulated MCP Apps host. They do not establish how a
particular desktop host renders or routes the conversation. Record that result
from the checklist above when testing the installed package.

## Build and verify

```sh
cd plugin
fnm install
fnm exec npm ci
fnm exec npm run check
```

`bundle/server.cjs`, `bundle/board.html`, and bundled third-party licenses are
committed installable artifacts. CI rebuilds them and rejects drift. The SDK
pair is pinned to MCP SDK 1.32.2 and MCP Apps 1.7.5, which share the SDK 1 API.

The server launches over stdio with `node bundle/server.cjs`. It owns board
queries and revisions; the UI owns transient selection and rendering. The
standard MCP Apps bridge carries selection context and follow-up messages.
The app reads saved board state while visible so agent updates do not depend
on broadcasts from unrelated tool calls.

Board state uses the plugin's own directory under
`$XDG_STATE_HOME/bigboard-mcp-app`, or `~/.local/state/bigboard-mcp-app` when
unset. `BIGBOARD_STATE_DIR` overrides it for isolated tests. State is local
and contains only fixture board queries and revisions. Native settings and
caches are separate.

The tool contract is `open_board`, `read_board`, `update_board`, and
`query_evidence`. Updates require `expected_revision`; evidence queries require
the exact `snapshot_id`. Each response includes source coverage and the
resolved half-open date interval `[since, until)`.
