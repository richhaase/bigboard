import assert from "node:assert/strict";
import { cp, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import { AppBridge } from "@modelcontextprotocol/ext-apps/app-bridge";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { getDefaultEnvironment, StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import type { CallToolResult, JSONRPCMessage } from "@modelcontextprotocol/sdk/types.js";
import { JSDOM, VirtualConsole } from "jsdom";
import type { BoardSnapshot, SelectionContext } from "../src/shared.js";

type ContextUpdate = Parameters<NonNullable<AppBridge["onupdatemodelcontext"]>>[0];
type ChatMessage = Parameters<NonNullable<AppBridge["onmessage"]>>[0];

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

async function server(t: TestContext) {
  const directory = await mkdtemp(join(tmpdir(), "bigboard-app-host-"));
  const installed = join(directory, "installed");
  await cp(fileURLToPath(new URL("../bundle/", import.meta.url)), installed, { recursive: true });
  const client = new Client({ name: "bigboard-app-host-test", version: "0.1.0" });
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [join(installed, "server.cjs")],
    cwd: installed,
    env: { ...getDefaultEnvironment(), BIGBOARD_STATE_DIR: join(directory, "state") },
    stderr: "pipe",
  });
  t.after(async () => {
    await client.close();
    await rm(directory, { recursive: true, force: true });
  });
  await client.connect(transport);
  return { client, html: await readFile(join(installed, "board.html"), "utf8") };
}

async function panel(client: Client, html: string, initial: CallToolResult) {
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
        guest.window.dispatchEvent(new guest.window.MessageEvent("message", {
          data: structuredClone(message),
          source: host.window as unknown as MessageEventSource,
        }));
      });
    },
  };
  host.window.postMessage = ((message: unknown) => {
    queueMicrotask(() => {
      if (!closed) transport.onmessage?.(structuredClone(message) as JSONRPCMessage);
    });
  }) as typeof host.window.postMessage;
  const bridge = new AppBridge(null, { name: "BigBoard test host", version: "0.1.0" }, {
    serverTools: {},
    updateModelContext: { text: {}, structuredContent: {} },
    message: { text: {} },
  }, { hostContext: { theme: "light", displayMode: "inline", locale: "en-US", timeZone: "UTC" } });
  bridge.oncalltool = async (params) => {
    calls.push(params);
    return await client.callTool(params) as CallToolResult;
  };
  bridge.onupdatemodelcontext = async (params) => {
    contexts.push(params);
    return {};
  };
  bridge.onmessage = async (params) => {
    messages.push(params);
    return {};
  };
  bridge.oninitialized = () => {
    initialized = true;
    void bridge.sendToolInput({ arguments: {} })
      .then(() => bridge.sendToolResult(initial))
      .catch((error) => errors.push(String(error)));
  };
  await bridge.connect(transport);
  guest = new JSDOM(html, {
    url: "https://bigboard.test/activity",
    runScripts: "dangerously",
    pretendToBeVisual: true,
    virtualConsole: new VirtualConsole(),
    beforeParse(window) {
      Object.defineProperty(window, "parent", { value: host.window });
      Object.defineProperty(window, "structuredClone", { value: structuredClone });
      Object.defineProperty(window, "ResizeObserver", {
        value: class {
          observe() {}
          unobserve() {}
          disconnect() {}
        },
      });
      window.addEventListener("error", (event) => errors.push(String(event.error ?? event.message)));
    },
  });
  await waitFor(() => initialized && contexts.length > 0, `the MCP Apps handshake and initial context (${errors.join("; ")})`);
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

test("the rendered MCP App shares exact selections and observes agent updates without replacing its panel", async (t) => {
  const { client, html } = await server(t);
  const opened = await client.callTool({ name: "open_board", arguments: {} }) as CallToolResult;
  const snapshot = opened.structuredContent as unknown as BoardSnapshot;
  const first = await panel(client, html, opened);
  t.after(first.close);
  await waitFor(() => dailyButtons(first.document).length === 7, "daily activity controls");
  const bucket = snapshot.buckets.find((value) => value.commits > 0);
  assert.ok(bucket);
  const date = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(bucket.start));
  const bar = dailyButtons(first.document).find((button) => button.getAttribute("aria-label")?.startsWith(`${date}:`));
  assert.ok(bar);
  bar.click();
  await waitFor(() => first.selectedContext()?.bucketStart === bucket.start, "selected bucket model context");
  const selection = first.selectedContext();
  assert.equal(selection.boardId, snapshot.boardId);
  assert.equal(selection.snapshotId, snapshot.snapshotId);
  assert.equal(selection.revision, snapshot.revision);
  assert.deepEqual(selection.recordIds, bucket.recordIds);
  assert.deepEqual(selection.scope, snapshot.scope);
  await waitFor(() => first.calls.some((call) => call.name === "query_evidence" && call.arguments?.bucket_start === bucket.start), "selected evidence request");
  const explain = [...first.document.querySelectorAll<HTMLButtonElement>("button")]
    .find((button) => button.textContent?.includes("Explain selection"));
  assert.ok(explain);
  await waitFor(() => !explain.disabled, "explain action to become available");
  explain.click();
  await waitFor(() => first.messages.length === 1, "explicit follow-up message");
  assert.equal(first.messages[0].role, "user");
  const message = JSON.stringify(first.messages[0].content);
  assert.ok(message.includes(snapshot.boardId));
  assert.ok(message.includes(snapshot.snapshotId));
  assert.ok(message.includes(bucket.start));

  const rootElement = first.document.getElementById("root");
  const updated = await client.callTool({
    name: "update_board",
    arguments: { board_id: snapshot.boardId, expected_revision: snapshot.revision, repository: "sample/api", compare: true },
  }) as CallToolResult;
  const updatedSnapshot = updated.structuredContent as unknown as BoardSnapshot;
  await waitFor(() => first.selectedContext()?.revision === updatedSnapshot.revision &&
    first.document.querySelector("select")?.value === "sample/api", "the existing panel to poll the agent update");
  assert.equal(first.document.getElementById("root"), rootElement);
  assert.equal(first.selectedContext().query.repository, "sample/api");
  assert.equal(first.selectedContext().query.compare, true);
  assert.ok(first.calls.some((call) => call.name === "read_board" && call.arguments?.board_id === snapshot.boardId));
  assert.equal(first.document.querySelector<HTMLInputElement>('input[type="checkbox"]')?.checked, true);
  assert.deepEqual(first.errors, []);
});

test("two rendered boards keep independent scopes and teardown/reopen recovers saved state", async (t) => {
  const { client, html } = await server(t);
  const opened = await client.callTool({ name: "open_board", arguments: {} }) as CallToolResult;
  const otherOpened = await client.callTool({ name: "open_board", arguments: {} }) as CallToolResult;
  const snapshot = opened.structuredContent as unknown as BoardSnapshot;
  const otherSnapshot = otherOpened.structuredContent as unknown as BoardSnapshot;
  const first = await panel(client, html, opened);
  const second = await panel(client, html, otherOpened);
  t.after(first.close);
  t.after(second.close);
  await client.callTool({
    name: "update_board",
    arguments: { board_id: snapshot.boardId, expected_revision: snapshot.revision, repository: "sample/web", days: 14 },
  });
  await waitFor(() => first.selectedContext()?.query.repository === "sample/web", "first board's changed scope");
  assert.equal(second.selectedContext().boardId, otherSnapshot.boardId);
  assert.equal(second.selectedContext().revision, otherSnapshot.revision);
  assert.deepEqual(second.selectedContext().query, otherSnapshot.query);
  const updatedContext = first.selectedContext();
  await first.close();
  const replacement = await panel(client, html, opened);
  t.after(replacement.close);
  await waitFor(() => replacement.selectedContext()?.revision === updatedContext.revision, "reopened panel to recover the latest saved revision");
  assert.equal(replacement.selectedContext().boardId, updatedContext.boardId);
  assert.equal(replacement.selectedContext().revision, updatedContext.revision);
  assert.deepEqual(replacement.selectedContext().query, updatedContext.query);
  await waitFor(() => dailyButtons(replacement.document).length === 14, "reopened fourteen-day view");
  assert.deepEqual([...first.errors, ...second.errors, ...replacement.errors], []);
});
