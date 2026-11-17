import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { registerAppResource, registerAppTool, RESOURCE_MIME_TYPE } from "@modelcontextprotocol/ext-apps/server";
import { z } from "zod";
import { BoardError, BoardStore, type EvidenceResult } from "./core";
import { APP_VERSION, BOARD_RESOURCE_URI, REPOSITORIES, type BoardSnapshot } from "./shared";

const boardId = z.string().uuid();
const annotations = { readOnlyHint: true, destructiveHint: false, openWorldHint: false };

function boardText(board: BoardSnapshot): string {
  const comparison = board.query.compare
    ? board.totals.previousCommits === null ? " Prior-period comparison is unavailable." : ` Previous period: ${board.totals.previousCommits} commits.`
    : "";
  return `Synthetic BigBoard activity. Board ${board.boardId}, revision ${board.revision}, snapshot ${board.snapshotId}. ` +
    `${board.query.repository}, [${board.scope.since}, ${board.scope.until}) UTC: ${board.totals.commits} commits, ` +
    `${board.totals.additions} additions, ${board.totals.deletions} deletions.${comparison} ${board.coverage.note}`;
}

function evidenceText(evidence: EvidenceResult): string {
  const selected = evidence.bucketStart ? ` Bucket ${evidence.bucketStart}.` : " Whole current board.";
  return `Synthetic fixture evidence. Board ${evidence.boardId}, revision ${evidence.revision}, snapshot ${evidence.snapshotId}. ` +
    `${evidence.query.repository}, [${evidence.scope.since}, ${evidence.scope.until}) UTC.${selected} ` +
    `${evidence.records.length} records. ${evidence.coverage.note}\n${JSON.stringify(evidence.records)}`;
}

async function result<T extends BoardSnapshot | EvidenceResult>(operation: () => Promise<T>): Promise<CallToolResult> {
  try {
    const value = await operation();
    return {
      content: [{ type: "text", text: "records" in value ? evidenceText(value) : boardText(value) }],
      structuredContent: value as unknown as Record<string, unknown>,
    };
  } catch (error) {
    const code = error instanceof BoardError ? error.code : "storage_error";
    const message = error instanceof BoardError ? error.message : "Board state could not be accessed. Try again or create a new board.";
    return { isError: true, content: [{ type: "text", text: message }], structuredContent: { error: { code, message } } };
  }
}

export function createBigboardServer(html: string, store: BoardStore): McpServer {
  const server = new McpServer({ name: "bigboard-mcp-app", version: APP_VERSION });
  const resourceMeta = { ui: { csp: { connectDomains: [], resourceDomains: [] }, prefersBorder: true } };
  registerAppResource(server, "BigBoard activity", BOARD_RESOURCE_URI,
    { description: "Interactive synthetic activity board", _meta: resourceMeta },
    async () => ({ contents: [{ uri: BOARD_RESOURCE_URI, mimeType: RESOURCE_MIME_TYPE, text: html, _meta: resourceMeta }] }),
  );
  registerAppTool(server, "open_board", {
    title: "Open BigBoard",
    description: "Open an interactive activity board backed by synthetic fixture data. Omit board_id to create an independent board; provide a saved board ID to reopen it. No live repositories are read.",
    inputSchema: { board_id: boardId.optional() },
    annotations: { ...annotations, readOnlyHint: false },
    _meta: { ui: { resourceUri: BOARD_RESOURCE_URI, visibility: ["model", "app"] } },
  }, ({ board_id }) => result(() => store.open(board_id)));

  server.registerTool("read_board", {
    title: "Read BigBoard",
    description: "Read a board's latest authoritative state, revision, scope, and fixture coverage without opening another view. Use this before updating a board after a conflict.",
    inputSchema: { board_id: boardId },
    annotations,
    _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id }) => result(() => store.read(board_id)));

  server.registerTool("update_board", {
    title: "Update BigBoard view",
    description: "Update this board's viewing filters or prior-period comparison. Pass its latest revision as expected_revision. Changes appear in the existing board through explicit reads; other boards remain independent. Only local fixture view state changes.",
    inputSchema: {
      board_id: boardId,
      expected_revision: z.number().int().positive(),
      days: z.union([z.literal(7), z.literal(14)]).optional(),
      repository: z.enum(REPOSITORIES).optional(),
      compare: z.boolean().optional(),
    },
    annotations: { ...annotations, readOnlyHint: false },
    _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id, expected_revision, days, repository, compare }) => {
    const patch = {
      ...(days === undefined ? {} : { days }),
      ...(repository === undefined ? {} : { repository }),
      ...(compare === undefined ? {} : { compare }),
    };
    return result(() => store.update(board_id, expected_revision, patch));
  });

  server.registerTool("query_evidence", {
    title: "Inspect BigBoard evidence",
    description: "Read exact synthetic records supporting the current board or selected UTC day. Use boardId and snapshotId from the selection context or latest board result. Old snapshots and buckets outside the current scope are rejected.",
    inputSchema: { board_id: boardId, snapshot_id: z.string(), bucket_start: z.string().optional() },
    annotations,
    _meta: { ui: { visibility: ["model", "app"] } },
  }, ({ board_id, snapshot_id, bucket_start }) => result(() => store.evidence(board_id, snapshot_id, bucket_start)));
  return server;
}
