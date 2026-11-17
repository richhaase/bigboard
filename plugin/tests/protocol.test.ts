import assert from "node:assert/strict";
import { cp, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { getDefaultEnvironment, StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { BOARD_RESOURCE_URI, type ActivityRecord, type BoardSnapshot } from "../src/shared.js";

type ToolResult = Awaited<ReturnType<Client["callTool"]>>;

interface EvidenceResult {
  boardId: string;
  revision: number;
  snapshotId: string;
  scope: BoardSnapshot["scope"];
  coverage: BoardSnapshot["coverage"];
  records: ActivityRecord[];
}

function board(result: ToolResult): BoardSnapshot {
  assert.notEqual(result.isError, true, JSON.stringify(result.content));
  assert.ok(result.structuredContent);
  const snapshot = result.structuredContent as unknown as BoardSnapshot;
  assert.equal(snapshot.coverage.source, "fixture");
  assert.equal(typeof snapshot.boardId, "string");
  assert.equal(typeof snapshot.snapshotId, "string");
  return snapshot;
}

function evidence(result: ToolResult): EvidenceResult {
  assert.notEqual(result.isError, true, JSON.stringify(result.content));
  assert.ok(result.structuredContent);
  const value = result.structuredContent as unknown as EvidenceResult;
  assert.equal(value.coverage.source, "fixture");
  assert.ok(Array.isArray(value.records));
  return value;
}

async function installation(t: TestContext) {
  const root = await mkdtemp(join(tmpdir(), "bigboard-mcp-protocol-"));
  const installed = join(root, "installed");
  const bundle = fileURLToPath(new URL("../bundle/", import.meta.url));
  await cp(bundle, installed, { recursive: true });
  const clients = new Set<Client>();
  t.after(async () => {
    await Promise.all([...clients].map((client) => client.close()));
    await rm(root, { recursive: true, force: true });
  });
  const start = async () => {
    const client = new Client({ name: "bigboard-protocol-test", version: "0.1.0" });
    const transport = new StdioClientTransport({
      command: process.execPath,
      args: [join(installed, "server.cjs")],
      cwd: installed,
      env: { ...getDefaultEnvironment(), BIGBOARD_STATE_DIR: join(root, "state") },
      stderr: "pipe",
    });
    let stderr = "";
    transport.stderr?.on("data", (data) => { stderr += data.toString(); });
    clients.add(client);
    try {
      await client.connect(transport);
    } catch (error) {
      await transport.close();
      throw new Error(`Bundled server could not initialize: ${String(error)}\n${stderr}`);
    }
    return client;
  };
  const stop = async (client: Client) => {
    await client.close();
    clients.delete(client);
  };
  return { start, stop };
}

test("the standalone bundle exposes the board resource and keeps polling tools headless", async (t) => {
  const { start } = await installation(t);
  const client = await start();
  const inventory = await client.listTools();
  assert.deepEqual(inventory.tools.map((tool) => tool.name).sort(), [
    "open_board", "query_evidence", "read_board", "update_board",
  ]);
  const opener = inventory.tools.find((tool) => tool.name === "open_board");
  assert.equal((opener?._meta?.ui as { resourceUri?: string } | undefined)?.resourceUri, BOARD_RESOURCE_URI);
  for (const tool of inventory.tools.filter((tool) => tool.name !== "open_board")) {
    assert.equal((tool._meta?.ui as { resourceUri?: string } | undefined)?.resourceUri, undefined);
    assert.equal(tool._meta?.["openai/outputTemplate"], undefined);
  }
  const resource = await client.readResource({ uri: BOARD_RESOURCE_URI });
  assert.equal(resource.contents.length, 1);
  const content = resource.contents[0];
  assert.equal(content.mimeType, "text/html;profile=mcp-app");
  assert.ok("text" in content);
  assert.match(content.text, /<html\b/i);
  assert.match(content.text, /<script\b/i);
  assert.match(content.text, /ui\/initialize/);
  const result = await client.callTool({ name: "open_board", arguments: {} });
  const snapshot = board(result);
  assert.deepEqual(snapshot.query, { days: 7, repository: "all", compare: false });
  assert.equal(snapshot.buckets.length, 7);
  assert.equal(snapshot.totals.commits, snapshot.buckets.reduce((sum, bucket) => sum + bucket.commits, 0));
  assert.match(JSON.stringify(result.content), /fixture|synthetic/i);
});

test("agent updates and selected evidence use one revision while independent boards retain their scope", async (t) => {
  const { start } = await installation(t);
  const client = await start();
  const first = board(await client.callTool({ name: "open_board", arguments: {} }));
  const second = board(await client.callTool({ name: "open_board", arguments: {} }));
  assert.notEqual(first.boardId, second.boardId);
  const changed = board(await client.callTool({
    name: "update_board",
    arguments: {
      board_id: first.boardId,
      expected_revision: first.revision,
      repository: "sample/api",
      compare: true,
    },
  }));
  assert.equal(changed.boardId, first.boardId);
  assert.equal(changed.revision, first.revision + 1);
  assert.notEqual(changed.snapshotId, first.snapshotId);
  assert.deepEqual(changed.query, { days: 7, repository: "sample/api", compare: true });
  assert.equal(changed.coverage.complete, true);
  assert.equal(typeof changed.totals.previousCommits, "number");
  const polled = board(await client.callTool({ name: "read_board", arguments: { board_id: first.boardId } }));
  assert.deepEqual(polled, changed);
  assert.deepEqual(board(await client.callTool({ name: "read_board", arguments: { board_id: second.boardId } })), second);
  const bucket = changed.buckets.find((value) => value.commits > 0);
  assert.ok(bucket);
  const selected = evidence(await client.callTool({
    name: "query_evidence",
    arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId, bucket_start: bucket.start },
  }));
  assert.equal(selected.boardId, changed.boardId);
  assert.equal(selected.revision, changed.revision);
  assert.equal(selected.snapshotId, changed.snapshotId);
  assert.deepEqual(selected.records.map((record) => record.id).sort(), [...bucket.recordIds].sort());
  assert.equal(selected.records.length, bucket.commits);
  assert.ok(selected.records.every((record) => record.repository === "sample/api"));
  assert.ok(selected.records.every((record) => record.timestamp >= bucket.start && record.timestamp < bucket.end));
  const all = evidence(await client.callTool({
    name: "query_evidence",
    arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId },
  }));
  assert.equal(all.records.length, changed.totals.commits);
  assert.ok(all.records.every((record) => record.timestamp >= changed.scope.since && record.timestamp < changed.scope.until));
  const conflict = await client.callTool({
    name: "update_board",
    arguments: { board_id: changed.boardId, expected_revision: first.revision, repository: "sample/web" },
  });
  assert.equal(conflict.isError, true);
  assert.deepEqual(board(await client.callTool({ name: "read_board", arguments: { board_id: first.boardId } })), changed);
  const staleSelection = await client.callTool({
    name: "query_evidence",
    arguments: { board_id: changed.boardId, snapshot_id: first.snapshotId, bucket_start: bucket.start },
  });
  assert.equal(staleSelection.isError, true);
  const longer = board(await client.callTool({
    name: "update_board",
    arguments: { board_id: changed.boardId, expected_revision: changed.revision, days: 14 },
  }));
  assert.equal(longer.buckets.length, 14);
  assert.equal(longer.query.compare, true);
  assert.equal(longer.totals.previousCommits, null);
  assert.equal(longer.coverage.complete, false);
});

test("reopening a saved board after the stdio server restarts preserves the agent's update", async (t) => {
  const { start, stop } = await installation(t);
  const original = await start();
  const initial = board(await original.callTool({ name: "open_board", arguments: {} }));
  const updated = board(await original.callTool({
    name: "update_board",
    arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "sample/web", days: 14 },
  }));
  await stop(original);
  const restarted = await start();
  const reopened = board(await restarted.callTool({ name: "open_board", arguments: { board_id: initial.boardId } }));
  assert.deepEqual(reopened, updated);
  const records = evidence(await restarted.callTool({
    name: "query_evidence", arguments: { board_id: reopened.boardId, snapshot_id: reopened.snapshotId },
  }));
  assert.equal(records.records.length, reopened.totals.commits);
  assert.ok(records.records.every((record) => record.repository === "sample/web"));
});

test("two stdio processes cannot overwrite the same board revision", async (t) => {
  const { start } = await installation(t);
  const first = await start();
  const second = await start();
  const initial = board(await first.callTool({ name: "open_board", arguments: {} }));
  const results = await Promise.all([
    first.callTool({
      name: "update_board",
      arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "sample/api" },
    }),
    second.callTool({
      name: "update_board",
      arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "sample/web" },
    }),
  ]);
  const successful = results.filter((result) => result.isError !== true);
  assert.equal(successful.length, 1);
  assert.equal(results.filter((result) => result.isError === true).length, 1);
  const saved = board(successful[0]);
  assert.equal(saved.revision, initial.revision + 1);
  assert.deepEqual(board(await first.callTool({ name: "read_board", arguments: { board_id: initial.boardId } })), saved);
  assert.deepEqual(board(await second.callTool({ name: "read_board", arguments: { board_id: initial.boardId } })), saved);
});
