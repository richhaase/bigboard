import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { access, chmod, cp, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import { promisify } from "node:util";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { getDefaultEnvironment, StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { APP_VERSION, BOARD_RESOURCE_URI, type BoardSnapshot, type EvidenceResult, type PeopleResult, type SourceConfiguration } from "../src/shared";

const exec = promisify(execFile);
type ToolResult = Awaited<ReturnType<Client["callTool"]>>;
function value<T>(result: ToolResult): T {
  assert.notEqual(result.isError, true, JSON.stringify(result.content));
  assert.ok(result.structuredContent);
  return result.structuredContent as unknown as T;
}
const board = (result: ToolResult) => {
  const resultBoard = value<BoardSnapshot>(result);
  assert.equal(resultBoard.coverage.source, "git+github");
  return resultBoard;
};

async function installation(t: TestContext) {
  const root = await mkdtemp(join(tmpdir(), "bigboard-real-protocol-"));
  const installed = join(root, "installed");
  await cp(fileURLToPath(new URL("../bundle/", import.meta.url)), installed, { recursive: true });
  const bin = join(root, "bin");
  await mkdir(bin);
  const fakeGH = join(bin, "gh");
  await writeFile(fakeGH, "#!/bin/sh\nexit 1\n");
  await chmod(fakeGH, 0o755);
  const paths: string[] = [];
  for (const name of ["widget", "other"]) {
    const path = join(root, name);
    await mkdir(path);
    const git = async (args: string[], date?: string) => (await exec("git", ["-C", path, ...args], { encoding: "utf8", env: { ...process.env, ...(date ? { GIT_AUTHOR_DATE: date, GIT_COMMITTER_DATE: date } : {}) } })).stdout.trim();
    await git(["init", "--initial-branch=main", "--quiet"]);
    await git(["config", "user.name", "Contributor A"]);
    await git(["config", "user.email", "contributor@example.invalid"]);
    await git(["remote", "add", "origin", `https://github.com/example/${name}.git`]);
    await writeFile(join(path, "source.txt"), "first\n");
    await git(["add", "--all"]);
    await git(["commit", "--quiet", "-m", "Prior week change"], new Date(Date.now() - 9 * 86_400_000).toISOString());
    await writeFile(join(path, "source.txt"), "first\nsecond\n");
    await git(["add", "--all"]);
    await git(["commit", "--quiet", "-m", "Current week change"], new Date(Date.now() - 1 * 86_400_000).toISOString());
    await git(["update-ref", "refs/remotes/origin/main", await git(["rev-parse", "HEAD"])]);
    await git(["symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"]);
    paths.push(path);
  }
  const clients = new Set<Client>();
  const stateRoot = join(root, "state");
  const stateDirectory = join(stateRoot, "bigboard-mcp-app");
  t.after(async () => { await Promise.all([...clients].map(client => client.close())); await rm(root, { recursive: true, force: true }); });
  const start = async () => {
    const client = new Client({ name: "real-source-protocol-test", version: "1.0.0" });
    const transport = new StdioClientTransport({ command: process.execPath, args: [join(installed, "server.cjs")], cwd: installed,
      env: { ...getDefaultEnvironment(), PATH: bin + delimiter + (process.env.PATH ?? ""), XDG_STATE_HOME: stateRoot, BIGBOARD_STATE_DIR: "" }, stderr: "pipe" });
    let stderr = "";
    transport.stderr?.on("data", data => { stderr += String(data); });
    clients.add(client);
    try { await client.connect(transport); }
    catch (error) { await transport.close(); throw new Error(`Installed server failed to initialize: ${String(error)}\n${stderr}`); }
    return client;
  };
  const stop = async (client: Client) => { await client.close(); clients.delete(client); };
  const configure = async (client: Client) => value<SourceConfiguration>(await client.callTool({ name: "configure_sources", arguments: { paths, expected_revision: 0 } }));
  return { paths, start, stop, configure, stateDirectory };
}

test("clean installed bundle exposes source tools and keeps polling calls headless", async t => {
  const { start, configure, stateDirectory } = await installation(t);
  const client = await start();
  assert.equal(client.getServerVersion()?.name, "bigboard");
  assert.equal(client.getServerVersion()?.version, APP_VERSION);
  const inventory = await client.listTools();
  assert.deepEqual(inventory.tools.map(tool => tool.name).sort(), ["configure_sources", "list_sources", "open_board", "query_evidence", "query_people", "read_board", "refresh_sources", "update_board"]);
  const opener = inventory.tools.find(tool => tool.name === "open_board");
  assert.equal((opener?._meta?.ui as { resourceUri?: string })?.resourceUri, BOARD_RESOURCE_URI);
  for (const tool of inventory.tools.filter(tool => tool.name !== "open_board")) {
    assert.equal((tool._meta?.ui as { resourceUri?: string } | undefined)?.resourceUri, undefined);
    assert.equal(tool._meta?.["openai/outputTemplate"], undefined);
  }
  const resource = await client.readResource({ uri: BOARD_RESOURCE_URI });
  assert.equal(resource.contents.length, 1);
  const content = resource.contents[0];
  assert.equal(content.mimeType, "text/html;profile=mcp-app");
  assert.ok("text" in content);
  assert.match(content.text, /ui\/initialize/);
  const before = board(await client.callTool({ name: "open_board", arguments: {} }));
  assert.equal(before.initialView, "activity");
  const peopleView = board(await client.callTool({ name: "open_board", arguments: { board_id: before.boardId, view: "people" } }));
  assert.equal(peopleView.boardId, before.boardId);
  assert.equal(peopleView.initialView, "people");
  assert.equal(peopleView.snapshotId, before.snapshotId);
  const prView = board(await client.callTool({ name: "open_board", arguments: { board_id: before.boardId, view: "pull_requests" } }));
  assert.equal(prView.initialView, "pullRequests");
  assert.equal(prView.revision, before.revision);
  assert.equal(board(await client.callTool({ name: "read_board", arguments: { board_id: before.boardId } })).initialView, undefined);
  assert.equal(before.collectedAt, null);
  assert.equal(before.configuration.repositories.length, 0);
  assert.equal(before.coverage.complete, false);
  const configured = await configure(client);
  await access(join(stateDirectory, "boards.sqlite"));
  assert.equal(configured.revision, 1);
  assert.equal(configured.repositories.length, 2);
  assert.deepEqual(value<SourceConfiguration>(await client.callTool({ name: "list_sources", arguments: {} })), configured);
  const conflict = await client.callTool({ name: "configure_sources", arguments: { paths: [], expected_revision: 0 } });
  assert.equal(conflict.isError, true);
  assert.equal(value<SourceConfiguration>(await client.callTool({ name: "list_sources", arguments: {} })).repositories.length, 2);
});

test("real Git refresh feeds the same board and stale selections fail without requiring GitHub auth", async t => {
  const { start, configure } = await installation(t);
  const client = await start();
  await configure(client);
  const first = board(await client.callTool({ name: "open_board", arguments: {} }));
  const second = board(await client.callTool({ name: "open_board", arguments: {} }));
  const notifications: Array<{ progress: number; total?: number; message?: string }> = [];
  const refresh = value<{ repositoryCount: number; complete: boolean }>(await client.callTool({ name: "refresh_sources", arguments: {} }, undefined, { onprogress: progress => { notifications.push(progress); } }));
  assert.equal(refresh.repositoryCount, 2);
  assert.equal(refresh.complete, false);
  assert.ok(notifications.length >= 3);
  assert.equal(notifications[0].progress, 0);
  assert.ok(notifications.every(progress => progress.total === 4 && progress.message?.includes("repositories checked")));
  for (let index = 1; index < notifications.length; index += 1) assert.ok(notifications[index].progress > notifications[index - 1].progress);
  const collected = board(await client.callTool({ name: "read_board", arguments: { board_id: first.boardId } }));
  assert.equal(collected.refresh?.status, "complete");
  assert.deepEqual(collected.refresh?.git, { completedRepositories: 2, totalRepositories: 2 });
  assert.equal(collected.refresh?.github.requests, 0);
  assert.match(collected.refresh!.message, /coverage gaps/);
  assert.equal(collected.boardId, first.boardId);
  assert.notEqual(collected.snapshotId, first.snapshotId);
  assert.equal(collected.totals.commits, 2);
  assert.equal(collected.totals.openPRs, 0);
  assert.equal(collected.account, null);
  assert.ok(collected.coverage.repositories.every(repo => repo.historyComplete && !repo.openPRsComplete));
  const people = value<PeopleResult>(await client.callTool({ name: "query_people", arguments: { board_id: collected.boardId, snapshot_id: collected.snapshotId, source: "git" } }));
  assert.equal(people.totalPeople, 1);
  assert.equal(people.people[0].commits, 2);
  assert.deepEqual(people.people[0].repositories, ["example/other", "example/widget"]);
  const personEvidence = value<EvidenceResult>(await client.callTool({ name: "query_evidence", arguments: { board_id: collected.boardId, snapshot_id: collected.snapshotId, person_id: people.people[0].id, kind: "activity" } }));
  assert.equal(personEvidence.selectedPersonId, people.people[0].id);
  assert.equal(personEvidence.totalRecords, 2);
  assert.deepEqual(personEvidence.pullRequests, []);
  const githubPeople = value<PeopleResult>(await client.callTool({ name: "query_people", arguments: { board_id: collected.boardId, snapshot_id: collected.snapshotId, source: "github" } }));
  assert.equal(githubPeople.totalPeople, 0);
  assert.equal(githubPeople.coverage.complete, false);
  const changed = board(await client.callTool({ name: "update_board", arguments: { board_id: first.boardId, expected_revision: first.revision, repository: "example/widget", compare: true } }));
  assert.equal(changed.revision, first.revision + 1);
  assert.equal(changed.totals.commits, 1);
  assert.equal(changed.totals.previousCommits, 1);
  assert.deepEqual((board(await client.callTool({ name: "read_board", arguments: { board_id: second.boardId } }))).query, second.query);
  const bucket = changed.buckets.find(bucket => bucket.commits > 0);
  assert.ok(bucket);
  const selected = value<EvidenceResult>(await client.callTool({ name: "query_evidence", arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId, bucket_start: bucket.start } }));
  assert.deepEqual(selected.records.map(record => record.id), bucket.recordIds);
  assert.equal(selected.records.length, 1);
  assert.equal(selected.records[0].repository, "example/widget");
  assert.match(selected.records[0].url, /^https:\/\/github\.com\/example\/widget\/commit\/[a-f0-9]{40}$/);
  assert.ok(selected.records[0].timestamp >= bucket.start && selected.records[0].timestamp < bucket.end);
  const activity = value<EvidenceResult>(await client.callTool({ name: "query_evidence", arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId, bucket_start: bucket.start, kind: "activity" } }));
  assert.equal(activity.kind, "activity");
  assert.equal(activity.records.length, 1);
  assert.deepEqual(activity.pullRequests, []);
  const openPRs = value<EvidenceResult>(await client.callTool({ name: "query_evidence", arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId, kind: "pull_requests", pull_request_state: "open" } }));
  assert.equal(openPRs.kind, "pull_requests");
  assert.equal(openPRs.pullRequestState, "open");
  assert.deepEqual(openPRs.records, []);
  assert.deepEqual(openPRs.pullRequests, []);
  const incompatible = await client.callTool({ name: "query_evidence", arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId, kind: "activity", pull_request_state: "open" } });
  assert.equal(incompatible.isError, true);
  assert.equal((incompatible.structuredContent as { error: { code: string } }).error.code, "invalid_filter");
  const unavailablePR = await client.callTool({ name: "query_evidence", arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId, kind: "pull_requests", pull_request_id: "example/widget#1" } });
  assert.equal(unavailablePR.isError, true);
  assert.equal((unavailablePR.structuredContent as { error: { code: string } }).error.code, "selection_not_found");
  const oldRevision = await client.callTool({ name: "update_board", arguments: { board_id: first.boardId, expected_revision: first.revision, days: 14 } });
  assert.equal(oldRevision.isError, true);
  await client.callTool({ name: "refresh_sources", arguments: {} });
  const oldSnapshot = await client.callTool({ name: "query_evidence", arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId } });
  assert.equal(oldSnapshot.isError, true);
  assert.equal((await client.callTool({ name: "query_people", arguments: { board_id: changed.boardId, snapshot_id: changed.snapshotId, source: "git" } })).isError, true);
});

test("server restart preserves selected sources and board query while requiring fresh GitHub verification", async t => {
  const { start, stop, configure, paths } = await installation(t);
  const original = await start();
  await configure(original);
  await original.callTool({ name: "refresh_sources", arguments: {} });
  const initial = board(await original.callTool({ name: "open_board", arguments: {} }));
  const updated = board(await original.callTool({ name: "update_board", arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "example/other", days: 14 } }));
  await stop(original);
  const restarted = await start();
  assert.equal(restarted.getServerVersion()?.name, "bigboard");
  assert.equal(restarted.getServerVersion()?.version, APP_VERSION);
  const reopened = board(await restarted.callTool({ name: "open_board", arguments: { board_id: initial.boardId } }));
  assert.equal(reopened.boardId, updated.boardId);
  assert.equal(reopened.revision, updated.revision);
  assert.deepEqual(reopened.query, updated.query);
  assert.equal(reopened.totals.commits, updated.totals.commits);
  assert.equal(reopened.configuration.needsRefresh, true);
  assert.equal(reopened.account, null);
  const settings = value<SourceConfiguration>(await restarted.callTool({ name: "list_sources", arguments: {} }));
  assert.equal(settings.revision, 1);
  assert.equal(settings.repositories.length, paths.length);
});

test("separate installed stdio processes cannot overwrite the same board revision", async t => {
  const { start, configure } = await installation(t);
  const first = await start(), second = await start();
  await configure(first);
  const initial = board(await first.callTool({ name: "open_board", arguments: {} }));
  const results = await Promise.all([
    first.callTool({ name: "update_board", arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "example/widget" } }),
    second.callTool({ name: "update_board", arguments: { board_id: initial.boardId, expected_revision: initial.revision, repository: "example/other" } }),
  ]);
  const successes = results.filter(result => result.isError !== true);
  assert.equal(successes.length, 1);
  assert.equal(results.filter(result => result.isError === true).length, 1);
  const saved = board(successes[0]);
  assert.equal(saved.revision, initial.revision + 1);
  assert.deepEqual((board(await first.callTool({ name: "read_board", arguments: { board_id: initial.boardId } }))).query, saved.query);
  assert.deepEqual((board(await second.callTool({ name: "read_board", arguments: { board_id: initial.boardId } }))).query, saved.query);
});
