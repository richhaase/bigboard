# BigBoard plugin

Explore real Git history and GitHub pull requests in focused views. Activity
opens with a chart and a small summary. Select an interval to reveal its
supporting commits. People groups Git contributors and GitHub authors in
separate lists. Pull requests has Open and Merged tables, sorting, and checkbox
selection. Discuss evidence or request reviews for selected PRs with the agent.

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
codex plugin add bigboard@bigboard-mcp
```

For later pushed versions:

```sh
codex plugin marketplace upgrade bigboard-mcp
codex plugin add bigboard@bigboard-mcp
```

If the older `bigboard-mcp-app` plugin is installed, run the update commands
above first, then verify that `bigboard@bigboard-mcp` is installed and enabled:

```sh
codex plugin list --marketplace bigboard-mcp
```

After verifying the new installation, remove the old plugin to avoid duplicate
tools and skills:

```sh
codex plugin remove bigboard-mcp-app@bigboard-mcp
```

Saved source settings, evidence, and boards remain in the existing state
directory outside the plugin cache, so this rename does not move or reset them.
Start a new local chat after upgrading and select **BigBoard**, so its server
and skills use the installed version. The skill IDs are `bigboard:explore` and
`bigboard:configure`, with no custom title override.

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

Use the `bigboard:configure` skill, or ask naturally: “Change BigBoard's
configuration,” “Add this local clone to BigBoard,” or “Show two weeks on this
board.” The skill inspects current settings, clarifies unspecified choices,
and updates shared sources or this board's viewing filters through the
existing tools. Explicit choices can be applied directly. Source details and
the empty state point to this workflow; configuration stays in the agent.

Activating **Pull requests**, including opening directly into that view,
automatically refreshes the saved sources. It uses the same collection and
progress display as **Refresh sources** and joins an already-running refresh
instead of starting a duplicate. Leaving and returning activates another
refresh. Ordinary sorting, filtering, selection, and polling do not collect.
Activity and People read the completed collection until you use **Refresh
sources** or ask the agent to call `refresh_sources`. A refresh collects 28
days of local Git history and GitHub PR evidence.

GitHub collection processes up to three repositories concurrently. Each
repository's Open and Merged inventories and their pages stay in order; the
existing global request, page, response-size, and time limits still apply.
Git history continues to use up to four workers alongside GitHub collection.

The app shows separate Git and GitHub repository progress during collection,
including refreshes started by the agent or another panel. Counts mean
repositories processed and can include coverage failures. Completion reports
coverage gaps separately from progress; it does not imply every source was
available. The last outcome remains visible after completion or cancellation.
Progress is available through `read_board` without changing the evidence
snapshot for every progress update. Callers supplying an MCP progress token
also receive progress notifications from `refresh_sources`.

Progress is saved separately from collected evidence. A newer refresh or a
source-configuration change prevents an older run from overwriting it. If a
process disappears without finishing, its running status expires after three
minutes and the app allows another explicit refresh. Collection still
publishes one completed snapshot; a new server session still requires a
refresh to verify GitHub access before displaying cached PR evidence.

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
  Activity selections narrow commit evidence to a chart interval. In Pull
  requests, Open is independent of the Activity interval, while Merged follows
  the board's date range. Merged does not establish deployed.
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
  stays hidden until a refresh verifies access for the new session. Activating
  Pull requests starts that refresh automatically.
  Failed or denied refreshes do not keep old PRs under a different account.

## Views and supporting evidence

**Activity** is the default overview. It shows repository/date controls, the
commit chart, and a compact summary. Commit evidence is requested and shown
after selecting an interval. Close its detail pane to return to the overview.

**Pull requests** has Open and Merged tabs with their own paged tables. Open
includes older drafts as of collection time; Merged uses the selected date
range. Sort by update time, creation time, number, title, author, or repository,
in either direction. Sorting applies to the full collected list before paging.
Select a row to inspect that exact PR and explain it with the agent.
Activity's selected interval does not filter this view.

The table includes the PR and repository, author, open/merged and draft state,
GitHub review decision, whether review is pending, your relationship to the PR,
and update time. Sortable column headers use the same full-inventory sorting
as the sort controls. Select a title to inspect details below the table;
checkboxes keep their independent batch selection.

Review decisions are GitHub's Approved, Changes requested, or Review required
states. A returned null decision is **No decision**; missing or unavailable
metadata is **Unknown**. Pending user or team requests count as needing review
for a ready, open PR. Drafts show Draft, merged PRs have no pending-review
indicator, and changes requested without a new request indicate author action.
No decision with a complete empty request list means No request. These states
describe the collection time. See the [GitHub review API reference](https://docs.github.com/en/graphql/reference/pulls#pullrequestreviewdecision).

**Yours** distinguishes PRs you authored, PRs assigned to you, and direct review
requests for the verified GitHub account. Team requests are shown separately;
BigBoard does not infer your team membership. Missing or truncated reviewer and
assignee lists remain marked incomplete. Existing collections lack the new
metadata until the next refresh.

Refresh adds batched read-only GraphQL queries for review decisions, requests,
and assignees, with up to 50 PRs per query and three concurrent requests. These
share the existing request, body-size, and time budgets. Changed PR heads,
unavailable fields, or exhausted limits remain unknown or incomplete. Review
coverage is separate from complete Open/Merged inventory coverage.

Use checkboxes to select several PRs. **Select loaded** adds the currently
loaded rows; it does not select unseen pages. Select up to 100 PRs per batch;
larger selections are rejected without changing the existing set. The selected set survives sorting
and paging within the same scope. It clears when the snapshot, repository,
state tab, or view changes. **Share selection** sends those exact PRs to the
conversation. **Request code reviews** sends a review request for the same set;
the agent follows its normal review workflow. Checkboxes also update agent
context directly, so you can select PRs and type “review these” in the chat.
Opening a row's details and selecting a checkbox are independent actions.

On hosts advertising structured context, ordinary navigation and selection
update background model context through
`ui/update-model-context` using `structuredContent`. They do not post a user
message or attach raw JSON to the composer. On hosts supporting only text
context, the serialized fallback is marked for the assistant audience, following
OpenAI's background-context extension. Rendering on other hosts depends on
their support for that annotation. Explain,
Share selection, and Request code reviews send short human-readable messages;
exact board, snapshot, and PR records stay in background context. If context
sharing is unsupported or fails, those actions stay disabled. This follows the
[MCP Apps background-context pattern](https://github.com/openai/mcp-extensions/blob/main/docs/patterns.md#background-context).

**People** has Git contributors and GitHub authors tabs. Git contributors are
grouped by the scanned revision's mailmapped author email; GitHub authors are
grouped by login. These sources are not linked by display name. Commit and
merged PR counts use the board's repository and date range; open PR counts are
as of collection time. Select a person to inspect supporting evidence and
discuss that exact identity with the agent. Counts retain the source coverage
limits and describe observed activity within the collected scope.

`open_board` accepts `view:activity`, `view:people`, or `view:pull_requests`.
Pass an existing `board_id` to open another view of that board's saved scope,
or omit it for a new independent board. The view argument chooses the opening
tab; subsequent reads do not reset the user's active tab.

The compact source-status control exposes collection time, scanned refs,
coverage gaps, and board/snapshot details. Pull requests refreshes on activation;
the Refresh sources action is also available in every view. The
agent receives the active view and selection, including selected PR records
and a separate inspected PR ID. Changing views does not change collected sources.

Evidence tools accept `kind:activity` or `kind:pull_requests`. PR queries can
include `pull_request_state`, `pull_request_id`, `pull_request_sort`, and
`sort_direction`. People summaries use `query_people` with source `git` or
`github`; `query_evidence` accepts an exact `person_id` for supporting records.
Page cursors are bound to their filters, sorting, snapshot, and any selected
interval. Shortened record-ID and file lists remain explicitly marked.

## Interaction acceptance test

1. Open Activity. Confirm the chart and summary are visible without commit or
   PR lists. Inspect the compact status control for source coverage.
2. Select an interval. Its supporting commits should appear in a detail pane;
   **Explain selection** should send that interval's exact context to the
   agent. Close the pane and confirm the overview becomes quiet again.
3. Switch to Pull requests and watch the automatic source refresh. Open should
   include older drafts without the Activity interval filter. Switch to Merged
   and check its date range. Returning from another view starts a new refresh;
   sorting or selecting rows must not start another collection.
4. Select a PR row. Its details and explanation should identify that exact PR
   and snapshot. Check several PRs, sort, and load more. The selected count and
   agent context must preserve that exact set. Share the selection or request
   reviews; verify the chat receives only the checked PRs. Checkbox changes
   alone must not start reviews.
5. Open directly into People. Switch identity sources, select a person, and
   inspect evidence. Confirm Git contributors and GitHub logins stay separate.
6. Page through a list, switch views, and change the repository. Results from
   a prior view or filter must not appear in the new view.
7. Ask the agent to change the repository or Activity comparison. Confirm the
   same board updates while source settings remain unchanged. Refresh sources
   explicitly and verify old snapshot queries fail.
8. Reopen an existing board and create another independent board. Their saved
   queries must remain separate. After server restart, refresh once to verify
   GitHub access before using PR evidence.
9. Refresh sources and watch separate Git and GitHub progress before the
   request completes. Check a refresh started by the agent too. Coverage gaps,
   cancellation, and interruption must show an honest final outcome. Progress
   updates alone must not reset evidence paging or checkbox selections.
10. Check source details and an empty board for guidance to `bigboard:configure`.
    Ask the agent to change source selection or board filters, and verify the
    skill distinguishes shared collection settings from per-board viewing.

Automated checks cover actual Git repositories in temporary test folders,
mocked GitHub responses, the packaged stdio server, saved state, and the
rendered app through the official MCP Apps bridge. Desktop-host acceptance
is recorded separately from those tests.

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
unset. This legacy directory name is retained across the plugin rename to
preserve existing data. `BIGBOARD_STATE_DIR` overrides it for isolated tests.
It stores source settings, collected evidence, and board queries, separately
from native data.
The native settings import honors `BIGBOARD_CONFIG_ROOT` or the usual
`$XDG_CONFIG_HOME/bigboard/config.json`. Tokens are never persisted.

Version 0.1 demo boards migrate to the real-data model, retaining their board
IDs and date/comparison controls while clearing synthetic repository filters.
No fixture activity is returned by the production server.
