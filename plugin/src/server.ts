import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerAppResource, registerAppTool, RESOURCE_MIME_TYPE } from "@modelcontextprotocol/ext-apps/server";
import { z } from "zod";
import { BoardError, BoardStore } from "./core";
import { APP_VERSION, BOARD_RESOURCE_URI, type BoardSnapshot, type EvidenceResult } from "./shared";

const boardId = z.string().uuid();
const annotations = { readOnlyHint: true, destructiveHint: false, openWorldHint: false };
function boardText(board: BoardSnapshot): string {
  return `BigBoard ${board.boardId}, revision ${board.revision}, snapshot ${board.snapshotId}. ${board.query.repository}, ` +
    `[${board.scope.since}, ${board.scope.until}) UTC: ${board.totals.commits} recorded non-merge commits, ${board.totals.mergedPRs} observed merged PRs, ${board.totals.openPRs} observed open PRs. ` +
    `${board.totals.unknownLineCommits} commits have unknown line counts. ${board.coverage.note}`;
}
function evidenceText(evidence: EvidenceResult): string {
  return `Git/GitHub evidence for board ${evidence.boardId}, snapshot ${evidence.snapshotId}. ` +
    `Evidence kind: ${evidence.kind ?? "all"}${evidence.pullRequestState ? `; PR state: ${evidence.pullRequestState}` : ""}${evidence.selectedPullRequestId ? `; selected PR: ${evidence.selectedPullRequestId}` : ""}. ` +
    `${evidence.bucketStart ? `Selected 24-hour interval ${evidence.bucketStart}.` : "Whole board period."} Current open PRs are independent of this date interval. ` +
    `${evidence.records.length}/${evidence.totalRecords} commit records and ${evidence.pullRequests.length}/${evidence.totalPullRequests} PR records on this page. ` +
    `${evidence.coverage.note}\n${JSON.stringify({ records: evidence.records, pullRequests: evidence.pullRequests, nextCursor: evidence.nextCursor })}`;
}
async function result(operation: () => Promise<unknown>): Promise<CallToolResult> {
  try {
    const value = await operation() as Record<string, unknown>;
    const text = "records" in value ? evidenceText(value as unknown as EvidenceResult) : "boardId" in value ? boardText(value as unknown as BoardSnapshot) : JSON.stringify(value);
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
    { description: "Git Activity and Pull requests views", _meta: resourceMeta },
    async () => ({ contents: [{ uri: BOARD_RESOURCE_URI, mimeType: RESOURCE_MIME_TYPE, text: html, _meta: resourceMeta }] }));
  registerAppTool(server, "open_board", {
    title: "Open BigBoard", description: "Open a Git/GitHub activity board. Omit board_id for an independent board or provide an existing ID to reopen it. Opening and viewing never collect data. If unconfigured, use configure_sources then refresh_sources.",
    inputSchema: { board_id: boardId.optional() }, annotations: { ...annotations, readOnlyHint: false }, _meta: { ui: { resourceUri: BOARD_RESOURCE_URI, visibility: ["model", "app"] } },
  }, ({ board_id }) => result(() => store.open(board_id)));
  server.registerTool("read_board", {
    title: "Read BigBoard", description: "Read saved board state and collected evidence coverage without opening a panel or refreshing sources. Use before updating viewing filters.",
    inputSchema: { board_id: boardId }, annotations, _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id }) => result(() => store.read(board_id)));
  server.registerTool("update_board", {
    title: "Update BigBoard view", description: "Change this board's viewing filters and comparison using its latest expected_revision. Repository must be all or an ID in list_sources. Viewing changes never change collected repositories or perform network requests.",
    inputSchema: { board_id: boardId, expected_revision: z.number().int().positive(), days: z.union([z.literal(7),z.literal(14)]).optional(), repository: z.string().min(1).optional(), compare: z.boolean().optional() },
    annotations: { ...annotations, readOnlyHint: false }, _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id,expected_revision,days,repository,compare }) => result(() => store.update(board_id,expected_revision,{ ...(days === undefined ? {} : { days }), ...(repository === undefined ? {} : { repository }), ...(compare === undefined ? {} : { compare }) })));
  server.registerTool("query_evidence", {
    title: "Inspect BigBoard evidence", description: "Page evidence for an exact snapshot and optional selected 24-hour bucket. Use kind:activity for commits or kind:pull_requests with pull_request_state:open/merged for the PR view. An exact pull_request_id selects one available PR within the board scope. Commits use author dates; merges use merged_at and default branch; open PRs ignore the historical interval. Reuse nextCursor with the same snapshot, selection, and filters.",
    inputSchema: { board_id: boardId, snapshot_id: z.string(), bucket_start: z.string().optional(), cursor: z.string().optional(), kind: z.enum(["all", "activity", "pull_requests"]).optional(), pull_request_state: z.enum(["open", "merged"]).optional(), pull_request_id: z.string().min(1).max(256).optional() }, annotations, _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id,snapshot_id,bucket_start,cursor,kind,pull_request_state,pull_request_id }) => result(() => store.evidence(board_id,snapshot_id,bucket_start,cursor,{kind,pullRequestState:pull_request_state,pullRequestId:pull_request_id})));
  server.registerTool("list_sources", {
    title: "List BigBoard sources", description: "Read saved collection selection and its revision. Paths refer to existing local clones, distinct from temporary viewing filters.", inputSchema: {}, annotations,
  }, () => result(async () => store.sources()));
  server.registerTool("configure_sources", {
    title: "Configure BigBoard sources", description: "Save local repository paths, or set import_native:true to copy native BigBoard selected repositories read-only. Provide exactly one of paths or import_native:true. Pass expected_revision from list_sources to avoid overwriting changed settings. Replaces selection and clears old collected data; does not fetch or modify clones.",
    inputSchema: { paths: z.array(z.string().min(1)).max(100).optional(), import_native: z.boolean().optional(), expected_revision: z.number().int().nonnegative().optional() }, annotations: { ...annotations, readOnlyHint: false },
  }, ({ paths,import_native,expected_revision }) => result(() => store.configure(paths,import_native === true,expected_revision)));
  server.registerTool("refresh_sources", {
    title: "Refresh BigBoard sources", description: "Explicitly collect 28 days of author-date Git history from saved local remote-tracking default refs plus open and merged GitHub PRs. Reads existing clones without fetching/modifying them, authenticates using gh auth token, and saves coverage/collection time. May take up to 150 seconds. Polling and filter changes do not refresh.",
    inputSchema: {}, annotations: { ...annotations, readOnlyHint: false, openWorldHint: true }, _meta: { ui: { visibility: ["model", "app"] } },
  }, (_,extra) => result(() => store.refresh(extra.signal)));
  return server;
}
