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
  const found = [...document.querySelectorAll<HTMLButtonElement>("button")].find((item) => item.getAttribute("aria-label") === label || item.textContent?.replace("↗", "").trim() === label);
  assert.ok(found, `Missing ${label} button`);
  return found;
}

class Workspace {
  boards = new Map<string, { revision: number; query: BoardQuery }>();
  generation = 1;
  configured = true;
  collected = true;
  delayEvidence: ((args: Record<string, unknown>) => Promise<void>) | null = null;
  completedEvidence: Array<Record<string, unknown>> = [];
  readonly records: ActivityRecord[] = Array.from({ length: 62 }, (_, index) => ({
    id: `sample/${index % 2 ? "web" : "api"}:${index.toString(16).padStart(40, "0")}`,
    repository: `sample/${index % 2 ? "web" : "api"}`,
    sha: index.toString(16).padStart(40, "0"),
    timestamp: new Date(END - (28 - (index >= 56 ? 24 : Math.floor(index / 2))) * DAY + (index >= 56 ? index - 54 : 1) * 3_600_000).toISOString(),
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
    { id: "sample/api#10", repository: "sample/api", number: 10, title: "Refine API loading", url: "https://github.com/sample/api/pull/10", author: "contributor-a", state: "open", draft: false, createdAt: "2026-11-10T09:00:00Z", updatedAt: "2026-11-16T11:00:00Z", mergedAt: null, baseBranch: "main", headSha: "c".repeat(40) },
    { id: "sample/web#11", repository: "sample/web", number: 11, title: "Improve keyboard navigation", url: "https://github.com/sample/web/pull/11", author: "contributor-b", state: "open", draft: false, createdAt: "2026-11-12T09:00:00Z", updatedAt: "2026-11-14T11:00:00Z", mergedAt: null, baseBranch: "main", headSha: "d".repeat(40) },
    { id: "sample/api#12", repository: "sample/api", number: 12, title: "Add retry coverage", url: "https://github.com/sample/api/pull/12", author: "contributor-a", state: "open", draft: false, createdAt: "2026-11-12T09:00:00Z", updatedAt: "2026-11-13T11:00:00Z", mergedAt: null, baseBranch: "main", headSha: "e".repeat(40) },
    { id: "sample/web#13", repository: "sample/web", number: 13, title: "Update source status", url: "https://github.com/sample/web/pull/13", author: "contributor-b", state: "merged", draft: false, createdAt: "2026-11-11T09:00:00Z", updatedAt: "2026-11-13T15:00:00Z", mergedAt: "2026-11-13T15:00:00Z", baseBranch: "main", headSha: "f".repeat(40) },
    { id: "sample/api#1", repository: "sample/api", number: 1, title: "An older merged change", url: "https://github.com/sample/api/pull/1", author: "contributor-a", state: "merged", draft: false, createdAt: "2026-10-01T09:00:00Z", updatedAt: "2026-10-25T13:00:00Z", mergedAt: "2026-10-25T13:00:00Z", baseBranch: "main", headSha: "1".repeat(40) },
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
    const included = this.records.filter(record => query.repository === "all" || record.repository === query.repository);
    const current = this.collected ? included.filter(record => Date.parse(record.timestamp) >= since && Date.parse(record.timestamp) < END) : [];
    const previous = included.filter(record => Date.parse(record.timestamp) >= since - query.days * DAY && Date.parse(record.timestamp) < since);
    const prs = this.collected ? this.prs.filter(pr => query.repository === "all" || pr.repository === query.repository) : [];
    const buckets = Array.from({ length: query.days }, (_, index) => {
      const start = since + index * DAY;
      const end = start + DAY;
      const records = current.filter(record => Date.parse(record.timestamp) >= start && Date.parse(record.timestamp) < end);
      return {
        start: new Date(start).toISOString(), end: new Date(end).toISOString(), commits: records.length,
        additions: records.reduce((sum, record) => sum + (record.additions ?? 0), 0), deletions: records.reduce((sum, record) => sum + (record.deletions ?? 0), 0),
        unknownLineCommits: records.filter(record => record.additions === null).length,
        previousCommits: query.compare ? previous.filter(record => Date.parse(record.timestamp) >= start - query.days * DAY && Date.parse(record.timestamp) < end - query.days * DAY).length : null,
        mergedPRs: prs.filter(pr => pr.mergedAt && Date.parse(pr.mergedAt) >= start && Date.parse(pr.mergedAt) < end).length,
        recordIds: records.map(record => record.id),
      };
    });
    return {
      boardId, revision: saved.revision, snapshotId: `${boardId}:${saved.revision}:${this.generation}`, updatedAt: new Date(END).toISOString(), query: { ...query },
      scope: { since: new Date(since).toISOString(), until: new Date(END).toISOString(), timezone: "UTC" },
      coverage: { source: "git+github", complete: false, note: this.collected ? "Some line evidence is unavailable. Git remote-tracking refs were read without fetching." : "Refresh sources to collect evidence.", repositories: this.configured ? ["sample/api", "sample/web"].map(repository => ({ repository, revision: "a".repeat(40), ref: "refs/remotes/origin/main", shallow: false, partial: true, historyComplete: true, linesComplete: false, openPRsComplete: true, mergedPRsComplete: true, errors: ["Line evidence is incomplete."] })) : [] },
      configuration: { revision: 1, repositories: this.configured ? ["sample/api", "sample/web"] : [], needsRefresh: !this.collected },
      account: { id: "1", login: "sample-user" }, collectedAt: this.collected ? new Date(END + this.generation * 1000).toISOString() : null, buckets,
      totals: { commits: current.length, additions: buckets.reduce((sum, item) => sum + item.additions, 0), deletions: buckets.reduce((sum, item) => sum + item.deletions, 0), unknownLineCommits: buckets.reduce((sum, item) => sum + item.unknownLineCommits, 0), previousCommits: query.compare ? previous.length : null, mergedPRs: prs.filter(pr => pr.mergedAt && Date.parse(pr.mergedAt) >= since && Date.parse(pr.mergedAt) < END).length, openPRs: prs.filter(pr => pr.state === "open").length },
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
    await this.delayEvidence?.(args);
    const kind = String(args.kind ?? (args.pull_request_state || args.pull_request_id ? "pull_requests" : "all"));
    const prState = args.pull_request_state as "open" | "merged" | undefined;
    const prId = args.pull_request_id as string | undefined;
    if (kind === "activity" && (prState || prId)) return { isError: true, content: [{ type: "text", text: "Incompatible evidence filters." }] };
    const bucket = snapshot.buckets.find(item => item.start === args.bucket_start);
    const records = kind !== "pull_requests" && this.collected ? this.records.filter(record => (snapshot.query.repository === "all" || record.repository === snapshot.query.repository) && record.timestamp >= (bucket?.start ?? snapshot.scope.since) && record.timestamp < (bucket?.end ?? snapshot.scope.until)).sort((a,b) => b.timestamp.localeCompare(a.timestamp) || a.id.localeCompare(b.id)) : [];
    const prs = kind !== "activity" && this.collected ? this.prs.filter(pr => (snapshot.query.repository === "all" || pr.repository === snapshot.query.repository) && (!prState || pr.state === prState) && (!prId || pr.id === prId) && (pr.state === "open" || (pr.mergedAt! >= (bucket?.start ?? snapshot.scope.since) && pr.mergedAt! < (bucket?.end ?? snapshot.scope.until)))).sort((a,b) => b.updatedAt.localeCompare(a.updatedAt) || a.id.localeCompare(b.id)) : [];
    if (prId && !prs.length) return { isError: true, content: [{ type: "text", text: "The selected pull request is unavailable." }] };
    const signature = JSON.stringify([snapshot.snapshotId, args.bucket_start ?? null, kind, prState ?? null, prId ?? null]);
    let offset = 0;
    if (args.cursor) {
      const decoded = JSON.parse(Buffer.from(String(args.cursor), "base64url").toString("utf8"));
      if (decoded[0] !== signature) return { isError: true, content: [{ type: "text", text: "The evidence cursor changed scope." }] };
      offset = decoded[1];
    }
    const nextOffset = offset + 3;
    const evidence: EvidenceResult = { boardId, revision: snapshot.revision, snapshotId: snapshot.snapshotId, query: snapshot.query, scope: snapshot.scope, coverage: snapshot.coverage, bucketStart: bucket?.start ?? null, kind: kind as "all" | "activity" | "pull_requests", pullRequestState: prState, selectedPullRequestId: prId, records: records.slice(offset, nextOffset), pullRequests: prs.slice(offset, nextOffset), nextCursor: Math.max(records.length, prs.length) > nextOffset ? Buffer.from(JSON.stringify([signature, nextOffset])).toString("base64url") : null, totalRecords: records.length, totalPullRequests: prs.length };
    this.completedEvidence.push(args);
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

test("Activity stays quiet until a bucket is selected, then shares only its supporting commits", async t => {
  const workspace = new Workspace();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7, "Activity chart");
  assert.equal(first.calls.filter(call => call.name === "query_evidence").length, 0);
  assert.equal(first.document.querySelector('[aria-label="Commit records"]'), null);
  assert.equal(first.document.querySelector('[aria-label="Pull request records"]'), null);
  assert.equal(first.document.querySelector('[aria-label="Activity evidence"]'), null);
  assert.equal(first.document.querySelector('[aria-label="Selected evidence"]'), null);
  assert.equal(first.document.querySelector('[aria-label="Pull request evidence"]'), null);
  assert.equal(first.selectedContext().view, "activity");
  const bucket = initial.buckets.find(item => item.commits > 3)!;
  const index = initial.buckets.indexOf(bucket);
  dailyButtons(first.document)[index].click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Commit records"] article').length === 3, "selected Activity evidence");
  const query = first.calls.find(call => call.name === "query_evidence")!;
  assert.equal(query.arguments?.kind, "activity");
  assert.equal(query.arguments?.bucket_start, bucket.start);
  assert.equal(first.document.querySelector('[aria-label="Pull request records"]'), null);
  assert.equal(first.selectedContext().bucketStart, bucket.start);
  assert.deepEqual(first.selectedContext().recordIds, bucket.recordIds);
  assert.equal(first.selectedContext().snapshotId, initial.snapshotId);
  button(first.document, "Explain selection").click();
  await waitFor(() => first.messages.length === 1, "Activity follow-up");
  assert.match(JSON.stringify(first.messages[0].content), /activity/);
  assert.ok(JSON.stringify(first.messages[0].content).includes(bucket.start));
  button(first.document, "Load more evidence").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Commit records"] article').length === 6, "selected Activity next page");
  button(first.document, "Load more evidence").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Commit records"] article').length === bucket.commits, "all selected commit records");
  assert.ok(first.document.querySelector('[aria-label="Commit records"]')?.textContent?.includes("unknown"));
  const evidenceCalls = first.calls.filter(call => call.name === "query_evidence").length;
  button(first.document, "Close activity evidence").click();
  await waitFor(() => first.document.querySelector('[aria-label="Activity evidence"]') === null && first.selectedContext()?.bucketStart === null, "dismissed Activity detail");
  assert.equal(first.calls.filter(call => call.name === "query_evidence").length, evidenceCalls);
  assert.deepEqual(first.errors, []);
});

test("Pull requests has separate Open and Merged inventories and exact selected PR context", async t => {
  const workspace = new Workspace();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7, "initial Activity");
  dailyButtons(first.document)[0].click();
  await waitFor(() => Boolean(first.selectedContext()?.bucketStart), "retained Activity selection");
  button(first.document, "Pull requests").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] button').length === 3, "Open PR inventory");
  assert.equal(first.document.querySelector('[aria-label="Daily commits"]'), null);
  assert.equal(first.document.querySelector('[aria-label="Activity evidence"]'), null);
  assert.equal(first.selectedContext().view, "pullRequests");
  assert.equal(first.selectedContext().bucketStart, null);
  assert.deepEqual(first.selectedContext().recordIds, []);
  assert.equal(first.selectedContext().pullRequestState, "open");
  const prList = first.document.querySelector('[aria-label="Pull request records"]')!;
  assert.ok(prList.textContent?.includes("An older draft still open"));
  assert.ok(!prList.textContent?.includes("Improve activity loading"));
  assert.equal(first.document.querySelector('[aria-label="Pull request details"]'), null);
  const selected = first.document.querySelector<HTMLButtonElement>('button[aria-label="Select pull request #9: An older draft still open"]');
  assert.ok(selected);
  selected.click();
  await waitFor(() => first.document.querySelector('[aria-label="Pull request details"]') !== null && first.selectedContext()?.pullRequestId === "sample/web#9", "selected PR details");
  const link = first.document.querySelector<HTMLAnchorElement>('[aria-label="Pull request details"] a[href="https://github.com/sample/web/pull/9"]');
  assert.ok(link);
  assert.equal(link.rel, "noopener noreferrer");
  button(first.document, "Explain selection").click();
  await waitFor(() => first.messages.length === 1, "PR follow-up");
  const message = JSON.stringify(first.messages[0].content);
  assert.ok(message.includes("sample/web#9"));
  assert.ok(message.includes(initial.snapshotId));
  assert.equal(first.selectedContext().bucketStart, null);
  button(first.document, "Close pull request details").click();
  await waitFor(() => first.document.querySelector('[aria-label="Pull request details"]') === null, "dismissed PR detail");
  button(first.document, "Load more pull requests").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] button').length === 4, "Open PR next page");
  button(first.document, "Merged").click();
  await waitFor(() => first.document.querySelector('[aria-label="Pull request records"]')?.textContent?.includes("Improve activity loading") ?? false, "Merged PR inventory");
  assert.equal(first.selectedContext().pullRequestState, "merged");
  assert.equal(first.selectedContext().pullRequestId, null);
  assert.ok(!first.document.querySelector('[aria-label="Pull request records"]')?.textContent?.includes("An older draft still open"));
  assert.ok(!first.document.querySelector('[aria-label="Pull request records"]')?.textContent?.includes("An older merged change"));
  assert.equal(first.document.querySelector('[aria-label="Commit records"]'), null);
  button(first.document, "Activity").click();
  await waitFor(() => first.selectedContext()?.view === "activity" && first.selectedContext()?.bucketStart === initial.buckets[0].start, "Activity selection restored without PR scope");
  assert.equal(first.selectedContext().pullRequestId, undefined);
  await waitFor(() => first.document.querySelector('[aria-label="Activity evidence"]') !== null, "restored Activity detail");
  button(first.document, "Pull requests").click();
  await waitFor(() => first.selectedContext()?.view === "pullRequests" && first.selectedContext()?.pullRequestState === "merged", "retained PR subview");
  assert.equal(first.selectedContext().bucketStart, null);
  assert.equal(first.document.querySelector('[aria-label="Pull request details"]'), null);
  assert.deepEqual(first.errors, []);
});

test("late Activity evidence cannot replace a newer PR view or reuse its cursor", async t => {
  const workspace = new Workspace();
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  workspace.delayEvidence = args => args.kind === "activity" ? gate : Promise.resolve();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7, "initial Activity chart");
  dailyButtons(first.document)[0].click();
  await waitFor(() => first.calls.some(call => call.name === "query_evidence" && call.arguments?.kind === "activity"), "delayed Activity query");
  button(first.document, "Pull requests").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] button').length === 3, "newer PR inventory");
  release();
  await waitFor(() => workspace.completedEvidence.some(args => args.kind === "activity"), "late Activity completion");
  assert.equal(first.document.querySelector('[aria-label="Activity evidence"]'), null);
  assert.equal(first.document.querySelector('[aria-label="Commit records"]'), null);
  assert.equal(first.document.querySelectorAll('[aria-label="Pull request records"] button').length, 3);
  button(first.document, "Merged").click();
  await waitFor(() => first.selectedContext()?.pullRequestState === "merged" && first.document.querySelectorAll('[aria-label="Pull request records"] button').length === 2, "Merged state after late reply");
  const mergedCalls = first.calls.filter(call => call.name === "query_evidence" && call.arguments?.pull_request_state === "merged");
  assert.ok(mergedCalls.length > 0);
  assert.ok(mergedCalls.every(call => call.arguments?.cursor === undefined));
  assert.deepEqual(first.errors, []);
});

test("compact source status preserves diagnostics and explicit refresh while agent updates keep the same board", async t => {
  const workspace = new Workspace();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7, "initial Activity chart");
  assert.equal(first.calls.some(call => call.name === "refresh_sources"), false);
  const details = [...first.document.querySelectorAll("details")].find(detail => detail.textContent?.includes("Line evidence is incomplete"));
  assert.ok(details, "Coverage remains accessible");
  assert.equal(details.open, false);
  details.querySelector("summary")?.click();
  assert.equal(details.open, true);
  assert.ok(details.textContent?.includes("refs/remotes/origin/main"));
  const before = first.selectedContext().snapshotId;
  button(first.document, "Refresh sources").click();
  await waitFor(() => first.selectedContext()?.snapshotId !== before, "explicit source refresh");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  const root = first.document.getElementById("root");
  await workspace.tool({ name: "update_board", arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "sample/api", compare: true } });
  await waitFor(() => first.document.querySelector("select")?.value === "sample/api", "agent filter update");
  assert.equal(first.document.getElementById("root"), root);
  assert.equal(first.selectedContext().boardId, initial.boardId);
  assert.equal(first.selectedContext().query.compare, true);
  assert.equal(first.document.querySelector('[aria-label="Activity evidence"]'), null);
  assert.deepEqual(first.errors, []);
});

test("setup stays passive and independent boards recover their saved query without opening evidence", async t => {
  const workspace = new Workspace();
  workspace.configured = false;
  workspace.collected = false;
  const empty = await panel(workspace, workspace.open());
  t.after(empty.close);
  await waitFor(() => empty.document.body.textContent?.includes("Choose the repositories to collect") ?? false, "source setup prompt");
  assert.equal(empty.calls.some(call => ["refresh_sources", "configure_sources", "query_evidence"].includes(call.name)), false);
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
  await waitFor(() => reopened.selectedContext()?.revision === updated.revision && dailyButtons(reopened.document).length === 14, "reopened latest query");
  assert.equal(reopened.selectedContext().boardId, initial.boardId);
  assert.deepEqual(reopened.selectedContext().query, updated.query);
  assert.equal(reopened.document.querySelector('[aria-label="Activity evidence"]'), null);
  assert.equal(reopened.calls.some(call => call.name === "query_evidence"), false);
  assert.deepEqual([...empty.errors, ...first.errors, ...second.errors, ...reopened.errors], []);
});
