import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerAppResource, registerAppTool, RESOURCE_MIME_TYPE } from "@modelcontextprotocol/ext-apps/server";
import { z } from "zod";
import { BoardError, BoardStore } from "./core";
import { APP_VERSION, BOARD_RESOURCE_URI, type BoardSnapshot, type EvidenceResult, type PeopleResult } from "./shared";

const boardId = z.string().uuid();
const annotations = { readOnlyHint: true, destructiveHint: false, openWorldHint: false };
function boardText(board: BoardSnapshot): string {
  return `BigBoard ${board.boardId}, revision ${board.revision}, snapshot ${board.snapshotId}. ${board.query.repository}, ` +
    `[${board.scope.since}, ${board.scope.until}) UTC: ${board.totals.commits} recorded non-merge commits, ${board.totals.mergedPRs} observed merged PRs, ${board.totals.openPRs} observed open PRs. ` +
    `${board.totals.unknownLineCommits} commits have unknown line counts. ${board.initialView ? `Opening ${board.initialView}. ` : ""}${board.coverage.note}` +
    (board.refresh ? ` Refresh ${board.refresh.status}: ${board.refresh.message} Git repositories checked ${board.refresh.git.completedRepositories}/${board.refresh.git.totalRepositories}; GitHub repositories checked ${board.refresh.github.completedRepositories}/${board.refresh.github.totalRepositories}; GitHub requests ${board.refresh.github.requests}.` : "");
}
function evidenceText(evidence: EvidenceResult): string {
  return `Git/GitHub evidence for board ${evidence.boardId}, snapshot ${evidence.snapshotId}. ` +
    `Evidence kind: ${evidence.kind ?? "all"}${evidence.pullRequestState ? `; PR state: ${evidence.pullRequestState}` : ""}${evidence.selectedPullRequestId ? `; selected PR: ${evidence.selectedPullRequestId}` : ""}${evidence.selectedPersonId ? `; selected person: ${evidence.selectedPersonId}` : ""}${evidence.pullRequestSort ? `; PR sort: ${evidence.pullRequestSort} ${evidence.sortDirection}` : ""}. ` +
    `${evidence.bucketStart ? `Selected 24-hour interval ${evidence.bucketStart}.` : "Whole board period."} Current open PRs are independent of this date interval. ` +
    `${evidence.records.length}/${evidence.totalRecords} commit records and ${evidence.pullRequests.length}/${evidence.totalPullRequests} PR records on this page. ` +
    `${evidence.coverage.note}\n${JSON.stringify({ records: evidence.records, pullRequests: evidence.pullRequests, nextCursor: evidence.nextCursor })}`;
}
function peopleText(people: PeopleResult): string {
  return `People for board ${people.boardId}, snapshot ${people.snapshotId}. ${people.source === "git" ? "Git identities use mailmapped author email; names alone are not merged." : "GitHub identities use PR author login, separately from Git identities. Open PRs are current as of collection and ignore the date interval."} ` +
    `${people.people.length}/${people.totalPeople} people on this page. ${people.coverage.note}\n${JSON.stringify({ people: people.people, nextCursor: people.nextCursor })}`;
}
async function result(operation: () => Promise<unknown>): Promise<CallToolResult> {
  try {
    const value = await operation() as Record<string, unknown>;
    const text = "records" in value ? evidenceText(value as unknown as EvidenceResult) : "people" in value ? peopleText(value as unknown as PeopleResult) : "boardId" in value ? boardText(value as unknown as BoardSnapshot) : JSON.stringify(value);
    return { content: [{ type: "text", text }], structuredContent: value };
  } catch (error) {
    const code = error instanceof BoardError ? error.code : "source_error";
    const message = error instanceof BoardError ? error.message : "BigBoard could not complete the operation. Check repository paths, Git, and GitHub CLI access, then retry.";
    return { isError: true, content: [{ type: "text", text: message }], structuredContent: { error: { code, message } } };
  }
}
export function createBigboardServer(html: string, store: BoardStore): McpServer {
  const server = new McpServer({ name: "bigboard-mcp-app", version: APP_VERSION });
  const resourceMeta = { ui: { csp: { connectDomains: [], resourceDomains: [] }, prefersBorder: true } };
  registerAppResource(server, "BigBoard", BOARD_RESOURCE_URI,
    { description: "Git Activity, Pull requests, and People views", _meta: resourceMeta },
    async () => ({ contents: [{ uri: BOARD_RESOURCE_URI, mimeType: RESOURCE_MIME_TYPE, text: html, _meta: resourceMeta }] }));
  registerAppTool(server, "open_board", {
    title: "Open BigBoard", description: "Open a Git/GitHub board in the activity (default), pull_requests, or people view. These are views of the same board. Omit board_id for an independent board or provide an existing ID to reopen its saved filters in a chosen view. Opening and viewing never collect data. If unconfigured, use configure_sources then refresh_sources.",
    inputSchema: { board_id: boardId.optional(), view: z.enum(["activity", "pull_requests", "people"]).optional() }, annotations: { ...annotations, readOnlyHint: false }, _meta: { ui: { resourceUri: BOARD_RESOURCE_URI, visibility: ["model", "app"] } },
  }, ({ board_id,view }) => result(async () => ({ ...await store.open(board_id), initialView: view === "pull_requests" ? "pullRequests" : view ?? "activity" })));
  server.registerTool("read_board", {
    title: "Read BigBoard", description: "Read saved board state, collected evidence coverage, and current source refresh progress without opening a panel or refreshing sources. Progress updates do not change the evidence snapshot. Use before updating viewing filters.",
    inputSchema: { board_id: boardId }, annotations, _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id }) => result(() => store.read(board_id)));
  server.registerTool("update_board", {
    title: "Update BigBoard view", description: "Change this board's viewing filters and comparison using its latest expected_revision. Repository must be all or an ID in list_sources. Viewing changes never change collected repositories or perform network requests.",
    inputSchema: { board_id: boardId, expected_revision: z.number().int().positive(), days: z.union([z.literal(7),z.literal(14)]).optional(), repository: z.string().min(1).optional(), compare: z.boolean().optional() },
    annotations: { ...annotations, readOnlyHint: false }, _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id,expected_revision,days,repository,compare }) => result(() => store.update(board_id,expected_revision,{ ...(days === undefined ? {} : { days }), ...(repository === undefined ? {} : { repository }), ...(compare === undefined ? {} : { compare }) })));
  server.registerTool("query_evidence", {
    title: "Inspect BigBoard evidence", description: "Page evidence for an exact snapshot and optional selected 24-hour bucket. Use kind:activity for commits or kind:pull_requests with pull_request_state:open/merged for the PR view. An exact pull_request_id selects one available PR; person_id selects a returned Git contributor or GitHub author. PR sorting covers the full filtered inventory before pagination; defaults to updated descending. Commits use author dates; merges use merged_at and default branch; open PRs ignore the historical interval. Reuse nextCursor with the same snapshot, selection, and filters including sort.",
    inputSchema: { board_id: boardId, snapshot_id: z.string(), bucket_start: z.string().optional(), cursor: z.string().optional(), kind: z.enum(["all", "activity", "pull_requests"]).optional(), pull_request_state: z.enum(["open", "merged"]).optional(), pull_request_id: z.string().min(1).max(256).optional(), person_id: z.string().min(1).max(8192).optional(), pull_request_sort: z.enum(["updated", "created", "number", "title", "author", "repository"]).optional(), sort_direction: z.enum(["asc", "desc"]).optional() }, annotations, _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id,snapshot_id,bucket_start,cursor,kind,pull_request_state,pull_request_id,person_id,pull_request_sort,sort_direction }) => result(() => store.evidence(board_id,snapshot_id,bucket_start,cursor,{kind,pullRequestState:pull_request_state,pullRequestId:pull_request_id,personId:person_id,pullRequestSort:pull_request_sort,sortDirection:sort_direction})));
  server.registerTool("query_people", {
    title: "Inspect BigBoard people", description: "Page contributor summaries for an exact board snapshot. source:git groups commits and known lines in the board period by mailmapped author email. source:github groups current open PRs and period merges by PR author login. Git and GitHub identities stay separate; names do not establish a link. Pass returned person IDs to query_evidence with person_id. Reuse nextCursor only for this snapshot and source.",
    inputSchema: { board_id: boardId, snapshot_id: z.string(), source: z.enum(["git", "github"]), cursor: z.string().optional() }, annotations, _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id,snapshot_id,source,cursor }) => result(() => store.people(board_id,snapshot_id,source,cursor)));
  server.registerTool("list_sources", {
    title: "List BigBoard sources", description: "Read saved collection selection and its revision. Paths refer to existing local clones, distinct from temporary viewing filters.", inputSchema: {}, annotations,
  }, () => result(async () => store.sources()));
  server.registerTool("configure_sources", {
    title: "Configure BigBoard sources", description: "Save local repository paths, or set import_native:true to copy native BigBoard selected repositories read-only. Provide exactly one of paths or import_native:true. Pass expected_revision from list_sources to avoid overwriting changed settings. Replaces selection and clears old collected data; does not fetch or modify clones.",
    inputSchema: { paths: z.array(z.string().min(1)).max(100).optional(), import_native: z.boolean().optional(), expected_revision: z.number().int().nonnegative().optional() }, annotations: { ...annotations, readOnlyHint: false },
  }, ({ paths,import_native,expected_revision }) => result(() => store.configure(paths,import_native === true,expected_revision)));
  server.registerTool("refresh_sources", {
    title: "Refresh BigBoard sources", description: "Explicitly collect 28 days of author-date Git history from saved local remote-tracking default refs plus open and merged GitHub PRs with bounded concurrency. Reads existing clones without fetching/modifying them, authenticates using gh auth token, and saves coverage/collection time. May take up to 150 seconds. Read current progress through read_board; callers requesting MCP progress receive notifications. Polling and filter changes do not refresh.",
    inputSchema: {}, annotations: { ...annotations, readOnlyHint: false, openWorldHint: true }, _meta: { ui: { visibility: ["model", "app"] } },
  }, (_,extra) => {
    let lastProgress = -1;
    return result(() => store.refresh(extra.signal, progress => {
      const progressToken = extra._meta?.progressToken;
      const completed = progress.git.completedRepositories + progress.github.completedRepositories;
      if (progressToken === undefined || completed <= lastProgress) return;
      lastProgress = completed;
      void extra.sendNotification({ method: "notifications/progress", params: { progressToken, progress: completed, total: progress.git.totalRepositories + progress.github.totalRepositories, message: `${progress.message} Git repositories checked ${progress.git.completedRepositories}/${progress.git.totalRepositories}; GitHub repositories checked ${progress.github.completedRepositories}/${progress.github.totalRepositories}; GitHub requests ${progress.github.requests}.` } }).catch(() => {});
    }));
  });
  return server;
}
