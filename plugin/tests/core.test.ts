import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { BoardError, BoardStore, FIXTURE_RECORDS } from "../src/core";

async function withStore(run: (store: BoardStore, directory: string) => Promise<void>): Promise<void> {
  const directory = await mkdtemp(join(tmpdir(), "bigboard-core-"));
  const store = new BoardStore(directory);
  try {
    await run(store, directory);
  } finally {
    store.close();
    await rm(directory, { recursive: true, force: true });
  }
}

const errorCode = (code: string) => (error: unknown): boolean => error instanceof BoardError && error.code === code;

test("empty or relative XDG environment values use the standard state location", () => {
  const previousBase = process.env.XDG_STATE_HOME;
  const previousOverride = process.env.BIGBOARD_STATE_DIR;
  try {
    process.env.BIGBOARD_STATE_DIR = "";
    for (const base of ["", "relative-state"]) {
      process.env.XDG_STATE_HOME = base;
      assert.equal(new BoardStore().directory, join(homedir(), ".local", "state", "bigboard-mcp-app"));
    }
    process.env.XDG_STATE_HOME = join(tmpdir(), "xdg-state");
    assert.equal(new BoardStore().directory, join(tmpdir(), "xdg-state", "bigboard-mcp-app"));
  } finally {
    if (previousBase === undefined) delete process.env.XDG_STATE_HOME;
    else process.env.XDG_STATE_HOME = previousBase;
    if (previousOverride === undefined) delete process.env.BIGBOARD_STATE_DIR;
    else process.env.BIGBOARD_STATE_DIR = previousOverride;
  }
});

test("default board resolves a fixed seven-day half-open fixture interval", async () => {
  await withStore(async (store) => {
    const board = await store.open();
    assert.deepEqual(board.query, { days: 7, repository: "all", compare: false });
    assert.deepEqual(board.scope, { since: "2026-11-09T00:00:00.000Z", until: "2026-11-16T00:00:00.000Z", timezone: "UTC" });
    assert.equal(board.buckets.length, 7);
    assert.equal(board.buckets[0].start, board.scope.since);
    assert.equal(board.buckets.at(-1)?.end, board.scope.until);
    const evidence = await store.evidence(board.boardId, board.snapshotId);
    assert.ok(evidence.records.length > 0);
    assert.ok(evidence.records.every((record) => record.timestamp >= board.scope.since && record.timestamp < board.scope.until));
    assert.ok(evidence.records.some((record) => record.id === "fixture-api-8-1"));
    assert.ok(evidence.records.every((record) => record.id !== "fixture-api-7-1"));
    assert.equal(board.totals.commits, evidence.records.length);
    assert.equal(board.buckets.reduce((sum, bucket) => sum + bucket.commits, 0), evidence.records.length);
    assert.equal(board.coverage.source, "fixture");
    assert.match(board.coverage.note, /Synthetic/);
  });
});

test("repository filters and prior-period comparison use the same evidence", async () => {
  await withStore(async (store) => {
    const initial = await store.open();
    const board = await store.update(initial.boardId, initial.revision, { repository: "sample/api", compare: true });
    const evidence = await store.evidence(board.boardId, board.snapshotId);
    const prior = FIXTURE_RECORDS.filter((record) => record.repository === "sample/api" && record.timestamp < board.scope.since);
    assert.ok(evidence.records.every((record) => record.repository === "sample/api"));
    assert.equal(board.totals.previousCommits, prior.length);
    assert.equal(board.buckets.reduce((sum, bucket) => sum + (bucket.previousCommits ?? 0), 0), prior.length);
    assert.equal(board.coverage.complete, true);
    const bucket = board.buckets[3];
    const selected = await store.evidence(board.boardId, board.snapshotId, bucket.start);
    assert.deepEqual(selected.records.map((record) => record.id), bucket.recordIds);
    assert.equal(selected.records.length, bucket.commits);
    assert.ok(selected.records.every((record) => record.timestamp >= bucket.start && record.timestamp < bucket.end));
  });
});

test("unavailable comparison remains unknown rather than zero", async () => {
  await withStore(async (store) => {
    const initial = await store.open();
    const board = await store.update(initial.boardId, initial.revision, { days: 14, compare: true });
    assert.equal(board.totals.commits, FIXTURE_RECORDS.length);
    assert.equal(board.totals.previousCommits, null);
    assert.equal(board.coverage.complete, false);
    assert.ok(board.buckets.every((bucket) => bucket.previousCommits === null));
    assert.match(board.coverage.note, /unavailable/);
  });
});

test("boards remain independent and survive another store instance and restart", async () => {
  await withStore(async (store, directory) => {
    const first = await store.open();
    const second = await store.open();
    const updated = await store.update(first.boardId, first.revision, { repository: "sample/web" });
    assert.notEqual(first.boardId, second.boardId);
    assert.deepEqual(await store.read(second.boardId), second);
    const another = new BoardStore(directory);
    try {
      assert.deepEqual(await another.open(first.boardId), updated);
      await assert.rejects(another.update(first.boardId, first.revision, { days: 14 }), errorCode("revision_conflict"));
      const next = await another.update(first.boardId, updated.revision, { days: 14 });
      assert.deepEqual(await store.read(first.boardId), next);
      store.close();
      assert.deepEqual(await store.open(first.boardId), next);
    } finally {
      another.close();
    }
  });
});

test("updates reject old revisions and evidence rejects old or foreign selections", async () => {
  await withStore(async (store) => {
    const first = await store.open();
    const second = await store.open();
    const updated = await store.update(first.boardId, first.revision, { compare: true });
    await assert.rejects(store.update(first.boardId, first.revision, { days: 14 }), errorCode("revision_conflict"));
    await assert.rejects(store.evidence(first.boardId, first.snapshotId), errorCode("stale_snapshot"));
    await assert.rejects(store.evidence(second.boardId, updated.snapshotId), errorCode("stale_snapshot"));
    await assert.rejects(store.evidence(first.boardId, updated.snapshotId, "2026-11-08T00:00:00.000Z"), errorCode("invalid_selection"));
    await assert.rejects(store.evidence(first.boardId, updated.snapshotId, ""), errorCode("invalid_selection"));
    assert.deepEqual(await store.read(first.boardId), updated);
  });
});

test("invalid identifiers and corrupt persisted state fail explicitly", async () => {
  await withStore(async (store, directory) => {
    await assert.rejects(store.read("../outside"), errorCode("invalid_board_id"));
    await assert.rejects(store.open(""), errorCode("invalid_board_id"));
    await assert.rejects(store.read("00000000-0000-4000-8000-000000000000"), errorCode("board_not_found"));
    const board = await store.open();
    await assert.rejects(store.update(board.boardId, 0, { days: 14 }), errorCode("invalid_revision"));
    const database = new DatabaseSync(join(directory, "boards.sqlite"));
    try {
      database.prepare("UPDATE boards SET state = ? WHERE id = ?").run("{broken", board.boardId);
      await assert.rejects(store.read(board.boardId), errorCode("board_unreadable"));
    } finally {
      database.close();
    }
  });
});

test("a no-op update preserves the revision and selection snapshot", async () => {
  await withStore(async (store) => {
    const board = await store.open();
    assert.deepEqual(await store.update(board.boardId, board.revision, { days: 7 }), board);
  });
});
