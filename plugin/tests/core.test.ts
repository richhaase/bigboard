import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { test, type TestContext } from "node:test";
import { BoardError, BoardStore } from "../src/core";
import type { ActivityRecord, ConfiguredRepository, GitCollectionResult, GitHubCollectionResult, PullRequestRecord, PullRequestSort } from "../src/shared";

const DAY = 86_400_000;
const NOW = new Date("2026-11-17T12:00:00.000Z");
const repos: ConfiguredRepository[] = [
  { id: "example/widget", path: "/example/widget", remote: "origin", defaultBranch: "main" },
  { id: "example/other", path: "/example/other", remote: "origin", defaultBranch: "main" },
];
const code = (expected: string) => (error: unknown) => error instanceof BoardError && error.code === expected;

function commit(repository: string, index: number, timestamp: string): ActivityRecord {
  const sha = (index + 1).toString(16).padStart(40, "0");
  return { id: `${repository}@${sha}`, repository, sha, timestamp, subject: `Change ${index}`, contributor: "Contributor A", email: "a@example.invalid", additions: 10, deletions: 2, files: [{ path: "source.txt", additions: 10, deletions: 2 }], url: `https://github.com/${repository}/commit/${sha}` };
}
function pr(repository: string, number: number, state: "open" | "merged", timestamp: string): PullRequestRecord {
  return { id: `${repository}#${number}`, repository, number, title: `Pull request ${number}`, url: `https://github.com/${repository}/pull/${number}`, author: "fixture-user", state, draft: false, createdAt: "2026-08-01T00:00:00.000Z", updatedAt: timestamp, mergedAt: state === "merged" ? timestamp : null, baseBranch: "main", headSha: number.toString(16).padStart(40, "0") };
}
function fixtures() {
  let records = repos.flatMap((repo, repoIndex) => Array.from({ length: 28 }, (_, day) => commit(repo.id, repoIndex * 100 + day, new Date(NOW.getTime() - (day + 0.5) * DAY).toISOString())));
  let pullRequests = [pr(repos[0].id, 1, "open", "2026-08-02T00:00:00.000Z"), pr(repos[0].id, 2, "merged", "2026-11-15T10:00:00.000Z"), pr(repos[1].id, 3, "merged", "2026-11-14T10:00:00.000Z")];
  let failure: "auth" | "denied" | null = null;
  const calls = { describe: 0, native: 0, git: 0, github: 0 };
  let beforeGit: (() => Promise<void>) | undefined;
  const dependencies = {
    now: () => NOW,
    describeRepository: async (path: string) => { calls.describe += 1; return repos.find(repo => repo.path === path)!; },
    importNativeSources: async () => { calls.native += 1; return [...repos, repos[0]]; },
    collectGitHistory: async (repository: ConfiguredRepository, options: { since: string; until: string }): Promise<GitCollectionResult> => {
      calls.git += 1;
      assert.equal(Date.parse(options.until) - Date.parse(options.since), 28 * DAY);
      await beforeGit?.();
      const selected = records.filter(record => record.repository === repository.id);
      return { repository, revision: "a".repeat(40), ref: `refs/remotes/origin/${repository.defaultBranch}`, shallow: false, partial: false, complete: true, linesComplete: selected.every(record => record.additions !== null && record.deletions !== null), records: selected, error: null };
    },
    collectGitHub: async (repositories: ConfiguredRepository[]): Promise<GitHubCollectionResult> => {
      calls.github += 1;
      return { account: failure === "auth" ? null : { id: "7", login: "fixture-user" }, repositories: repositories.map(repo => ({ id: repo.id, openComplete: failure === null, mergedComplete: failure === null, accessDenied: failure === "denied", error: failure ? "Access could not be verified." : null })), pullRequests: failure ? [] : pullRequests.filter(pr => repositories.some(repo => repo.id === pr.repository)), error: failure === "auth" ? "Authentication is unavailable." : null };
    },
  };
  return { dependencies, calls, setRecords: (value: ActivityRecord[]) => { records = value; }, setPRs: (value: PullRequestRecord[]) => { pullRequests = value; }, setFailure: (value: typeof failure) => { failure = value; }, delayGit: (value: () => Promise<void>) => { beforeGit = value; } };
}
async function installation(t: TestContext) {
  const directory = await mkdtemp(join(tmpdir(), "bigboard-core-"));
  const data = fixtures();
  const stores: BoardStore[] = [];
  const open = () => { const store = new BoardStore(directory, data.dependencies); stores.push(store); return store; };
  t.after(async () => { stores.forEach(store => store.close()); await rm(directory, { recursive: true, force: true }); });
  return { directory, data, open, store: open() };
}

test("empty or relative XDG values use the standard state location", () => {
  const base = process.env.XDG_STATE_HOME, override = process.env.BIGBOARD_STATE_DIR;
  try {
    process.env.BIGBOARD_STATE_DIR = "";
    for (const value of ["", "relative"]) {
      process.env.XDG_STATE_HOME = value;
      assert.equal(new BoardStore().directory, join(homedir(), ".local", "state", "bigboard-mcp-app"));
    }
  } finally {
    if (base === undefined) delete process.env.XDG_STATE_HOME; else process.env.XDG_STATE_HOME = base;
    if (override === undefined) delete process.env.BIGBOARD_STATE_DIR; else process.env.BIGBOARD_STATE_DIR = override;
  }
});

test("source settings use independent revisions and reads or viewing filters never collect", async (t) => {
  const { store, data, open } = await installation(t);
  const board = await store.open();
  assert.equal(board.collectedAt, null);
  assert.equal(board.configuration.needsRefresh, true);
  assert.equal(board.coverage.complete, false);
  await assert.rejects(store.refresh(), code("sources_required"));
  const configured = await store.configure(undefined, true, 0);
  assert.equal(configured.repositories.length, 2);
  assert.equal(data.calls.native, 1);
  await assert.rejects(open().configure([repos[0].path], false, 0), code("configuration_conflict"));
  const changed = await store.update(board.boardId, board.revision, { repository: repos[0].id, compare: true });
  await store.read(changed.boardId);
  assert.equal(data.calls.git, 0);
  assert.equal(data.calls.github, 0);
  assert.equal(store.sources().repositories.length, 2);
  await assert.rejects(store.update(changed.boardId, changed.revision, { repository: "example/missing" }), code("repository_not_configured"));
  await assert.rejects(store.configure([], true, configured.revision), code("invalid_sources"));
});

test("refresh supplies real records, exact half-open dates, comparison, and independent current PRs", async (t) => {
  const { store, data } = await installation(t);
  const boundary = new Date(NOW.getTime() - 7 * DAY).toISOString();
  data.setRecords([commit(repos[0].id, 1, boundary), commit(repos[0].id, 2, new Date(Date.parse(boundary) - 1).toISOString()), commit(repos[0].id, 3, NOW.toISOString())]);
  await store.configure([repos[0].path], false, 0);
  const opened = await store.open();
  await store.refresh();
  const board = await store.update(opened.boardId, opened.revision, { compare: true });
  assert.equal(board.scope.since, boundary);
  assert.equal(board.scope.until, NOW.toISOString());
  assert.equal(board.coverage.source, "git+github");
  assert.equal(board.coverage.complete, true);
  assert.equal(board.totals.commits, 1);
  assert.equal(board.totals.previousCommits, 1);
  assert.equal(board.totals.mergedPRs, 1);
  assert.equal(board.totals.openPRs, 1);
  const evidence = await store.evidence(board.boardId, board.snapshotId, board.buckets[0].start);
  assert.equal(evidence.records.length, 1);
  assert.equal(evidence.records[0].timestamp, boundary);
  assert.ok(evidence.pullRequests.some(value => value.state === "open" && value.createdAt < board.scope.since));
  assert.equal(evidence.pullRequests.some(value => value.state === "merged"), false);
  assert.equal(data.calls.github, 1);
});

test("refresh invalidates old selection snapshots while filters remain independent", async (t) => {
  const { store, data } = await installation(t);
  await store.configure(undefined, true, 0);
  await store.refresh();
  const first = await store.open(), second = await store.open();
  const changed = await store.update(first.boardId, first.revision, { repository: repos[0].id, days: 14, compare: true });
  assert.deepEqual((await store.read(second.boardId)).query, second.query);
  const calls = { ...data.calls };
  await store.read(first.boardId);
  await store.evidence(first.boardId, changed.snapshotId);
  assert.deepEqual(data.calls, calls);
  assert.equal(changed.totals.commits, 14);
  assert.equal(changed.totals.previousCommits, 14);
  await store.refresh();
  const latest = await store.read(first.boardId);
  assert.equal(latest.revision, changed.revision);
  assert.notEqual(latest.snapshotId, changed.snapshotId);
  await assert.rejects(store.evidence(first.boardId, changed.snapshotId), code("stale_snapshot"));
});

test("restarted stores preserve Git and board queries but hide PR cache until access is refreshed", async (t) => {
  const { store, open } = await installation(t);
  await store.configure(undefined, true, 0);
  await store.refresh();
  const initial = await store.open();
  const changed = await store.update(initial.boardId, initial.revision, { repository: repos[0].id, days: 14 });
  store.close();
  const restarted = open();
  const recovered = await restarted.open(initial.boardId);
  assert.deepEqual(recovered.query, changed.query);
  assert.equal(recovered.revision, changed.revision);
  assert.equal(recovered.totals.commits, changed.totals.commits);
  assert.equal(recovered.totals.openPRs, 0);
  assert.equal(recovered.totals.mergedPRs, 0);
  assert.equal(recovered.account, null);
  assert.equal(recovered.configuration.needsRefresh, true);
  assert.equal((await restarted.evidence(recovered.boardId, recovered.snapshotId)).pullRequests.length, 0);
  await restarted.refresh();
  const verified = await restarted.read(initial.boardId);
  assert.equal(verified.totals.openPRs, 1);
  assert.notEqual(verified.snapshotId, recovered.snapshotId);
});

test("failed authentication and denied access discard previous PR evidence", async (t) => {
  const { store, data } = await installation(t);
  await store.configure([repos[0].path], false, 0);
  await store.refresh();
  const initial = await store.open();
  assert.equal(initial.totals.openPRs, 1);
  for (const failure of ["auth", "denied"] as const) {
    data.setFailure(failure);
    await store.refresh();
    const board = await store.read(initial.boardId);
    assert.equal(board.totals.openPRs, 0);
    assert.equal(board.totals.mergedPRs, 0);
    assert.equal(board.coverage.complete, false);
    assert.equal(board.coverage.repositories[0].openPRsComplete, false);
    assert.equal((await store.evidence(board.boardId, board.snapshotId)).pullRequests.length, 0);
  }
});

test("unknown line evidence remains a lower bound with explicit coverage gaps", async (t) => {
  const { store, data } = await installation(t);
  data.setRecords([{ ...commit(repos[0].id, 1, "2026-11-16T12:00:00.000Z"), additions: null, deletions: null, files: [] }]);
  await store.configure([repos[0].path], false, 0);
  await store.refresh();
  const board = await store.open();
  assert.equal(board.totals.commits, 1);
  assert.equal(board.totals.unknownLineCommits, 1);
  assert.equal(board.totals.additions, 0);
  assert.equal(board.coverage.complete, false);
  assert.equal(board.coverage.repositories[0].linesComplete, false);
});

test("a configuration change rejects an in-flight old refresh", async (t) => {
  const { store, data } = await installation(t);
  const configuration = await store.configure([repos[0].path], false, 0);
  let release!: () => void;
  let started!: () => void;
  const waiting = new Promise<void>(resolve => { release = resolve; });
  const entered = new Promise<void>(resolve => { started = resolve; });
  data.delayGit(async () => { started(); await waiting; });
  const refresh = store.refresh();
  await entered;
  await assert.rejects(store.refresh(), code("refresh_in_progress"));
  await store.configure([repos[1].path], false, configuration.revision);
  release();
  await assert.rejects(refresh, code("refresh_superseded"));
  const board = await store.open();
  assert.equal(board.collectedAt, null);
  assert.deepEqual(board.configuration.repositories, [repos[1].id]);
});

test("paged evidence is complete, stable, and scoped to its snapshot", async (t) => {
  const { store, data } = await installation(t);
  data.setRecords(Array.from({ length: 123 }, (_, index) => commit(repos[0].id, index, new Date(NOW.getTime() - 3_600_000 - index * 1000).toISOString())));
  data.setPRs(Array.from({ length: 61 }, (_, index) => pr(repos[0].id, index + 1, "open", "2026-11-16T12:00:00.000Z")));
  await store.configure([repos[0].path], false, 0);
  await store.refresh();
  const board = await store.open();
  const records: string[] = [], prs: string[] = [];
  let cursor: string | undefined;
  do {
    const page = await store.evidence(board.boardId, board.snapshotId, undefined, cursor);
    assert.ok(page.records.length <= 50 && page.pullRequests.length <= 50);
    records.push(...page.records.map(record => record.id));
    prs.push(...page.pullRequests.map(record => record.id));
    cursor = page.nextCursor ?? undefined;
  } while (cursor !== undefined);
  assert.equal(records.length, 123);
  assert.equal(new Set(records).size, 123);
  assert.equal(prs.length, 61);
  assert.equal(new Set(prs).size, 61);
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, "wrong"), code("invalid_cursor"));
});

test("cancelled refresh retains Git evidence while invalidating cached GitHub access", async (t) => {
  const { store, data } = await installation(t);
  await store.configure([repos[0].path], false, 0);
  await store.refresh();
  const initial = await store.open();
  let release!: () => void;
  let started!: () => void;
  const waiting = new Promise<void>(resolve => { release = resolve; });
  const entered = new Promise<void>(resolve => { started = resolve; });
  data.delayGit(async () => { started(); await waiting; });
  const controller = new AbortController();
  const pending = store.refresh(controller.signal);
  await entered;
  controller.abort();
  release();
  await assert.rejects(pending, code("refresh_cancelled"));
  const retained = await store.read(initial.boardId);
  assert.notEqual(retained.snapshotId, initial.snapshotId);
  assert.equal(retained.collectedAt, initial.collectedAt);
  assert.deepEqual(retained.query, initial.query);
  assert.equal(retained.totals.commits, initial.totals.commits);
  assert.equal(retained.totals.additions, initial.totals.additions);
  assert.equal(retained.totals.deletions, initial.totals.deletions);
  assert.equal(retained.totals.openPRs, 0);
  assert.equal(retained.totals.mergedPRs, 0);
  assert.equal(retained.configuration.needsRefresh, true);
  assert.equal(retained.coverage.repositories[0].openPRsComplete, false);
});

test("an aborted auth-denied refresh invalidates another active store's cached PR access", async (t) => {
  const { store, open, data } = await installation(t);
  await store.configure([repos[0].path], false, 0);
  await store.refresh();
  const initial = await store.open();
  const selected = await store.update(initial.boardId, initial.revision, { repository: repos[0].id });
  const another = open();
  await another.refresh();
  const verified = await another.read(initial.boardId);
  assert.equal(verified.totals.openPRs, 1);
  assert.equal((await another.evidence(verified.boardId, verified.snapshotId)).pullRequests.length, 2);
  let release!: () => void;
  let started!: () => void;
  const waiting = new Promise<void>(resolve => { release = resolve; });
  const entered = new Promise<void>(resolve => { started = resolve; });
  data.setFailure("auth");
  data.delayGit(async () => { started(); await waiting; });
  const controller = new AbortController();
  const pending = store.refresh(controller.signal);
  await entered;
  controller.abort();
  release();
  await assert.rejects(pending, code("refresh_cancelled"));
  for (const active of [store, another]) {
    const board = await active.read(initial.boardId);
    assert.deepEqual(board.query, selected.query);
    assert.equal(board.collectedAt, verified.collectedAt);
    assert.equal(board.totals.commits, verified.totals.commits);
    assert.equal(board.totals.openPRs, 0);
    assert.equal(board.totals.mergedPRs, 0);
    assert.equal(board.account, null);
    assert.equal(board.configuration.needsRefresh, true);
    assert.equal((await active.evidence(board.boardId, board.snapshotId)).pullRequests.length, 0);
  }
  await assert.rejects(another.evidence(verified.boardId, verified.snapshotId), code("stale_snapshot"));
});

test("old demo boards migrate to real source scope without recreating fixture evidence", async (t) => {
  const { store, directory } = await installation(t);
  const original = await store.open();
  const database = new DatabaseSync(join(directory, "boards.sqlite"));
  try {
    const saved = { version: 1, boardId: original.boardId, revision: original.revision, updatedAt: original.updatedAt, query: { days: 7, repository: "sample/api", compare: false } };
    database.prepare("UPDATE boards SET state=? WHERE id=?").run(JSON.stringify(saved), original.boardId);
  } finally { database.close(); }
  const migrated = await store.read(original.boardId);
  assert.equal(migrated.revision, original.revision + 1);
  assert.equal(migrated.query.repository, "all");
  assert.equal(migrated.coverage.source, "git+github");
  assert.equal(migrated.totals.commits, 0);
  assert.equal(migrated.collectedAt, null);
});

test("independent SQLite stores enforce board CAS and reset removed repository filters", async (t) => {
  const { store, open } = await installation(t);
  await store.configure(undefined, true, 0);
  const initial = await store.open();
  const another = open();
  const results = await Promise.allSettled([
    store.update(initial.boardId, initial.revision, { repository: repos[0].id }),
    another.update(initial.boardId, initial.revision, { repository: repos[1].id }),
  ]);
  assert.equal(results.filter(result => result.status === "fulfilled").length, 1);
  assert.equal(results.filter(result => result.status === "rejected").length, 1);
  const current = await store.read(initial.boardId);
  await store.configure([], false, store.sources().revision);
  const removed = await store.read(initial.boardId);
  assert.equal(removed.query.repository, "all");
  assert.equal(removed.revision, current.revision + 1);
});

test("activity and PR views receive only their own evidence while the default query stays compatible", async t => {
  const { store } = await installation(t);
  await store.configure(undefined, true, 0);
  await store.refresh();
  const board = await store.open();
  const mixed = await store.evidence(board.boardId, board.snapshotId);
  assert.ok(mixed.records.length > 0);
  assert.ok(mixed.pullRequests.length > 0);
  assert.equal(mixed.kind, "all");
  const bucket = board.buckets[0];
  const activity = await store.evidence(board.boardId, board.snapshotId, bucket.start, undefined, { kind: "activity" });
  assert.equal(activity.kind, "activity");
  assert.deepEqual(activity.records.map(record => record.id), bucket.recordIds);
  assert.deepEqual(activity.pullRequests, []);
  assert.equal(activity.totalPullRequests, 0);
  const open = await store.evidence(board.boardId, board.snapshotId, bucket.start, undefined, { kind: "pull_requests", pullRequestState: "open" });
  assert.deepEqual(open.records, []);
  assert.equal(open.totalRecords, 0);
  assert.equal(open.pullRequestState, "open");
  assert.equal(open.pullRequests.length, 1);
  assert.ok(open.pullRequests.every(request => request.state === "open" && request.createdAt < bucket.start));
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, undefined, { kind: "activity", pullRequestState: "open" }), code("invalid_filter"));
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, undefined, { kind: "activity", pullRequestId: "example/widget#1" }), code("invalid_filter"));
});

test("PR tabs filter before pagination and reject cursors from another evidence view or state", async t => {
  const { store, data } = await installation(t);
  data.setPRs(Array.from({ length: 61 }, (_, index) => pr(repos[0].id, index + 1, index % 2 ? "open" : "merged", "2026-11-16T12:00:00.000Z")));
  await store.configure([repos[0].path], false, 0);
  await store.refresh();
  const board = await store.open();
  const first = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { kind: "pull_requests", pullRequestState: "open" });
  assert.equal(first.totalPullRequests, 30);
  assert.equal(first.pullRequests.length, 25);
  assert.ok(first.pullRequests.every(request => request.state === "open"));
  assert.deepEqual(first.records, []);
  assert.ok(first.nextCursor);
  const second = await store.evidence(board.boardId, board.snapshotId, undefined, first.nextCursor, { kind: "pull_requests", pullRequestState: "open" });
  assert.equal(second.pullRequests.length, 5);
  assert.equal(second.nextCursor, null);
  assert.equal(new Set([...first.pullRequests, ...second.pullRequests].map(request => request.id)).size, 30);
  const merged = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { pullRequestState: "merged" });
  assert.equal(merged.kind, "pull_requests");
  assert.equal(merged.totalPullRequests, 31);
  assert.ok(merged.pullRequests.every(request => request.state === "merged"));
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, first.nextCursor, { kind: "pull_requests", pullRequestState: "merged" }), code("invalid_cursor"));
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, first.nextCursor, { kind: "activity" }), code("invalid_cursor"));
  const activity = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { kind: "activity" });
  assert.equal(activity.totalPullRequests, 0);
  const all = await store.evidence(board.boardId, board.snapshotId);
  assert.ok(all.nextCursor);
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, all.nextCursor, { kind: "activity" }), code("invalid_cursor"));
  await store.refresh();
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, first.nextCursor, { kind: "pull_requests", pullRequestState: "open" }), code("stale_snapshot"));
});

test("selected PR evidence is exact and constrained by repository, merge interval, state, and verified account", async t => {
  const { store, data } = await installation(t);
  data.setPRs([
    pr(repos[0].id, 1, "open", "2026-08-02T00:00:00.000Z"),
    pr(repos[0].id, 2, "merged", "2026-11-15T10:00:00.000Z"),
    pr(repos[0].id, 3, "merged", "2026-10-20T10:00:00.000Z"),
    pr(repos[1].id, 4, "open", "2026-11-16T10:00:00.000Z"),
  ]);
  await store.configure(undefined, true, 0);
  await store.refresh();
  const initial = await store.open();
  const board = await store.update(initial.boardId, initial.revision, { repository: repos[0].id });
  const selected = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { pullRequestId: "example/widget#1" });
  assert.equal(selected.kind, "pull_requests");
  assert.equal(selected.selectedPullRequestId, "example/widget#1");
  assert.deepEqual(selected.records, []);
  assert.deepEqual(selected.pullRequests.map(request => request.id), ["example/widget#1"]);
  assert.equal(selected.nextCursor, null);
  for (const filter of [
    { kind: "pull_requests" as const, pullRequestId: "example/other#4" },
    { kind: "pull_requests" as const, pullRequestId: "example/widget#3" },
    { kind: "pull_requests" as const, pullRequestId: "example/widget#99" },
    { kind: "pull_requests" as const, pullRequestId: "example/widget#1", pullRequestState: "merged" as const },
  ]) await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, undefined, filter), code("selection_not_found"));
  const merged = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { kind: "pull_requests", pullRequestState: "merged", pullRequestId: "example/widget#2" });
  assert.deepEqual(merged.pullRequests.map(request => request.id), ["example/widget#2"]);
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, board.buckets[0].start, undefined, { pullRequestId: "example/widget#2" }), code("selection_not_found"));
  data.setFailure("denied");
  await store.refresh();
  const denied = await store.read(board.boardId);
  await assert.rejects(store.evidence(denied.boardId, denied.snapshotId, undefined, undefined, { pullRequestId: "example/widget#1" }), code("selection_not_found"));
});

test("People keeps mailmapped email and GitHub login identities distinct with exact date and repository scope", async t => {
  const { store, data } = await installation(t);
  const since = new Date(NOW.getTime() - 7 * DAY).toISOString();
  data.setRecords([
    { ...commit(repos[0].id, 1, since), contributor: "Same Name", email: "first@example.invalid" },
    { ...commit(repos[1].id, 2, "2026-11-16T10:00:00.000Z"), contributor: "New Name", email: "first@example.invalid", additions: null, deletions: null },
    { ...commit(repos[0].id, 3, "2026-11-16T09:00:00.000Z"), contributor: "Same Name", email: "other@example.invalid" },
    { ...commit(repos[0].id, 4, new Date(Date.parse(since) - 1).toISOString()), email: "prior@example.invalid" },
    { ...commit(repos[0].id, 5, NOW.toISOString()), email: "future@example.invalid" },
    { ...commit(repos[0].id, 6, "2026-11-15T09:00:00.000Z"), contributor: "Same Name", email: "" },
    { ...commit(repos[0].id, 7, "2026-11-14T09:00:00.000Z"), contributor: "Same Name", email: "" },
  ]);
  data.setPRs([
    { ...pr(repos[0].id, 1, "open", "2026-08-02T00:00:00.000Z"), author: "Same Name" },
    { ...pr(repos[1].id, 2, "merged", "2026-11-15T10:00:00.000Z"), author: "SAME NAME" },
    { ...pr(repos[0].id, 3, "merged", new Date(Date.parse(since) - 1).toISOString()), author: "prior-user" },
    { ...pr(repos[0].id, 4, "merged", NOW.toISOString()), author: "future-user" },
  ]);
  await store.configure(undefined, true, 0);
  await store.refresh();
  const board = await store.open();
  const git = await store.people(board.boardId, board.snapshotId, "git");
  assert.equal(git.totalPeople, 4);
  assert.equal(git.source, "git");
  const first = git.people.find(person => person.identity === "first@example.invalid")!;
  assert.equal(first.name, "New Name");
  assert.equal(first.commits, 2);
  assert.equal(first.additions, 10);
  assert.equal(first.deletions, 2);
  assert.equal(first.unknownLineCommits, 1);
  assert.deepEqual(first.repositories, [repos[1].id, repos[0].id]);
  assert.equal(git.people.filter(person => person.identity === "Email unavailable").length, 2);
  const commits = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { personId: first.id });
  assert.equal(commits.selectedPersonId, first.id);
  assert.equal(commits.records.length, 2);
  assert.deepEqual(commits.pullRequests, []);
  assert.ok(commits.records.every(record => record.email === first.identity));
  const github = await store.people(board.boardId, board.snapshotId, "github");
  assert.equal(github.totalPeople, 1);
  const author = github.people[0];
  assert.notEqual(author.id, first.id);
  assert.equal(author.openPRs, 1);
  assert.equal(author.mergedPRs, 1);
  assert.equal(author.commits, 0);
  const prs = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { personId: author.id });
  assert.equal(prs.pullRequests.length, 2);
  assert.deepEqual(prs.records, []);
  assert.ok(prs.pullRequests.some(request => request.createdAt < since));
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, undefined, { personId: first.id, kind: "pull_requests" }), code("invalid_filter"));
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, undefined, { personId: author.id, kind: "activity" }), code("invalid_filter"));
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, undefined, { personId: "git:unavailable" }), code("selection_not_found"));
  const narrowed = await store.update(board.boardId, board.revision, { repository: repos[0].id });
  const narrowedGit = await store.people(narrowed.boardId, narrowed.snapshotId, "git");
  assert.equal(narrowedGit.people.find(person => person.id === first.id)?.commits, 1);
  assert.deepEqual((await store.people(narrowed.boardId, narrowed.snapshotId, "github")).people.map(person => [person.openPRs, person.mergedPRs]), [[1, 0]]);
  await assert.rejects(store.people(board.boardId, board.snapshotId, "git"), code("stale_snapshot"));
});

test("People pages after aggregation, binds cursors to source and snapshot, and honors GitHub access invalidation", async t => {
  const { store, data, open } = await installation(t);
  data.setRecords(Array.from({ length: 31 }, (_, index) => ({ ...commit(repos[0].id, index, "2026-11-16T10:00:00.000Z"), email: `contributor-${String(index).padStart(2, "0")}@example.invalid` })));
  data.setPRs(Array.from({ length: 31 }, (_, index) => ({ ...pr(repos[0].id, index + 1, "open", "2026-08-02T00:00:00.000Z"), author: `author-${index}` })));
  await store.configure([repos[0].path], false, 0);
  await store.refresh();
  const board = await store.open();
  const first = await store.people(board.boardId, board.snapshotId, "git");
  assert.equal(first.totalPeople, 31);
  assert.equal(first.people.length, 25);
  assert.ok(first.nextCursor);
  const second = await store.people(board.boardId, board.snapshotId, "git", first.nextCursor);
  assert.equal(second.people.length, 6);
  assert.equal(second.nextCursor, null);
  assert.equal(new Set([...first.people, ...second.people].map(person => person.id)).size, 31);
  await assert.rejects(store.people(board.boardId, board.snapshotId, "github", first.nextCursor), code("invalid_cursor"));
  const github = await store.people(board.boardId, board.snapshotId, "github");
  assert.equal(github.totalPeople, 31);
  const restarted = open();
  const cached = await restarted.read(board.boardId);
  assert.equal((await restarted.people(cached.boardId, cached.snapshotId, "github")).totalPeople, 0);
  assert.equal((await restarted.people(cached.boardId, cached.snapshotId, "git")).totalPeople, 31);
  await assert.rejects(restarted.evidence(cached.boardId, cached.snapshotId, undefined, undefined, { personId: github.people[0].id }), code("selection_not_found"));
  data.setFailure("denied");
  await store.refresh();
  const denied = await store.read(board.boardId);
  assert.equal((await store.people(denied.boardId, denied.snapshotId, "github")).totalPeople, 0);
  await assert.rejects(store.people(board.boardId, board.snapshotId, "git", first.nextCursor), code("stale_snapshot"));
});

test("PR sorts cover the full filtered inventory before paging and bind cursors to normalized sort scope", async t => {
  const { store, data } = await installation(t);
  const prs = Array.from({ length: 61 }, (_, index) => ({
    ...pr(index % 2 ? repos[0].id : repos[1].id, index + 1, index % 4 ? "open" : "merged", new Date(NOW.getTime() - index * 1000).toISOString()),
    title: `Title ${String(61 - index).padStart(2, "0")}`,
    author: `author-${index % 3}`,
    createdAt: new Date(NOW.getTime() - (61 - index) * DAY).toISOString(),
  }));
  data.setPRs(prs);
  await store.configure(undefined, true, 0);
  await store.refresh();
  const board = await store.open();
  for (const field of ["updated", "created", "number", "title", "author", "repository"] as PullRequestSort[]) {
    for (const direction of ["asc", "desc"] as const) {
      const filter = { kind: "pull_requests" as const, pullRequestState: "open" as const, pullRequestSort: field, sortDirection: direction };
      const first = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, filter);
      assert.equal(first.pullRequestSort, field);
      assert.equal(first.sortDirection, direction);
      assert.equal(first.totalPullRequests, prs.filter(pr => pr.state === "open").length);
      assert.equal(first.pullRequests.length, 25);
      assert.ok(first.nextCursor);
      const second = await store.evidence(board.boardId, board.snapshotId, undefined, first.nextCursor, filter);
      const key = (pr: PullRequestRecord) => field === "updated" ? pr.updatedAt : field === "created" ? pr.createdAt : pr[field];
      const expected = prs.filter(pr => pr.state === "open").sort((a, b) => {
        const aKey = key(a), bKey = key(b);
        const order = typeof aKey === "number" && typeof bKey === "number" ? aKey - bKey : String(aKey).localeCompare(String(bKey));
        return (direction === "asc" ? order : -order) || a.id.localeCompare(b.id);
      });
      assert.deepEqual([...first.pullRequests, ...second.pullRequests].map(pr => pr.id), expected.map(pr => pr.id));
      await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, first.nextCursor, { ...filter, sortDirection: direction === "asc" ? "desc" : "asc" }), code("invalid_cursor"));
    }
  }
  const implicit = await store.evidence(board.boardId, board.snapshotId, undefined, undefined, { kind: "pull_requests", pullRequestState: "open" });
  assert.ok(implicit.nextCursor);
  await store.evidence(board.boardId, board.snapshotId, undefined, implicit.nextCursor, { kind: "pull_requests", pullRequestState: "open", pullRequestSort: "updated", sortDirection: "desc" });
  await assert.rejects(store.evidence(board.boardId, board.snapshotId, undefined, implicit.nextCursor, { kind: "pull_requests", pullRequestState: "open", pullRequestSort: "title" }), code("invalid_cursor"));
});
