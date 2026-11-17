import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { AppBridge } from "@modelcontextprotocol/ext-apps/app-bridge";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import type { CallToolResult, JSONRPCMessage } from "@modelcontextprotocol/sdk/types.js";
import { JSDOM, VirtualConsole } from "jsdom";
import type { ActivityRecord, BoardQuery, BoardSnapshot, EvidenceResult, PullRequestRecord, SelectionContext } from "../src/shared.js";

type ContextUpdate = Parameters<NonNullable<AppBridge["onupdatemodelcontext"]>>[0];
type ChatMessage = Parameters<NonNullable<AppBridge["onmessage"]>>[0];
const DAY = 86_400_000;
const END = Date.parse("2026-11-17T12:00:00.000Z");

async function waitFor(predicate: () => boolean, description: string, timeout = 6000): Promise<void> {
  const deadline = Date.now() + timeout;
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error(`Timed out waiting for ${description}`);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

function dailyButtons(document: Document): HTMLButtonElement[] {
  return [...document.querySelectorAll<HTMLButtonElement>('section[aria-label="Daily commits"] button[aria-pressed]')];
}

function button(document: Document, label: string): HTMLButtonElement {
  const found = [...document.querySelectorAll<HTMLButtonElement>("button")].find((item) => item.textContent?.trim() === label);
  assert.ok(found, `Missing ${label} button`);
  return found;
}

class Workspace {
  boards = new Map<string, { revision: number; query: BoardQuery }>();
  generation = 1;
  configured = true;
  collected = true;
  readonly records: ActivityRecord[] = Array.from({ length: 56 }, (_, index) => ({
    id: `sample/${index % 2 ? "web" : "api"}:${index.toString(16).padStart(40, "0")}`,
    repository: `sample/${index % 2 ? "web" : "api"}`,
    sha: index.toString(16).padStart(40, "0"),
    timestamp: new Date(END - (28 - Math.floor(index / 2)) * DAY + 3_600_000).toISOString(),
    subject: `Improve activity record ${index + 1}`,
    contributor: `Contributor ${index % 2 ? "B" : "A"}`,
    email: `contributor-${index % 2}@example.test`,
    additions: index === 48 ? null : 10 + index,
    deletions: index === 48 ? null : 2,
    files: [],
    url: `https://github.com/sample/${index % 2 ? "web" : "api"}/commit/${index.toString(16).padStart(40, "0")}`,
  }));
  readonly prs: PullRequestRecord[] = [
    { id: "sample/api#2", repository: "sample/api", number: 2, title: "Improve activity loading", url: "https://github.com/sample/api/pull/2", author: "contributor-a", state: "merged", draft: false, createdAt: "2026-11-01T09:00:00Z", updatedAt: "2026-11-15T13:00:00Z", mergedAt: "2026-11-15T13:00:00Z", baseBranch: "main", headSha: "a".repeat(40) },
    { id: "sample/web#9", repository: "sample/web", number: 9, title: "An older draft still open", url: "https://github.com/sample/web/pull/9", author: "contributor-b", state: "open", draft: true, createdAt: "2026-05-01T09:00:00Z", updatedAt: "2026-11-16T10:00:00Z", mergedAt: null, baseBranch: "main", headSha: "b".repeat(40) },
  ];

  open(): BoardSnapshot {
    const boardId = `00000000-0000-4000-8000-${(this.boards.size + 1).toString().padStart(12, "0")}`;
    this.boards.set(boardId, { revision: 1, query: { days: 7, repository: "all", compare: false } });
    return this.snapshot(boardId);
  }

  snapshot(boardId: string): BoardSnapshot {
    const saved = this.boards.get(boardId);
    assert.ok(saved);
    const query = saved.query;
    const since = END - query.days * DAY;
    const included = this.records.filter((record) => (query.repository === "all" || record.repository === query.repository));
    const current = this.collected ? included.filter((record) => Date.parse(record.timestamp) >= since) : [];
    const previous = included.filter((record) => Date.parse(record.timestamp) >= since - query.days * DAY && Date.parse(record.timestamp) < since);
    const prs = this.collected ? this.prs.filter((pr) => query.repository === "all" || pr.repository === query.repository) : [];
    const buckets = Array.from({ length: query.days }, (_, index) => {
      const start = since + index * DAY;
      const end = start + DAY;
      const records = current.filter((record) => Date.parse(record.timestamp) >= start && Date.parse(record.timestamp) < end);
      return {
        start: new Date(start).toISOString(), end: new Date(end).toISOString(), commits: records.length,
        additions: records.reduce((sum, record) => sum + (record.additions ?? 0), 0), deletions: records.reduce((sum, record) => sum + (record.deletions ?? 0), 0),
        unknownLineCommits: records.filter((record) => record.additions === null).length,
        previousCommits: query.compare ? previous.filter((record) => Date.parse(record.timestamp) >= start - query.days * DAY && Date.parse(record.timestamp) < end - query.days * DAY).length : null,
        mergedPRs: prs.filter((pr) => pr.mergedAt && Date.parse(pr.mergedAt) >= start && Date.parse(pr.mergedAt) < end).length,
        recordIds: records.map((record) => record.id),
      };
    });
    return {
      boardId, revision: saved.revision, snapshotId: `${boardId}:${saved.revision}:${this.generation}`, updatedAt: new Date(END).toISOString(), query: { ...query },
      scope: { since: new Date(since).toISOString(), until: new Date(END).toISOString(), timezone: "UTC" },
      coverage: { source: "git+github", complete: false, note: this.collected ? "Some line evidence is unavailable. Git remote-tracking refs were read without fetching." : "Refresh sources to collect evidence.", repositories: this.configured ? ["sample/api", "sample/web"].map((repository) => ({ repository, revision: "a".repeat(40), ref: "refs/remotes/origin/main", shallow: false, partial: true, historyComplete: true, linesComplete: false, openPRsComplete: true, mergedPRsComplete: true, errors: ["Line evidence is incomplete."] })) : [] },
      configuration: { revision: 1, repositories: this.configured ? ["sample/api", "sample/web"] : [], needsRefresh: !this.collected },
      account: { id: "1", login: "sample-user" }, collectedAt: this.collected ? new Date(END + this.generation * 1000).toISOString() : null, buckets,
      totals: { commits: current.length, additions: buckets.reduce((sum, item) => sum + item.additions, 0), deletions: buckets.reduce((sum, item) => sum + item.deletions, 0), unknownLineCommits: buckets.reduce((sum, item) => sum + item.unknownLineCommits, 0), previousCommits: query.compare ? previous.length : null, mergedPRs: prs.filter((pr) => pr.mergedAt && Date.parse(pr.mergedAt) >= since).length, openPRs: prs.filter((pr) => pr.state === "open").length },
    };
  }

  async tool(params: { name: string; arguments?: Record<string, unknown> }): Promise<CallToolResult> {
    const args = params.arguments ?? {};
    if (params.name === "refresh_sources") {
      this.collected = true;
      this.generation += 1;
      return { content: [], structuredContent: { collectionId: `collection-${this.generation}`, complete: false } };
    }
    const boardId = String(args.board_id);
    const snapshot = this.snapshot(boardId);
    if (params.name === "read_board") return { content: [], structuredContent: { ...snapshot } };
    if (params.name === "update_board") {
      const saved = this.boards.get(boardId)!;
      if (args.expected_revision !== saved.revision) return { isError: true, content: [{ type: "text", text: "Read the current revision." }] };
      saved.revision += 1;
      saved.query = { ...saved.query, ...(args.repository === undefined ? {} : { repository: String(args.repository) }), ...(args.days === undefined ? {} : { days: args.days as 7 | 14 }), ...(args.compare === undefined ? {} : { compare: Boolean(args.compare) }) };
      return { content: [], structuredContent: { ...this.snapshot(boardId) } };
    }
    assert.equal(params.name, "query_evidence");
    if (args.snapshot_id !== snapshot.snapshotId) return { isError: true, content: [{ type: "text", text: "The selected snapshot changed." }] };
    const bucket = snapshot.buckets.find((item) => item.start === args.bucket_start);
    const records = this.collected ? this.records.filter((record) => (snapshot.query.repository === "all" || record.repository === snapshot.query.repository) && record.timestamp >= (bucket?.start ?? snapshot.scope.since) && record.timestamp < (bucket?.end ?? snapshot.scope.until)) : [];
    const prs = this.collected ? this.prs.filter((pr) => (snapshot.query.repository === "all" || pr.repository === snapshot.query.repository) && (pr.state === "open" || (pr.mergedAt! >= (bucket?.start ?? snapshot.scope.since) && pr.mergedAt! < (bucket?.end ?? snapshot.scope.until)))) : [];
    const offset = Number(args.cursor ?? 0);
    const evidence: EvidenceResult = { boardId, revision: snapshot.revision, snapshotId: snapshot.snapshotId, query: snapshot.query, scope: snapshot.scope, coverage: snapshot.coverage, bucketStart: bucket?.start ?? null, records: records.slice(offset, offset + 3), pullRequests: prs.slice(offset, offset + 3), nextCursor: Math.max(records.length, prs.length) > offset + 3 ? String(offset + 3) : null, totalRecords: records.length, totalPullRequests: prs.length };
    return { content: [], structuredContent: { ...evidence } };
  }
}

async function panel(workspace: Workspace, initial: BoardSnapshot) {
  const html = await readFile(new URL("../bundle/board.html", import.meta.url), "utf8");
  const host = new JSDOM("<!doctype html><html><body></body></html>");
  const contexts: ContextUpdate[] = [];
  const messages: ChatMessage[] = [];
  const calls: Array<{ name: string; arguments?: Record<string, unknown> }> = [];
  const errors: string[] = [];
  let guest: JSDOM | undefined;
  let closed = false;
  let initialized = false;
  const transport: Transport = {
    async start() {},
    async close() { transport.onclose?.(); },
    async send(message: JSONRPCMessage) {
      queueMicrotask(() => {
        if (!guest || closed) return;
        guest.window.dispatchEvent(new guest.window.MessageEvent("message", { data: structuredClone(message), source: host.window as unknown as MessageEventSource }));
      });
    },
  };
  host.window.postMessage = ((message: unknown) => { queueMicrotask(() => { if (!closed) transport.onmessage?.(structuredClone(message) as JSONRPCMessage); }); }) as typeof host.window.postMessage;
  const bridge = new AppBridge(null, { name: "BigBoard test host", version: "1.0.0" }, { serverTools: {}, updateModelContext: { text: {}, structuredContent: {} }, message: { text: {} } }, { hostContext: { theme: "light", displayMode: "inline", locale: "en-US", timeZone: "UTC" } });
  bridge.oncalltool = async (params) => { calls.push(params); return workspace.tool(params); };
  bridge.onupdatemodelcontext = async (params) => { contexts.push(params); return {}; };
  bridge.onmessage = async (params) => { messages.push(params); return {}; };
  bridge.oninitialized = () => { initialized = true; void bridge.sendToolInput({ arguments: {} }).then(() => bridge.sendToolResult({ content: [], structuredContent: { ...initial } })).catch((error) => errors.push(String(error))); };
  await bridge.connect(transport);
  guest = new JSDOM(html, { url: "https://bigboard.test/activity", runScripts: "dangerously", pretendToBeVisual: true, virtualConsole: new VirtualConsole(), beforeParse(window) {
    Object.defineProperty(window, "parent", { value: host.window });
    Object.defineProperty(window, "structuredClone", { value: structuredClone });
    Object.defineProperty(window, "ResizeObserver", { value: class { observe() {} unobserve() {} disconnect() {} } });
    window.addEventListener("error", (event) => errors.push(String(event.error ?? event.message)));
  } });
  await waitFor(() => initialized && contexts.length > 0, "MCP Apps handshake and initial context");
  const document = guest.window.document;
  const selectedContext = () => contexts.at(-1)?.structuredContent as unknown as SelectionContext;
  const close = async () => {
    if (closed || !guest) return;
    await bridge.teardownResource({}, { timeout: 1000 });
    guest.window.dispatchEvent(new guest.window.Event("pagehide"));
    closed = true;
    await bridge.close();
    guest.window.close();
    host.window.close();
  };
  return { document, contexts, messages, calls, errors, selectedContext, close };
}

test("rendered real-data board shares selections, includes current PRs, and observes agent changes", async (t) => {
  const workspace = new Workspace();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7 && first.document.querySelectorAll(".evidence-row").length > 0, "real activity records");
  assert.deepEqual([...first.document.querySelectorAll("option")].map((option) => option.value), ["all", "sample/api", "sample/web"]);
  assert.ok(first.document.body.textContent?.includes("Line totals are a lower bound"));
  const link = first.document.querySelector<HTMLAnchorElement>('.pr-section a[href="https://github.com/sample/web/pull/9"]');
  assert.ok(link);
  assert.equal(link.rel, "noopener noreferrer");
  assert.ok(first.document.body.textContent?.includes("Open draft"));
  const bucket = initial.buckets[0];
  dailyButtons(first.document)[0].click();
  await waitFor(() => first.selectedContext()?.bucketStart === bucket.start, "selected bucket context");
  assert.deepEqual(first.selectedContext().recordIds, bucket.recordIds);
  assert.equal(first.selectedContext().snapshotId, initial.snapshotId);
  await waitFor(() => first.calls.some((call) => call.name === "query_evidence" && call.arguments?.bucket_start === bucket.start), "scoped evidence request");
  await waitFor(() => Boolean(first.document.querySelector('.pr-section a[href="https://github.com/sample/web/pull/9"]')), "older open PR retained in selection");
  button(first.document, "↗ Explain selection").click();
  await waitFor(() => first.messages.length === 1, "selection follow-up message");
  assert.ok(JSON.stringify(first.messages[0].content).includes(initial.snapshotId));
  const root = first.document.getElementById("root");
  await workspace.tool({ name: "update_board", arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "sample/api", compare: true } });
  await waitFor(() => first.document.querySelector("select")?.value === "sample/api", "same board agent filter update");
  assert.equal(first.document.getElementById("root"), root);
  assert.equal(first.selectedContext().query.compare, true);
  assert.deepEqual(first.errors, []);
});

test("rendered evidence pagination and explicit source refresh keep one board", async (t) => {
  const workspace = new Workspace();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => first.document.querySelectorAll('[aria-label="Commit records"] article').length === 3, "first evidence page");
  assert.equal(first.calls.some((call) => call.name === "refresh_sources"), false);
  button(first.document, "Load more evidence").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Commit records"] article').length === 6, "next evidence page");
  assert.ok(first.calls.some((call) => call.name === "query_evidence" && call.arguments?.cursor === "3"));
  button(first.document, "Load more evidence").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Commit records"] article').length === 9, "unknown line evidence page");
  assert.ok(first.document.querySelector('[aria-label="Commit records"]')?.textContent?.includes("unknown"));
  const before = first.selectedContext().snapshotId;
  button(first.document, "Refresh sources").click();
  await waitFor(() => first.selectedContext()?.snapshotId !== before, "refreshed evidence snapshot");
  assert.equal(first.selectedContext().boardId, initial.boardId);
  assert.equal(first.calls.filter((call) => call.name === "refresh_sources").length, 1);
  assert.deepEqual(first.errors, []);
});

test("unconfigured board asks for sources without collecting, and independent boards recover on reopen", async (t) => {
  const workspace = new Workspace();
  workspace.configured = false;
  workspace.collected = false;
  const empty = await panel(workspace, workspace.open());
  t.after(empty.close);
  await waitFor(() => empty.document.body.textContent?.includes("Choose the repositories to collect") ?? false, "source setup prompt");
  assert.equal(empty.calls.some((call) => call.name === "refresh_sources" || call.name === "configure_sources"), false);
  assert.equal(button(empty.document, "Refresh sources").disabled, true);
  workspace.configured = true;
  workspace.collected = true;
  const initial = workspace.open();
  const other = workspace.open();
  const first = await panel(workspace, initial);
  const second = await panel(workspace, other);
  t.after(first.close);
  t.after(second.close);
  await workspace.tool({ name: "update_board", arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "sample/web", days: 14 } });
  await waitFor(() => first.selectedContext()?.query.repository === "sample/web", "first board changed scope");
  assert.equal(second.selectedContext().boardId, other.boardId);
  assert.deepEqual(second.selectedContext().query, other.query);
  const updated = first.selectedContext();
  await first.close();
  const reopened = await panel(workspace, initial);
  t.after(reopened.close);
  await waitFor(() => reopened.selectedContext()?.revision === updated.revision && dailyButtons(reopened.document).length === 14, "reopened latest scope");
  assert.equal(reopened.selectedContext().boardId, initial.boardId);
  assert.deepEqual(reopened.selectedContext().query, updated.query);
  assert.deepEqual([...empty.errors, ...first.errors, ...second.errors, ...reopened.errors], []);
});
