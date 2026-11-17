import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { AppBridge } from "@modelcontextprotocol/ext-apps/app-bridge";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import type { CallToolResult, JSONRPCMessage } from "@modelcontextprotocol/sdk/types.js";
import { JSDOM, VirtualConsole } from "jsdom";
import type { ActivityRecord, BoardQuery, BoardSnapshot, EvidenceResult, PeopleResult, PersonSummary, PullRequestRecord, RefreshProgress, SelectionContext } from "../src/shared.js";

type ContextUpdate = Parameters<NonNullable<AppBridge["onupdatemodelcontext"]>>[0];
type ChatMessage = Parameters<NonNullable<AppBridge["onmessage"]>>[0];
type ContextSupport = "structured" | "text" | "none";
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

function checkbox(document: Document, label: string): HTMLInputElement {
  const found = [...document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')].find(item => item.getAttribute("aria-label") === label);
  assert.ok(found, `Missing ${label} checkbox`);
  return found;
}

function choose(document: Document, label: string, value: string): void {
  const found = [...document.querySelectorAll<HTMLSelectElement>("select")].find(item => item.getAttribute("aria-label") === label || item.closest("label")?.textContent?.startsWith(label));
  assert.ok(found, `Missing ${label} select`);
  found.value = value;
  found.dispatchEvent(new document.defaultView!.Event("change", { bubbles: true }));
}

function humanMessage(message: ChatMessage, context: SelectionContext): string {
  assert.equal(message.role, "user");
  assert.equal(message.content.length, 1);
  const content = message.content[0];
  assert.equal(content.type, "text");
  const text = content.type === "text" ? content.text : "";
  assert.ok(text.trim().length > 0);
  assert.doesNotMatch(text, /[{}]|boardId|snapshotId|recordIds|bucket_start|person_id|query_evidence|headSha|requestedReviewers/);
  assert.ok(!text.includes(context.boardId));
  assert.ok(!text.includes(context.snapshotId));
  for (const pr of context.selectedPullRequests ?? []) assert.ok(!text.includes(pr.headSha));
  return text;
}

function review(decision: NonNullable<PullRequestRecord["review"]>["decision"], fields: Partial<NonNullable<PullRequestRecord["review"]>> = {}): NonNullable<PullRequestRecord["review"]> {
  return { decision, requestedReviewers: [], requestedTeams: [], assignees: [], reviewRequestsComplete: true, assigneesComplete: true, ...fields };
}

function prTable(document: Document): HTMLTableElement {
  const found = document.querySelector<HTMLTableElement>('table[aria-label="Pull request records"]');
  assert.ok(found, "Missing pull request table");
  return found;
}

function prRow(document: Document, id: string): HTMLTableRowElement {
  const found = [...prTable(document).querySelectorAll<HTMLButtonElement>("[data-pr-id]")].find(item => item.dataset.prId === id)?.closest("tr");
  assert.ok(found, `Missing table row ${id}`);
  return found;
}

function prCell(document: Document, id: string, column: string): HTMLTableCellElement {
  const headers = [...prTable(document).querySelectorAll<HTMLTableCellElement>('thead th[scope="col"]')];
  const index = headers.findIndex(header => header.textContent?.replace(/[↑↓↕]/g, "").trim() === column);
  assert.ok(index >= 0, `Missing ${column} column`);
  const found = prRow(document, id).cells[index];
  assert.ok(found, `Missing ${column} cell for ${id}`);
  return found;
}

function refreshProgress(git: number, github: number, status: RefreshProgress["status"] = "running", message = "Collecting repository evidence."): RefreshProgress {
  return {
    id: "synthetic-refresh", status, startedAt: new Date(END).toISOString(), updatedAt: new Date(END + (git + github) * 1000).toISOString(),
    finishedAt: status === "running" ? null : new Date(END + 10_000).toISOString(),
    git: { completedRepositories: git, totalRepositories: 2 }, github: { completedRepositories: github, totalRepositories: 2, requests: github * 2 + 1 }, message,
  };
}

function refreshSection(document: Document): HTMLElement | null {
  return document.querySelector<HTMLElement>('[aria-label="Source refresh progress"]');
}

function refreshButton(document: Document): HTMLButtonElement {
  const found = document.querySelector<HTMLButtonElement>("button.refresh-sources");
  assert.ok(found);
  return found;
}

function processed(document: Document, source: "Git" | "GitHub"): number | null {
  return document.querySelector<HTMLProgressElement>(`progress[aria-label="${source} repositories processed"]`)?.value ?? null;
}

class Workspace {
  boards = new Map<string, { revision: number; query: BoardQuery }>();
  generation = 1;
  configured = true;
  collected = true;
  account: BoardSnapshot["account"] = { id: "1", login: "sample-user" };
  refresh: RefreshProgress | null = null;
  refreshOperation: (() => Promise<void>) | null = null;
  refreshFailure: string | null = null;
  delayEvidence: ((args: Record<string, unknown>) => Promise<void>) | null = null;
  delayPeople: ((args: Record<string, unknown>) => Promise<void>) | null = null;
  completedEvidence: Array<Record<string, unknown>> = [];
  completedPeople: Array<Record<string, unknown>> = [];
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
    { id: "sample/api#2", repository: "sample/api", number: 2, title: "Improve activity loading", url: "https://github.com/sample/api/pull/2", author: "contributor-a", state: "merged", draft: false, createdAt: "2026-11-01T09:00:00Z", updatedAt: "2026-11-15T13:00:00Z", mergedAt: "2026-11-15T13:00:00Z", baseBranch: "main", headSha: "a".repeat(40), review: review("approved") },
    { id: "sample/web#9", repository: "sample/web", number: 9, title: "An older draft still open", url: "https://github.com/sample/web/pull/9", author: "contributor-b", state: "open", draft: true, createdAt: "2026-05-01T09:00:00Z", updatedAt: "2026-11-16T10:00:00Z", mergedAt: null, baseBranch: "main", headSha: "b".repeat(40), review: review("review_required", { requestedTeams: ["sample/reviewers"] }) },
    { id: "sample/api#10", repository: "sample/api", number: 10, title: "Refine API loading", url: "https://github.com/sample/api/pull/10", author: "contributor-a", state: "open", draft: false, createdAt: "2026-11-10T09:00:00Z", updatedAt: "2026-11-16T11:00:00Z", mergedAt: null, baseBranch: "main", headSha: "c".repeat(40), review: review("review_required", { requestedReviewers: ["sample-user"] }) },
    { id: "sample/web#11", repository: "sample/web", number: 11, title: "Improve keyboard navigation", url: "https://github.com/sample/web/pull/11", author: "contributor-b", state: "open", draft: false, createdAt: "2026-11-12T09:00:00Z", updatedAt: "2026-11-14T11:00:00Z", mergedAt: null, baseBranch: "main", headSha: "d".repeat(40), review: review("changes_requested") },
    { id: "sample/api#12", repository: "sample/api", number: 12, title: "Add retry coverage", url: "https://github.com/sample/api/pull/12", author: "contributor-a", state: "open", draft: false, createdAt: "2026-11-12T09:00:00Z", updatedAt: "2026-11-13T11:00:00Z", mergedAt: null, baseBranch: "main", headSha: "e".repeat(40), review: review("none", { assignees: ["sample-user"] }) },
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
      boardId, revision: saved.revision, snapshotId: `${boardId}:${saved.revision}:${this.generation}`, updatedAt: new Date(END).toISOString(), query: { ...query }, refresh: this.refresh,
      scope: { since: new Date(since).toISOString(), until: new Date(END).toISOString(), timezone: "UTC" },
      coverage: { source: "git+github", complete: false, note: this.collected ? "Some line evidence is unavailable. Git remote-tracking refs were read without fetching." : "Refresh sources to collect evidence.", repositories: this.configured ? ["sample/api", "sample/web"].map(repository => ({ repository, revision: "a".repeat(40), ref: "refs/remotes/origin/main", shallow: false, partial: true, historyComplete: true, linesComplete: false, openPRsComplete: true, mergedPRsComplete: true, errors: ["Line evidence is incomplete."] })) : [] },
      configuration: { revision: 1, repositories: this.configured ? ["sample/api", "sample/web"] : [], needsRefresh: !this.collected },
      account: this.account, collectedAt: this.collected ? new Date(END + this.generation * 1000).toISOString() : null, buckets,
      totals: { commits: current.length, additions: buckets.reduce((sum, item) => sum + item.additions, 0), deletions: buckets.reduce((sum, item) => sum + item.deletions, 0), unknownLineCommits: buckets.reduce((sum, item) => sum + item.unknownLineCommits, 0), previousCommits: query.compare ? previous.length : null, mergedPRs: prs.filter(pr => pr.mergedAt && Date.parse(pr.mergedAt) >= since && Date.parse(pr.mergedAt) < END).length, openPRs: prs.filter(pr => pr.state === "open").length },
    };
  }

  people(snapshot: BoardSnapshot, source: "git" | "github"): PersonSummary[] {
    const people = new Map<string, PersonSummary>();
    const person = (identity: string, name: string): PersonSummary => {
      const id = `${source}:${identity}`;
      if (!people.has(id)) people.set(id, { id, source, name, identity, commits: 0, additions: 0, deletions: 0, unknownLineCommits: 0, openPRs: 0, mergedPRs: 0, repositories: [] });
      return people.get(id)!;
    };
    if (!this.collected) return [];
    if (source === "git") {
      for (const record of this.records.filter(record => (snapshot.query.repository === "all" || record.repository === snapshot.query.repository) && record.timestamp >= snapshot.scope.since && record.timestamp < snapshot.scope.until)) {
        const summary = person(record.email, record.contributor);
        summary.commits += 1;
        summary.additions += record.additions ?? 0;
        summary.deletions += record.deletions ?? 0;
        summary.unknownLineCommits += record.additions === null || record.deletions === null ? 1 : 0;
        if (!summary.repositories.includes(record.repository)) summary.repositories.push(record.repository);
      }
    } else {
      for (const pr of this.prs.filter(pr => (snapshot.query.repository === "all" || pr.repository === snapshot.query.repository) && (pr.state === "open" || (pr.mergedAt! >= snapshot.scope.since && pr.mergedAt! < snapshot.scope.until)))) {
        const summary = person(pr.author, pr.author);
        if (pr.state === "open") summary.openPRs += 1;
        else summary.mergedPRs += 1;
        if (!summary.repositories.includes(pr.repository)) summary.repositories.push(pr.repository);
      }
    }
    return [...people.values()].sort((a,b) => (source === "git" ? b.commits - a.commits : b.openPRs + b.mergedPRs - a.openPRs - a.mergedPRs) || a.identity.localeCompare(b.identity));
  }

  async tool(params: { name: string; arguments?: Record<string, unknown> }): Promise<CallToolResult> {
    const args = params.arguments ?? {};
    if (params.name === "refresh_sources") {
      await this.refreshOperation?.();
      if (this.refreshFailure) return { isError: true, content: [{ type: "text", text: this.refreshFailure }] };
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
    if (args.snapshot_id !== snapshot.snapshotId) return { isError: true, content: [{ type: "text", text: "The selected snapshot changed." }] };
    if (params.name === "query_people") {
      await this.delayPeople?.(args);
      const source = args.source as "git" | "github";
      const people = this.people(snapshot, source);
      const signature = JSON.stringify([snapshot.snapshotId, source]);
      let offset = 0;
      if (args.cursor) {
        const decoded = JSON.parse(Buffer.from(String(args.cursor), "base64url").toString("utf8"));
        if (decoded[0] !== signature) return { isError: true, content: [{ type: "text", text: "The people cursor changed scope." }] };
        offset = decoded[1];
      }
      const nextOffset = offset + 3;
      const result: PeopleResult = { boardId, revision: snapshot.revision, snapshotId: snapshot.snapshotId, query: snapshot.query, scope: snapshot.scope, coverage: snapshot.coverage, source, people: people.slice(offset, nextOffset), totalPeople: people.length, nextCursor: people.length > nextOffset ? Buffer.from(JSON.stringify([signature, nextOffset])).toString("base64url") : null };
      this.completedPeople.push(args);
      return { content: [], structuredContent: { ...result } };
    }
    assert.equal(params.name, "query_evidence");
    await this.delayEvidence?.(args);
    const kind = String(args.kind ?? (args.pull_request_state || args.pull_request_id ? "pull_requests" : "all"));
    const prState = args.pull_request_state as "open" | "merged" | undefined;
    const prId = args.pull_request_id as string | undefined;
    const personId = args.person_id as string | undefined;
    const selectedPerson = personId ? [...this.people(snapshot, "git"), ...this.people(snapshot, "github")].find(person => person.id === personId) : undefined;
    if (personId && !selectedPerson) return { isError: true, content: [{ type: "text", text: "The selected person is unavailable." }] };
    if (kind === "activity" && (prState || prId)) return { isError: true, content: [{ type: "text", text: "Incompatible evidence filters." }] };
    const bucket = snapshot.buckets.find(item => item.start === args.bucket_start);
    const records = kind !== "pull_requests" && this.collected ? this.records.filter(record => (snapshot.query.repository === "all" || record.repository === snapshot.query.repository) && (!selectedPerson || selectedPerson.source === "git" && record.email === selectedPerson.identity) && record.timestamp >= (bucket?.start ?? snapshot.scope.since) && record.timestamp < (bucket?.end ?? snapshot.scope.until)).sort((a,b) => b.timestamp.localeCompare(a.timestamp) || a.id.localeCompare(b.id)) : [];
    const sort = String(args.pull_request_sort ?? "updated");
    const direction = args.sort_direction === "asc" ? 1 : -1;
    const sortValue = (pr: PullRequestRecord): string | number => sort === "created" ? pr.createdAt : sort === "updated" ? pr.updatedAt : sort === "number" ? pr.number : sort === "title" ? pr.title : sort === "author" ? pr.author : pr.repository;
    const prs = kind !== "activity" && this.collected ? this.prs.filter(pr => (snapshot.query.repository === "all" || pr.repository === snapshot.query.repository) && (!selectedPerson || selectedPerson.source === "github" && pr.author === selectedPerson.identity) && (!prState || pr.state === prState) && (!prId || pr.id === prId) && (pr.state === "open" || (pr.mergedAt! >= (bucket?.start ?? snapshot.scope.since) && pr.mergedAt! < (bucket?.end ?? snapshot.scope.until)))).sort((a,b) => {
      const av = sortValue(a), bv = sortValue(b);
      return direction * (typeof av === "number" && typeof bv === "number" ? av - bv : String(av).localeCompare(String(bv))) || a.id.localeCompare(b.id);
    }) : [];
    if (prId && !prs.length) return { isError: true, content: [{ type: "text", text: "The selected pull request is unavailable." }] };
    const signature = JSON.stringify([snapshot.snapshotId, args.bucket_start ?? null, kind, prState ?? null, prId ?? null, personId ?? null, sort, direction]);
    let offset = 0;
    if (args.cursor) {
      const decoded = JSON.parse(Buffer.from(String(args.cursor), "base64url").toString("utf8"));
      if (decoded[0] !== signature) return { isError: true, content: [{ type: "text", text: "The evidence cursor changed scope." }] };
      offset = decoded[1];
    }
    const nextOffset = offset + 3;
    const evidence: EvidenceResult = { boardId, revision: snapshot.revision, snapshotId: snapshot.snapshotId, query: snapshot.query, scope: snapshot.scope, coverage: snapshot.coverage, bucketStart: bucket?.start ?? null, kind: kind as "all" | "activity" | "pull_requests", pullRequestState: prState, selectedPullRequestId: prId, selectedPersonId: personId, records: records.slice(offset, nextOffset), pullRequests: prs.slice(offset, nextOffset), nextCursor: Math.max(records.length, prs.length) > nextOffset ? Buffer.from(JSON.stringify([signature, nextOffset])).toString("base64url") : null, totalRecords: records.length, totalPullRequests: prs.length };
    this.completedEvidence.push(args);
    return { content: [], structuredContent: { ...evidence } };
  }
}

async function panel(workspace: Workspace, initial: BoardSnapshot, contextSupport: ContextSupport = "structured", options: { waitForInitialContext?: boolean; initialResult?: Promise<void> } = {}) {
  const html = await readFile(new URL("../bundle/board.html", import.meta.url), "utf8");
  const host = new JSDOM("<!doctype html><html><body></body></html>");
  const contexts: ContextUpdate[] = [];
  const messages: ChatMessage[] = [];
  const calls: Array<{ name: string; arguments?: Record<string, unknown> }> = [];
  const errors: string[] = [];
  let guest: JSDOM | undefined;
  let closed = false;
  let initialized = false;
  let rejectContext = false;
  let rejectMessage = false;
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
  const updateModelContext = contextSupport === "structured" ? { text: {}, structuredContent: {} } : contextSupport === "text" ? { text: {} } : {};
  const bridge = new AppBridge(null, { name: "BigBoard test host", version: "1.0.0" }, { serverTools: {}, updateModelContext, message: { text: {} } }, { hostContext: { theme: "light", displayMode: "inline", locale: "en-US", timeZone: "UTC" } });
  bridge.oncalltool = async (params) => { calls.push(params); return workspace.tool(params); };
  bridge.onupdatemodelcontext = async (params) => {
    if (rejectContext) throw new Error("Context sharing unavailable");
    contexts.push(params);
    return {};
  };
  bridge.onmessage = async (params) => {
    if (rejectMessage) return { isError: true };
    messages.push(params);
    return {};
  };
  bridge.oninitialized = () => { initialized = true; void bridge.sendToolInput({ arguments: {} }).then(() => options.initialResult ? options.initialResult.then(() => bridge.sendToolResult({ content: [], structuredContent: { ...initial } })) : bridge.sendToolResult({ content: [], structuredContent: { ...initial } })).catch((error) => errors.push(String(error))); };
  await bridge.connect(transport);
  guest = new JSDOM(html, { url: "https://bigboard.test/activity", runScripts: "dangerously", pretendToBeVisual: true, virtualConsole: new VirtualConsole(), beforeParse(window) {
    Object.defineProperty(window, "parent", { value: host.window });
    Object.defineProperty(window, "structuredClone", { value: structuredClone });
    Object.defineProperty(window, "ResizeObserver", { value: class { observe() {} unobserve() {} disconnect() {} } });
    window.addEventListener("error", (event) => errors.push(String(event.error ?? event.message)));
  } });
  await waitFor(() => initialized && (options.waitForInitialContext === false ? Boolean(guest?.window.document.querySelector('nav[aria-label="Board views"]')) : contexts.length > 0 || contextSupport === "none" && Boolean(guest?.window.document.querySelector('[role="alert"]'))), "MCP Apps handshake and initial context");
  const document = guest.window.document;
  const selectedContext = (): SelectionContext => {
    const context = contexts.at(-1);
    if (context?.structuredContent) return context.structuredContent as unknown as SelectionContext;
    const text = context?.content?.filter(item => item.type === "text").map(item => item.text).join("\n");
    return text ? JSON.parse(text.slice(text.indexOf("{"))) as SelectionContext : undefined as unknown as SelectionContext;
  };
  const close = async () => {
    if (closed || !guest) return;
    await bridge.teardownResource({}, { timeout: 1000 });
    guest.window.dispatchEvent(new guest.window.Event("pagehide"));
    closed = true;
    await bridge.close();
    guest.window.close();
    host.window.close();
  };
  return { document, contexts, messages, calls, errors, selectedContext, close, sendSnapshot: (snapshot: BoardSnapshot) => bridge.sendToolResult({ content: [], structuredContent: { ...snapshot } }), rejectContext: (value: boolean) => { rejectContext = value; }, rejectMessage: (value: boolean) => { rejectMessage = value; } };
}

test("Activity stays quiet until a bucket is selected, then shares only its supporting commits", async t => {
  const workspace = new Workspace();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7, "Activity chart");
  assert.deepEqual([...first.document.querySelectorAll('nav[aria-label="Board views"] button')].map(item => item.textContent), ["Activity", "People", "Pull requests"]);
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
  assert.equal(first.messages.length, 0);
  assert.ok(first.contexts.every(context => context.content === undefined));
  button(first.document, "Explain selection").click();
  await waitFor(() => first.messages.length === 1, "Activity follow-up");
  assert.match(humanMessage(first.messages[0], first.selectedContext()), /activity/i);
  assert.equal(first.selectedContext().bucketStart, bucket.start);
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
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length === 3, "Open PR inventory");
  const refreshedSnapshot = first.selectedContext().snapshotId;
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
  assert.match(humanMessage(first.messages[0], first.selectedContext()), /pull request|PR/i);
  assert.equal(first.selectedContext().pullRequestId, "sample/web#9");
  assert.equal(first.selectedContext().snapshotId, refreshedSnapshot);
  assert.equal(first.selectedContext().bucketStart, null);
  button(first.document, "Close pull request details").click();
  await waitFor(() => first.document.querySelector('[aria-label="Pull request details"]') === null, "dismissed PR detail");
  button(first.document, "Load more pull requests").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length === 4, "Open PR next page");
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
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length === 3, "newer PR inventory");
  release();
  await waitFor(() => workspace.completedEvidence.some(args => args.kind === "activity"), "late Activity completion");
  assert.equal(first.document.querySelector('[aria-label="Activity evidence"]'), null);
  assert.equal(first.document.querySelector('[aria-label="Commit records"]'), null);
  assert.equal(first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length, 3);
  button(first.document, "Merged").click();
  await waitFor(() => first.selectedContext()?.pullRequestState === "merged" && first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length === 2, "Merged state after late reply");
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

test("People can open directly and keep Git and GitHub identities and evidence separate", async t => {
  const workspace = new Workspace();
  const initial = { ...workspace.open(), initialView: "people" as const };
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => first.document.querySelectorAll('[aria-label="People records"] [data-person-id]').length === 2, "Git contributors");
  assert.equal(first.selectedContext().view, "people");
  assert.equal(first.document.querySelector('[aria-label="Daily commits"]'), null);
  assert.equal(first.calls.some(call => call.name === "query_evidence"), false);
  const gitPerson = workspace.people(initial, "git")[0];
  button(first.document, `Select person ${gitPerson.name}: ${gitPerson.identity}`).click();
  await waitFor(() => first.document.querySelector('[aria-label="People evidence"]') !== null && first.document.querySelectorAll('[aria-label="Commit records"] article').length > 0 && first.selectedContext()?.personId === gitPerson.id, "selected Git identity");
  const gitEvidence = first.calls.find(call => call.name === "query_evidence")!.arguments!;
  assert.equal(gitEvidence.person_id, gitPerson.id);
  assert.equal(gitEvidence.bucket_start, undefined);
  assert.equal(first.selectedContext().bucketStart, null);
  assert.equal(first.selectedContext().pullRequestId, undefined);
  assert.ok(first.document.querySelector('[aria-label="Commit records"]')?.textContent?.includes(gitPerson.name));
  assert.ok(!first.document.querySelector('[aria-label="Commit records"]')?.textContent?.includes(gitPerson.name === "Contributor A" ? "Contributor B" : "Contributor A"));
  button(first.document, "Explain selection").click();
  await waitFor(() => first.messages.length === 1, "People explanation");
  assert.match(humanMessage(first.messages[0], first.selectedContext()), /person|people|contributor/i);
  assert.equal(first.selectedContext().personId, gitPerson.id);
  button(first.document, "GitHub authors").click();
  await waitFor(() => first.selectedContext()?.peopleSource === "github" && first.document.querySelectorAll('[aria-label="People records"] [data-person-id]').length === 2, "GitHub authors");
  assert.equal(first.selectedContext().personId, null);
  assert.equal(first.document.querySelector('[aria-label="People evidence"]'), null);
  const githubPerson = workspace.people(initial, "github")[0];
  button(first.document, `Select person ${githubPerson.name}: ${githubPerson.identity}`).click();
  await waitFor(() => first.document.querySelector('[aria-label="People evidence"]') !== null && first.selectedContext()?.personId === githubPerson.id && first.document.querySelector('[aria-label="Person pull requests"]') !== null, "GitHub identity evidence");
  assert.equal(first.document.querySelector('[aria-label="Commit records"]'), null);
  const prEvidence = first.calls.filter(call => call.name === "query_evidence").at(-1)!.arguments!;
  assert.equal(prEvidence.person_id, githubPerson.id);
  assert.equal(prEvidence.pull_request_state, undefined);
  assert.equal(prEvidence.bucket_start, undefined);
  const personLinks = [...first.document.querySelectorAll<HTMLAnchorElement>('[aria-label="Person pull requests"] a')];
  assert.ok(personLinks.length > 0);
  assert.ok(personLinks.every(link => workspace.prs.some(pr => pr.url === link.href && pr.author === githubPerson.identity)));
  button(first.document, "Close people evidence").click();
  await waitFor(() => first.selectedContext()?.personId === null && first.document.querySelector('[aria-label="People evidence"]') === null, "dismissed person selection");
  assert.deepEqual(first.errors, []);
});

test("PR multi-selection survives paging and sorting and sends the exact requested review set", async t => {
  const workspace = new Workspace();
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length === 3, "direct PR view");
  assert.equal(first.selectedContext().view, "pullRequests");
  assert.equal(first.document.querySelector('[aria-label="Daily commits"]'), null);
  checkbox(first.document, "Select sample/api#10").click();
  checkbox(first.document, "Select sample/web#9").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 2, "two exact PR references shared");
  assert.equal(first.messages.length, 0);
  button(first.document, "Select pull request #11: Improve keyboard navigation").click();
  await waitFor(() => first.selectedContext()?.pullRequestId === "sample/web#11", "inspected PR independent of batch");
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id).sort(), ["sample/api#10", "sample/web#9"]);
  button(first.document, "Load more pull requests").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length === 4, "next PR page");
  checkbox(first.document, "Select sample/api#12").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 3, "cross-page PR selection");
  choose(first.document, "PR sort", "number");
  await waitFor(() => first.calls.some(call => call.name === "query_evidence" && call.arguments?.pull_request_sort === "number") && first.document.querySelector<HTMLButtonElement>('[aria-label="Pull request records"] [data-pr-id]')?.dataset.prId === "sample/api#12", "global numeric sort before paging");
  choose(first.document, "Sort direction", "asc");
  await waitFor(() => first.document.querySelector<HTMLButtonElement>('[aria-label="Pull request records"] [data-pr-id]')?.dataset.prId === "sample/web#9" && first.selectedContext()?.sortDirection === "asc", "ascending sort");
  const ids = ["sample/api#10", "sample/api#12", "sample/web#9"];
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id).sort(), ids);
  assert.equal(checkbox(first.document, "Select sample/api#10").checked, true);
  assert.equal(checkbox(first.document, "Select sample/web#9").checked, true);
  assert.equal(first.document.querySelector('[aria-label="Select sample/api#12"]'), null);
  assert.equal(first.messages.length, 0);
  button(first.document, "Share selection").click();
  await waitFor(() => first.messages.length === 1, "explicit shared PR selection");
  button(first.document, "Request code reviews").click();
  await waitFor(() => first.messages.length === 2, "explicit code review request");
  humanMessage(first.messages[0], first.selectedContext());
  const message = humanMessage(first.messages[1], first.selectedContext());
  assert.match(message, /review/i);
  assert.match(message, /3/);
  for (const id of ids) {
    const pr = workspace.prs.find(pr => pr.id === id)!;
    const selected = first.selectedContext().selectedPullRequests?.find(item => item.id === id);
    assert.ok(selected);
    assert.equal(selected.repository, pr.repository);
    assert.equal(selected.number, pr.number);
    assert.equal(selected.url, pr.url);
    assert.equal(selected.headSha, pr.headSha);
  }
  assert.ok(!first.selectedContext().selectedPullRequests?.some(pr => pr.id === "sample/web#11"));
  assert.ok(first.contexts.every(context => context.content === undefined));
  button(first.document, "Clear selection").click();
  await waitFor(() => !first.selectedContext()?.selectedPullRequests?.length, "cleared batch selection");
  assert.equal(first.document.querySelector('[aria-label="Selected pull requests"]'), null);
  assert.deepEqual(first.errors, []);
});

test("PR selections clear when state, view, repository, or source snapshot changes", async t => {
  const workspace = new Workspace();
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  const select = async () => {
    await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "selectable PR");
    checkbox(first.document, "Select sample/api#10").click();
    await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 1, "selected PR batch");
  };
  await select();
  button(first.document, "Merged").click();
  await waitFor(() => first.selectedContext()?.pullRequestState === "merged" && !first.selectedContext()?.selectedPullRequests?.length, "cleared after PR state change");
  button(first.document, "Open").click();
  await select();
  button(first.document, "People").click();
  await waitFor(() => first.selectedContext()?.view === "people", "People view");
  assert.ok(!first.selectedContext().selectedPullRequests?.length);
  button(first.document, "Pull requests").click();
  await select();
  choose(first.document, "Repository", "sample/api");
  await waitFor(() => first.selectedContext()?.query.repository === "sample/api" && !first.selectedContext()?.selectedPullRequests?.length, "cleared after repository change");
  await select();
  const oldSnapshot = first.selectedContext().snapshotId;
  button(first.document, "Refresh sources").click();
  await waitFor(() => first.selectedContext()?.snapshotId !== oldSnapshot && !first.selectedContext()?.selectedPullRequests?.length, "cleared after source refresh");
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("a late People response cannot replace the newer source or leak its selection", async t => {
  const workspace = new Workspace();
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  workspace.delayPeople = args => args.source === "git" ? gate : Promise.resolve();
  const first = await panel(workspace, { ...workspace.open(), initialView: "people" });
  t.after(first.close);
  await waitFor(() => first.calls.some(call => call.name === "query_people" && call.arguments?.source === "git"), "delayed Git people query");
  button(first.document, "GitHub authors").click();
  await waitFor(() => first.document.querySelector('[aria-label="People records"]')?.textContent?.includes("contributor-a") ?? false, "new GitHub people");
  release();
  await waitFor(() => workspace.completedPeople.some(args => args.source === "git"), "late Git people completion");
  assert.equal(first.selectedContext().peopleSource, "github");
  assert.equal(first.selectedContext().personId, null);
  assert.ok(!first.document.querySelector('[aria-label="People records"]')?.textContent?.includes("@example.test"));
  assert.equal(first.calls.some(call => call.name === "query_evidence"), false);
  assert.deepEqual(first.errors, []);
});

test("late PR inventory responses cannot replace a newer sort or its batch selection", async t => {
  const workspace = new Workspace();
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  workspace.delayEvidence = args => args.pull_request_sort === "updated" ? gate : Promise.resolve();
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.calls.some(call => call.name === "query_evidence" && call.arguments?.pull_request_sort === "updated"), "delayed initial inventory");
  choose(first.document, "PR sort", "number");
  await waitFor(() => first.document.querySelector<HTMLButtonElement>('[aria-label="Pull request records"] [data-pr-id]')?.dataset.prId === "sample/api#12", "newer numeric inventory");
  checkbox(first.document, "Select sample/api#12").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.[0]?.id === "sample/api#12", "selected newer PR");
  release();
  await waitFor(() => workspace.completedEvidence.some(args => args.pull_request_sort === "updated"), "late original inventory completion");
  assert.equal(first.document.querySelector<HTMLButtonElement>('[aria-label="Pull request records"] [data-pr-id]')?.dataset.prId, "sample/api#12");
  assert.equal(checkbox(first.document, "Select sample/api#12").checked, true);
  assert.equal(first.selectedContext().pullRequestSort, "number");
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id), ["sample/api#12"]);
  assert.deepEqual(first.errors, []);
});

test("failed context updates block review requests and rejected messages remain retryable", async t => {
  const workspace = new Workspace();
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "PR inventory");
  first.rejectContext(true);
  checkbox(first.document, "Select sample/api#10").click();
  await waitFor(() => first.document.querySelector('[role="alert"]')?.textContent?.includes("could not be shared") ?? false, "context update failure");
  assert.equal(button(first.document, "Request code reviews").disabled, true);
  assert.equal(first.messages.length, 0);
  first.rejectContext(false);
  button(first.document, "Try again").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 1 && !button(first.document, "Request code reviews").disabled, "context sharing recovered");
  first.rejectMessage(true);
  button(first.document, "Request code reviews").click();
  await waitFor(() => first.document.querySelector('[role="alert"]')?.textContent?.includes("did not accept") ?? false, "message rejected");
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id), ["sample/api#10"]);
  first.rejectMessage(false);
  button(first.document, "Request code reviews").click();
  await waitFor(() => first.messages.length === 1, "review request retry accepted");
  assert.deepEqual(first.errors, []);
});

test("retry keeps a later-page People selection visible and consistent with shared context", async t => {
  const workspace = new Workspace();
  for (const name of ["C", "D", "E"]) {
    const source = workspace.records[0];
    workspace.records.push({ ...source, id: `sample/api:person-${name}`, subject: `Improve ${name} evidence`, timestamp: "2026-11-16T09:00:00Z", contributor: `Contributor ${name}`, email: `contributor-${name.toLowerCase()}@example.test` });
  }
  const initial = { ...workspace.open(), initialView: "people" as const };
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => first.document.querySelectorAll('[aria-label="People records"] [data-person-id]').length === 3, "first people page");
  button(first.document, "Load more people").click();
  await waitFor(() => first.document.querySelectorAll('[aria-label="People records"] [data-person-id]').length === 5, "later people page");
  const person = workspace.people(initial, "git").at(-1)!;
  button(first.document, `Select person ${person.name}: ${person.identity}`).click();
  await waitFor(() => first.document.querySelector('[aria-label="People evidence"]')?.textContent?.includes(person.identity) === true && first.selectedContext()?.personId === person.id, "later-page person selected");
  first.rejectMessage(true);
  button(first.document, "Explain selection").click();
  await waitFor(() => first.document.querySelector('[role="alert"]') !== null, "explanation rejection");
  first.rejectMessage(false);
  const contextUpdates = first.contexts.length;
  button(first.document, "Try again").click();
  await waitFor(() => first.contexts.length > contextUpdates && first.document.querySelector('[role="alert"]') === null && first.selectedContext()?.personId === person.id, "People retry");
  assert.ok(first.document.querySelector('[aria-label="People evidence"]')?.textContent?.includes(person.identity));
  assert.equal(button(first.document, `Select person ${person.name}: ${person.identity}`).getAttribute("aria-pressed"), "true");
  button(first.document, "Explain selection").click();
  await waitFor(() => first.messages.length === 1, "retried exact person explanation");
  humanMessage(first.messages[0], first.selectedContext());
  assert.equal(first.selectedContext().personId, person.id);
  assert.deepEqual(first.errors, []);
});

test("source refresh progress is visible while collection is pending and finishes honestly with coverage gaps", async t => {
  const workspace = new Workspace();
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  workspace.refreshOperation = async () => {
    workspace.refresh = refreshProgress(1, 0);
    await gate;
    workspace.refresh = refreshProgress(2, 1, "complete", "Source refresh completed with coverage gaps.");
  };
  const first = await panel(workspace, workspace.open());
  t.after(async () => { release(); await first.close(); });
  const originalSnapshot = first.selectedContext().snapshotId;
  refreshButton(first.document).click();
  await waitFor(() => first.calls.some(call => call.name === "refresh_sources"), "started source collection");
  assert.equal(refreshButton(first.document).disabled, true);
  await waitFor(() => processed(first.document, "Git") === 1 && processed(first.document, "GitHub") === 0, "in-flight refresh progress");
  assert.equal(refreshSection(first.document)?.getAttribute("role"), "status");
  assert.equal(first.selectedContext().snapshotId, originalSnapshot);
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  const git = first.document.querySelector<HTMLProgressElement>('progress[aria-label="Git repositories processed"]');
  const github = first.document.querySelector<HTMLProgressElement>('progress[aria-label="GitHub repositories processed"]');
  assert.equal(git?.max, 2);
  assert.equal(github?.max, 2);
  workspace.refresh = refreshProgress(2, 1);
  await waitFor(() => processed(first.document, "Git") === 2 && processed(first.document, "GitHub") === 1, "updated refresh progress before completion");
  assert.equal(refreshButton(first.document).disabled, true);
  release();
  await waitFor(() => refreshSection(first.document)?.textContent?.includes("completed with coverage gaps") === true && !refreshButton(first.document).disabled, "completed refresh with partial coverage");
  assert.equal(refreshSection(first.document)?.querySelector("progress"), null);
  assert.notEqual(first.selectedContext().snapshotId, originalSnapshot);
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  assert.deepEqual(first.errors, []);
});

test("same-snapshot refresh progress keeps PR pages and selections while external collection disables refresh", async t => {
  const workspace = new Workspace();
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "PR selection ready");
  checkbox(first.document, "Select sample/api#10").click();
  button(first.document, "Load more pull requests").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 1 && first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length === 4, "paged PR selection");
  const originalSnapshot = first.selectedContext().snapshotId;
  const evidenceCalls = first.calls.filter(call => call.name === "query_evidence").length;
  const sourceCalls = first.calls.filter(call => call.name === "refresh_sources").length;
  workspace.refresh = refreshProgress(1, 0);
  await waitFor(() => processed(first.document, "Git") === 1, "external refresh progress");
  assert.equal(refreshButton(first.document).disabled, true);
  assert.equal(refreshButton(first.document).textContent, "Collecting…");
  workspace.refresh = refreshProgress(2, 1);
  await waitFor(() => processed(first.document, "GitHub") === 1, "external progress update");
  assert.equal(first.selectedContext().snapshotId, originalSnapshot);
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id), ["sample/api#10"]);
  assert.equal(checkbox(first.document, "Select sample/api#10").checked, true);
  assert.equal(first.document.querySelectorAll('[aria-label="Pull request records"] [data-pr-id]').length, 4);
  assert.equal(first.calls.filter(call => call.name === "query_evidence").length, evidenceCalls);
  workspace.refresh = refreshProgress(2, 1, "cancelled", "Source refresh was cancelled. Previous Git evidence remains available.");
  await waitFor(() => refreshSection(first.document)?.textContent?.includes("was cancelled") === true && !refreshButton(first.document).disabled, "cancelled refresh progress");
  assert.equal(refreshSection(first.document)?.querySelector("progress"), null);
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id), ["sample/api#10"]);
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, sourceCalls);
  assert.equal(first.calls.filter(call => call.name === "query_evidence").length, evidenceCalls);
  assert.deepEqual(first.errors, []);
});

test("a failed refresh keeps its terminal progress and allows another explicit attempt", async t => {
  const workspace = new Workspace();
  workspace.refresh = refreshProgress(1, 0, "failed", "Source refresh stopped before all repositories were processed.");
  const first = await panel(workspace, workspace.open());
  t.after(first.close);
  await waitFor(() => refreshSection(first.document)?.textContent?.includes("stopped before all repositories") === true, "failed progress on opening");
  assert.equal(refreshSection(first.document)?.querySelector("progress"), null);
  assert.equal(refreshButton(first.document).disabled, false);
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 0);
  const originalSnapshot = first.selectedContext().snapshotId;
  workspace.refreshFailure = "Source refresh failed. Try again.";
  workspace.refreshOperation = async () => { workspace.refresh = refreshProgress(1, 0, "failed", "Source refresh failed. Previous Git evidence remains available."); };
  refreshButton(first.document).click();
  await waitFor(() => first.document.querySelector('[role="alert"]')?.textContent?.includes("Source refresh failed") === true && refreshSection(first.document)?.textContent?.includes("Previous Git evidence remains available") === true && !refreshButton(first.document).disabled, "failed refresh completion");
  assert.equal(first.selectedContext().snapshotId, originalSnapshot);
  assert.equal(dailyButtons(first.document).length, 7);
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  workspace.refreshFailure = null;
  workspace.refreshOperation = async () => { workspace.refresh = refreshProgress(2, 2, "complete", "Source refresh completed."); };
  refreshButton(first.document).click();
  await waitFor(() => first.selectedContext().snapshotId !== originalSnapshot && refreshSection(first.document)?.textContent?.includes("Source refresh completed.") === true, "explicit retry after failure");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 2);
  assert.deepEqual(first.errors, []);
});

test("equal-timestamp progress cannot regress repository or request counters", async t => {
  const workspace = new Workspace();
  workspace.refresh = refreshProgress(2, 1, "running", "Current refresh progress.");
  const first = await panel(workspace, workspace.open());
  t.after(first.close);
  await waitFor(() => processed(first.document, "Git") === 2 && processed(first.document, "GitHub") === 1, "current refresh counters");
  const current = workspace.refresh;
  const delayed = [
    { ...current, git: { ...current.git, completedRepositories: 1 } },
    { ...current, github: { ...current.github, completedRepositories: 0 } },
    { ...current, github: { ...current.github, requests: 2 } },
  ];
  for (const progress of delayed) {
    workspace.refresh = { ...progress, message: "Delayed refresh progress." };
    await first.sendSnapshot(workspace.snapshot(first.selectedContext().boardId));
    await new Promise(resolve => setTimeout(resolve, 40));
    assert.equal(processed(first.document, "Git"), 2);
    assert.equal(processed(first.document, "GitHub"), 1);
    assert.ok(refreshSection(first.document)?.textContent?.includes("Current refresh progress."));
    assert.ok(!refreshSection(first.document)?.textContent?.includes("Delayed refresh progress."));
  }
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 0);
  assert.deepEqual(first.errors, []);
});

test("a delayed pre-refresh board read cannot replace the completed snapshot or a new PR selection", async t => {
  const workspace = new Workspace();
  const originalTool = workspace.tool.bind(workspace);
  let holdNextRead = false;
  let capturedOldRead = false;
  let returnedOldRead = false;
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  workspace.tool = async params => {
    const result = await originalTool(params);
    if (params.name === "read_board" && holdNextRead) {
      holdNextRead = false;
      capturedOldRead = true;
      await gate;
      returnedOldRead = true;
    }
    return result;
  };
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(async () => { release(); await first.close(); });
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "initial PR inventory");
  const originalSnapshot = first.selectedContext().snapshotId;
  holdNextRead = true;
  await waitFor(() => capturedOldRead, "held pre-refresh poll response");
  workspace.refreshOperation = async () => { workspace.refresh = refreshProgress(2, 2, "complete", "Source refresh completed."); };
  refreshButton(first.document).click();
  await waitFor(() => first.selectedContext().snapshotId !== originalSnapshot && refreshSection(first.document)?.textContent?.includes("Source refresh completed.") === true && first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "new collection and PR inventory");
  checkbox(first.document, "Select sample/api#10").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 1, "new-snapshot PR selection");
  const completedSnapshot = first.selectedContext().snapshotId;
  const evidenceCalls = first.calls.filter(call => call.name === "query_evidence").length;
  release();
  await waitFor(() => returnedOldRead, "delayed old response delivered");
  await new Promise(resolve => setTimeout(resolve, 40));
  assert.equal(first.selectedContext().snapshotId, completedSnapshot);
  assert.ok(refreshSection(first.document)?.textContent?.includes("Source refresh completed."));
  assert.equal(refreshButton(first.document).disabled, false);
  assert.equal(checkbox(first.document, "Select sample/api#10").checked, true);
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id), ["sample/api#10"]);
  assert.equal(first.calls.filter(call => call.name === "query_evidence").length, evidenceCalls);
  assert.deepEqual(first.errors, []);
});

test("Pull requests renders a semantic table with review decisions, readiness, drafts, and merged state", async t => {
  const workspace = new Workspace();
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelectorAll('table[aria-label="Pull request records"] tbody tr').length === 3, "PR table first page");
  const table = prTable(first.document);
  assert.deepEqual([...table.querySelectorAll('thead th[scope="col"]')].map(header => header.textContent?.replace(/[↑↓↕]/g, "").trim()), ["Select", "PR", "Author", "State", "Review", "Needs review", "Yours", "Updated"]);
  for (const label of ["Sort by PR", "Sort by Author", "Sort by Updated"]) assert.ok(button(first.document, label));
  assert.ok(prCell(first.document, "sample/api#10", "PR").textContent?.includes("sample/api"));
  assert.equal(prCell(first.document, "sample/api#10", "Author").textContent?.trim(), "contributor-a");
  assert.ok(prCell(first.document, "sample/api#10", "Review").textContent?.includes("Review required"));
  assert.equal(prCell(first.document, "sample/api#10", "Needs review").textContent?.trim(), "Yes");
  assert.ok(prCell(first.document, "sample/web#9", "State").textContent?.includes("Draft"));
  assert.equal(prCell(first.document, "sample/web#9", "Needs review").textContent?.trim(), "Draft");
  assert.ok(prCell(first.document, "sample/web#11", "Review").textContent?.includes("Changes requested"));
  assert.equal(prCell(first.document, "sample/web#11", "Needs review").textContent?.trim(), "Author action");
  button(first.document, "Load more pull requests").click();
  await waitFor(() => prTable(first.document).tBodies[0].rows.length === 4, "PR table next page");
  assert.ok(prCell(first.document, "sample/api#12", "Review").textContent?.includes("No decision"));
  assert.equal(prCell(first.document, "sample/api#12", "Needs review").textContent?.trim(), "No request");
  button(first.document, "Merged").click();
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#2"]') !== null, "merged PR table");
  assert.ok(prCell(first.document, "sample/api#2", "Review").textContent?.includes("Approved"));
  assert.ok(prCell(first.document, "sample/api#2", "State").textContent?.includes("Merged"));
  assert.equal(prCell(first.document, "sample/api#2", "Needs review").textContent?.trim(), "—");
  assert.ok(prCell(first.document, "sample/web#13", "Review").textContent?.includes("Unknown"));
  assert.deepEqual(first.errors, []);
});

test("PR ownership distinguishes authorship, assignment, and direct review requests without inferring team membership", async t => {
  const workspace = new Workspace();
  workspace.prs.find(pr => pr.id === "sample/api#10")!.review = review("review_required", { requestedReviewers: ["SAMPLE-USER"], requestedTeams: ["sample/reviewers"] });
  workspace.prs.find(pr => pr.id === "sample/web#11")!.author = "SAMPLE-USER";
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#10"]') !== null, "owned PR table");
  assert.ok(prCell(first.document, "sample/api#10", "Yours").textContent?.includes("Your review requested"));
  assert.ok(!prCell(first.document, "sample/api#10", "Yours").textContent?.includes("You authored"));
  assert.ok(prCell(first.document, "sample/web#11", "Yours").textContent?.includes("You authored"));
  assert.equal(prCell(first.document, "sample/web#9", "Yours").textContent?.trim(), "—");
  button(first.document, "Load more pull requests").click();
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#12"]') !== null, "assigned PR on next page");
  assert.ok(prCell(first.document, "sample/api#12", "Yours").textContent?.includes("Assigned to you"));
  button(first.document, "Select pull request #10: Refine API loading").click();
  await waitFor(() => first.document.querySelector('[aria-label="Pull request details"]') !== null, "review metadata inspector");
  const details = first.document.querySelector('[aria-label="Pull request details"]')!;
  assert.ok(details.textContent?.includes("SAMPLE-USER"));
  assert.ok(details.textContent?.includes("sample/reviewers"));
  assert.ok(details.textContent?.includes("Review required"));
  assert.deepEqual(first.errors, []);
});

test("legacy and incomplete PR review data stay unknown and viewer highlighting requires an authenticated identity", async t => {
  const workspace = new Workspace();
  workspace.prs.find(pr => pr.id === "sample/api#10")!.review = undefined;
  workspace.prs.find(pr => pr.id === "sample/web#11")!.review = review("unknown", { reviewRequestsComplete: false, assigneesComplete: false });
  workspace.prs.find(pr => pr.id === "sample/api#12")!.review = review("none", { reviewRequestsComplete: false, assigneesComplete: false });
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#10"]') !== null, "legacy review table");
  for (const id of ["sample/api#10", "sample/web#11"]) {
    assert.ok(prCell(first.document, id, "Review").textContent?.includes("Unknown"));
    assert.equal(prCell(first.document, id, "Needs review").textContent?.trim(), "Unknown");
    assert.equal(prCell(first.document, id, "Yours").textContent?.trim(), "Unknown");
  }
  button(first.document, "Load more pull requests").click();
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#12"]') !== null, "incomplete reviewer list");
  assert.equal(prCell(first.document, "sample/api#12", "Needs review").textContent?.trim(), "Unknown");
  assert.equal(prCell(first.document, "sample/api#12", "Yours").textContent?.trim(), "Unknown");
  const unauthenticated = new Workspace();
  unauthenticated.account = null;
  unauthenticated.prs.find(pr => pr.id === "sample/api#10")!.author = "sample-user";
  const second = await panel(unauthenticated, { ...unauthenticated.open(), initialView: "pullRequests" });
  t.after(second.close);
  await waitFor(() => second.document.querySelector('[data-pr-id="sample/api#10"]') !== null, "unknown viewer table");
  assert.equal(prCell(second.document, "sample/api#10", "Yours").textContent?.trim(), "Unknown");
  assert.ok(!prTable(second.document).textContent?.includes("You authored"));
  assert.ok(!prTable(second.document).textContent?.includes("Your review requested"));
  assert.deepEqual([...first.errors, ...second.errors], []);
});

test("table header sorting covers the inventory while selected PR review metadata stays exact", async t => {
  const workspace = new Workspace();
  workspace.prs.find(pr => pr.id === "sample/api#12")!.author = "zz-contributor";
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#10"]') !== null, "review selection table");
  checkbox(first.document, "Select sample/api#10").click();
  button(first.document, "Load more pull requests").click();
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#12"]') !== null, "later selectable review metadata");
  checkbox(first.document, "Select sample/api#12").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 2, "full review records shared");
  const selected = workspace.prs.filter(pr => ["sample/api#10", "sample/api#12"].includes(pr.id));
  for (const pr of selected) assert.deepEqual(first.selectedContext().selectedPullRequests?.find(item => item.id === pr.id)?.review, pr.review);
  button(first.document, "Sort by Author").click();
  await waitFor(() => first.selectedContext()?.pullRequestSort === "author", "header author sort");
  choose(first.document, "Sort direction", "desc");
  await waitFor(() => first.document.querySelector<HTMLButtonElement>('table[aria-label="Pull request records"] [data-pr-id]')?.dataset.prId === "sample/api#12", "global header sort pulls later row into first page");
  assert.ok(first.calls.some(call => call.name === "query_evidence" && call.arguments?.pull_request_sort === "author" && call.arguments?.cursor === undefined));
  assert.equal(button(first.document, "Sort by Author").closest("th")?.getAttribute("aria-sort"), "descending");
  assert.deepEqual(first.selectedContext().selectedPullRequests?.map(pr => pr.id).sort(), selected.map(pr => pr.id).sort());
  button(first.document, "Request code reviews").click();
  await waitFor(() => first.messages.length === 1, "review request from table selection");
  assert.match(humanMessage(first.messages[0], first.selectedContext()), /review/i);
  for (const pr of selected) assert.deepEqual(first.selectedContext().selectedPullRequests?.find(item => item.id === pr.id), pr);
  assert.ok(!first.selectedContext().selectedPullRequests?.some(pr => pr.id === "sample/web#11"));
  assert.deepEqual(first.errors, []);
});

test("ordinary navigation updates structured background context without creating chat messages", async t => {
  const workspace = new Workspace();
  const initial = workspace.open();
  const first = await panel(workspace, initial);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7, "initial Activity navigation");
  const bucket = initial.buckets.find(item => item.commits > 0)!;
  dailyButtons(first.document)[initial.buckets.indexOf(bucket)].click();
  await waitFor(() => first.selectedContext()?.bucketStart === bucket.start, "background Activity selection");
  button(first.document, "People").click();
  const person = workspace.people(initial, "git")[0];
  await waitFor(() => first.document.querySelector(`[data-person-id="${person.id}"]`) !== null, "People navigation");
  button(first.document, `Select person ${person.name}: ${person.identity}`).click();
  await waitFor(() => first.selectedContext()?.personId === person.id, "background person selection");
  button(first.document, "Pull requests").click();
  await waitFor(() => first.document.querySelector('[data-pr-id="sample/api#10"]') !== null, "PR navigation");
  button(first.document, "Select pull request #9: An older draft still open").click();
  await waitFor(() => first.selectedContext()?.pullRequestId === "sample/web#9", "background PR inspection");
  checkbox(first.document, "Select sample/api#10").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 1, "background PR checkbox selection");
  button(first.document, "Sort by Author").click();
  await waitFor(() => first.selectedContext()?.pullRequestSort === "author", "background sort selection");
  choose(first.document, "Repository", "sample/api");
  await waitFor(() => first.selectedContext()?.query.repository === "sample/api", "background scope change");
  assert.equal(first.messages.length, 0);
  assert.ok(first.contexts.length >= 7);
  assert.ok(first.contexts.every(context => context.content === undefined && context.structuredContent !== undefined));
  assert.equal(first.selectedContext().boardId, initial.boardId);
  assert.deepEqual(first.errors, []);
});

test("text-only context hosts receive assistant-only background data and human action messages", async t => {
  const workspace = new Workspace();
  const initial = { ...workspace.open(), initialView: "pullRequests" as const };
  const first = await panel(workspace, initial, "text");
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "text-only host PR inventory");
  checkbox(first.document, "Select sample/api#10").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 1, "assistant-only selection fallback");
  assert.equal(first.messages.length, 0);
  for (const context of first.contexts) {
    assert.equal(context.structuredContent, undefined);
    assert.ok(context.content?.length);
    for (const content of context.content ?? []) {
      assert.equal(content.type, "text");
      assert.deepEqual(content.annotations?.audience, ["assistant"]);
    }
  }
  const pr = workspace.prs.find(item => item.id === "sample/api#10")!;
  assert.equal(first.selectedContext().snapshotId, workspace.snapshot(initial.boardId).snapshotId);
  assert.deepEqual(first.selectedContext().selectedPullRequests, [pr]);
  button(first.document, "Request code reviews").click();
  await waitFor(() => first.messages.length === 1, "text-only host review action");
  assert.match(humanMessage(first.messages[0], first.selectedContext()), /review/i);
  assert.deepEqual(first.errors, []);
});

test("unsupported background context never falls back to posting selection data in chat", async t => {
  const workspace = new Workspace();
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" }, "none");
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "PR inventory without context support");
  checkbox(first.document, "Select sample/api#10").click();
  await waitFor(() => first.document.querySelector('[aria-label="Selected pull requests"]') !== null && first.document.querySelector('[role="alert"]')?.textContent?.includes("could not be shared") === true, "unavailable context reported");
  assert.equal(button(first.document, "Request code reviews").disabled, true);
  assert.equal(button(first.document, "Share selection").disabled, true);
  button(first.document, "Request code reviews").click();
  assert.equal(first.contexts.length, 0);
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("activating PRs refreshes source data with progress and coalesces re-entry during collection", async t => {
  const workspace = new Workspace();
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let attempts = 0;
  workspace.refreshOperation = async () => {
    attempts += 1;
    workspace.refresh = { ...refreshProgress(1, 0), id: `activation-${attempts}` };
    await gate;
    workspace.prs.find(pr => pr.id === "sample/api#10")!.title = `Source refresh ${attempts}`;
    workspace.refresh = { ...refreshProgress(2, 2, "complete", "Source refresh completed."), id: `activation-${attempts}` };
  };
  const first = await panel(workspace, workspace.open());
  t.after(async () => { release(); await first.close(); });
  const initialSnapshot = first.selectedContext().snapshotId;
  assert.equal(first.calls.some(call => call.name === "refresh_sources"), false);
  button(first.document, "Pull requests").click();
  await waitFor(() => attempts === 1 && first.calls.filter(call => call.name === "refresh_sources").length === 1, "automatic PR collection");
  await waitFor(() => processed(first.document, "Git") === 1, "automatic refresh progress");
  assert.equal(refreshButton(first.document).disabled, true);
  button(first.document, "Pull requests").click();
  button(first.document, "People").click();
  button(first.document, "Pull requests").click();
  await new Promise(resolve => setTimeout(resolve, 40));
  assert.equal(attempts, 1);
  release();
  await waitFor(() => first.document.querySelector('[aria-label="Pull request records"]')?.textContent?.includes("Source refresh 1") === true && !refreshButton(first.document).disabled, "fresh PR inventory after activation");
  assert.notEqual(first.selectedContext().snapshotId, initialSnapshot);
  assert.equal(first.messages.length, 0);
  await new Promise(resolve => setTimeout(resolve, 2100));
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  button(first.document, "Activity").click();
  button(first.document, "Pull requests").click();
  await waitFor(() => attempts === 2 && first.document.querySelector('[aria-label="Pull request records"]')?.textContent?.includes("Source refresh 2") === true, "new refresh on later PR activation");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 2);
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("opening directly into PRs collects once while rows, filters, sorting, and polls remain read-only", async t => {
  const workspace = new Workspace();
  workspace.collected = false;
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null && !refreshButton(first.document).disabled, "direct PR opening collected sources");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  checkbox(first.document, "Select sample/api#10").click();
  button(first.document, "Select pull request #9: An older draft still open").click();
  await waitFor(() => first.selectedContext()?.selectedPullRequests?.length === 1 && first.selectedContext()?.pullRequestId === "sample/web#9", "PR interaction after auto collection");
  button(first.document, "Sort by Author").click();
  await waitFor(() => first.selectedContext()?.pullRequestSort === "author", "PR sort after auto collection");
  choose(first.document, "Repository", "sample/api");
  await waitFor(() => first.selectedContext()?.query.repository === "sample/api" && !refreshButton(first.document).disabled, "PR filter after auto collection");
  button(first.document, "Merged").click();
  await waitFor(() => first.selectedContext()?.pullRequestState === "merged", "merged inventory");
  button(first.document, "Open").click();
  await waitFor(() => first.selectedContext()?.pullRequestState === "open" && first.document.querySelector('[aria-label="Select sample/api#10"]') !== null, "open inventory");
  await new Promise(resolve => setTimeout(resolve, 2100));
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("a PR activation reuses an external refresh without queuing another after it finishes", async t => {
  const workspace = new Workspace();
  workspace.refresh = refreshProgress(1, 0);
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => processed(first.document, "Git") === 1, "external source refresh on PR opening");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 0);
  button(first.document, "People").click();
  button(first.document, "Pull requests").click();
  await new Promise(resolve => setTimeout(resolve, 40));
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 0);
  workspace.refresh = refreshProgress(2, 2, "complete", "External source refresh completed.");
  await waitFor(() => refreshSection(first.document)?.textContent?.includes("External source refresh completed.") === true && !refreshButton(first.document).disabled, "external refresh completion");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 0);
  button(first.document, "Activity").click();
  button(first.document, "Pull requests").click();
  await waitFor(() => first.calls.filter(call => call.name === "refresh_sources").length === 1, "new activation after external completion");
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("PR activation without configured repositories stays passive", async t => {
  const workspace = new Workspace();
  workspace.configured = false;
  workspace.collected = false;
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[aria-label="Configure repositories"]') !== null, "PR setup view");
  assert.match(first.document.querySelector('[aria-label="Configure repositories"]')?.textContent ?? "", /bigboard:configure/);
  assert.equal(first.document.querySelector("button.configure-sources"), null);
  button(first.document, "People").click();
  button(first.document, "Pull requests").click();
  await waitFor(() => first.selectedContext()?.view === "pullRequests", "unconfigured PR reactivation");
  assert.equal(first.calls.some(call => ["refresh_sources", "configure_sources"].includes(call.name)), false);
  workspace.configured = true;
  workspace.generation += 1;
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null && !refreshButton(first.document).disabled, "first configured PR collection");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("failed automatic PR refreshes stop once and allow manual or later activation retry", async t => {
  const workspace = new Workspace();
  workspace.refreshFailure = "Source refresh failed. Try again.";
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" });
  t.after(first.close);
  await waitFor(() => first.document.querySelector('[role="alert"]')?.textContent?.includes("Source refresh failed") === true && !refreshButton(first.document).disabled, "automatic source failure");
  await new Promise(resolve => setTimeout(resolve, 2100));
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  workspace.refreshFailure = null;
  const failedSnapshot = first.selectedContext().snapshotId;
  refreshButton(first.document).click();
  await waitFor(() => first.selectedContext()?.snapshotId !== failedSnapshot && first.document.querySelector('[aria-label="Select sample/api#10"]') !== null && !refreshButton(first.document).disabled, "manual retry after automatic failure");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 2);
  button(first.document, "Activity").click();
  button(first.document, "Pull requests").click();
  await waitFor(() => first.calls.filter(call => call.name === "refresh_sources").length === 3 && !refreshButton(first.document).disabled, "later automatic retry");
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("choosing Activity before the initial PR snapshot arrives cancels automatic collection", async t => {
  const workspace = new Workspace();
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const first = await panel(workspace, { ...workspace.open(), initialView: "pullRequests" }, "structured", { waitForInitialContext: false, initialResult: gate });
  t.after(async () => { release(); await first.close(); });
  button(first.document, "Activity").click();
  release();
  await waitFor(() => first.selectedContext()?.view === "activity" && dailyButtons(first.document).length === 7, "Activity selected during PR startup");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 0);
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("entering PRs during a viewing-filter update waits for the latest scope before refreshing", async t => {
  const workspace = new Workspace();
  const originalTool = workspace.tool.bind(workspace);
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let updating = false;
  workspace.tool = async params => {
    const result = await originalTool(params);
    if (params.name === "update_board") {
      updating = true;
      await gate;
    }
    return result;
  };
  const first = await panel(workspace, workspace.open());
  t.after(async () => { release(); await first.close(); });
  choose(first.document, "Repository", "sample/api");
  await waitFor(() => updating, "pending viewing-filter update");
  button(first.document, "Pull requests").click();
  await waitFor(() => first.selectedContext()?.view === "pullRequests", "PR view selected during update");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 0);
  assert.equal(first.calls.filter(call => call.name === "query_evidence").length, 0);
  release();
  await waitFor(() => first.document.querySelector('[aria-label="Select sample/api#10"]') !== null && !refreshButton(first.document).disabled, "PR collection after filter update");
  assert.equal(first.selectedContext().query.repository, "sample/api");
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  assert.ok(!prTable(first.document).textContent?.includes("sample/web"));
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});

test("a newer poll superseding the refresh completion read delays PR evidence until the fresh snapshot arrives", async t => {
  const workspace = new Workspace();
  const originalTool = workspace.tool.bind(workspace);
  let releaseFinal!: () => void;
  let releasePoll!: () => void;
  const finalGate = new Promise<void>(resolve => { releaseFinal = resolve; });
  const pollGate = new Promise<void>(resolve => { releasePoll = resolve; });
  let holdReads = false;
  let heldReads = 0;
  let finalReturned = false;
  workspace.refreshOperation = async () => {
    workspace.prs.find(pr => pr.id === "sample/api#10")!.title = "Current source snapshot";
    workspace.refresh = refreshProgress(2, 2, "complete", "Source refresh completed.");
    holdReads = true;
  };
  workspace.tool = async params => {
    const result = await originalTool(params);
    if (params.name === "read_board" && holdReads) {
      heldReads += 1;
      if (heldReads === 1) {
        await finalGate;
        finalReturned = true;
      } else if (heldReads === 2) await pollGate;
    }
    return result;
  };
  const first = await panel(workspace, workspace.open());
  t.after(async () => { releaseFinal(); releasePoll(); await first.close(); });
  const oldSnapshot = first.selectedContext().snapshotId;
  button(first.document, "Pull requests").click();
  await waitFor(() => heldReads === 1, "held refresh completion read");
  await waitFor(() => heldReads === 2, "newer poll overtook the completion read");
  releaseFinal();
  await waitFor(() => finalReturned && !refreshButton(first.document).disabled, "older completion read discarded");
  await new Promise(resolve => setTimeout(resolve, 40));
  assert.equal(first.calls.filter(call => call.name === "query_evidence").length, 0);
  assert.equal(first.document.querySelector('[role="alert"]'), null);
  releasePoll();
  await waitFor(() => first.selectedContext()?.snapshotId !== oldSnapshot && first.document.querySelector('[aria-label="Pull request records"]')?.textContent?.includes("Current source snapshot") === true, "fresh PR evidence after pending poll");
  const evidence = first.calls.filter(call => call.name === "query_evidence");
  assert.equal(evidence.length, 1);
  assert.equal(evidence[0].arguments?.snapshot_id, first.selectedContext().snapshotId);
  assert.equal(first.calls.filter(call => call.name === "refresh_sources").length, 1);
  assert.equal(first.messages.length, 0);
  assert.deepEqual(first.errors, []);
});
