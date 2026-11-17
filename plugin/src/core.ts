import { randomUUID } from "node:crypto";
import { mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import {
  REPOSITORIES,
  type ActivityRecord,
  type BoardQuery,
  type BoardSnapshot,
} from "./shared";

const DAY = 86_400_000;
const FIXTURE_END = Date.parse("2026-11-16T00:00:00.000Z");
const FIXTURE_START = FIXTURE_END - 14 * DAY;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const DEFAULT_QUERY: BoardQuery = { days: 7, repository: "all", compare: false };
const COVERAGE_NOTE = "Synthetic fixture records for interaction testing; no live repositories were queried.";

export class BoardError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "BoardError";
  }
}

interface SavedBoard {
  version: 1;
  boardId: string;
  revision: number;
  updatedAt: string;
  query: BoardQuery;
}

export interface EvidenceResult {
  boardId: string;
  revision: number;
  snapshotId: string;
  query: BoardQuery;
  scope: BoardSnapshot["scope"];
  coverage: BoardSnapshot["coverage"];
  bucketStart: string | null;
  records: ActivityRecord[];
}

const subjects = [
  "Adjust request validation",
  "Refine activity filters",
  "Update status rendering",
  "Add pagination coverage",
  "Simplify query handling",
  "Improve empty-state copy",
];

function fixtureRecords(): ActivityRecord[] {
  const counts = {
    "sample/api": [1, 2, 1, 3, 1, 2, 1, 2, 1, 2, 6, 3, 2, 1],
    "sample/web": [1, 1, 2, 1, 2, 1, 1, 1, 2, 1, 2, 1, 3, 2],
  } as const;
  return Object.entries(counts).flatMap(([repository, days]) =>
    days.flatMap((count, day) =>
      Array.from({ length: count }, (_, index) => ({
        id: `fixture-${repository.split("/")[1]}-${day + 1}-${index + 1}`,
        repository: repository as ActivityRecord["repository"],
        timestamp: new Date(FIXTURE_START + day * DAY + (9 + index) * 3_600_000).toISOString(),
        subject: subjects[(day + index) % subjects.length],
        contributor: (day + index) % 2 === 0 ? "Contributor A" : "Contributor B",
        additions: 12 + ((day * 7 + index * 13) % 94),
        deletions: 3 + ((day * 5 + index * 3) % 31),
      })),
    ),
  );
}

export const FIXTURE_RECORDS: readonly ActivityRecord[] = fixtureRecords();

function stateDirectory(): string {
  const configuredBase = process.env.XDG_STATE_HOME;
  const base = configuredBase && isAbsolute(configuredBase) ? configuredBase : join(homedir(), ".local", "state");
  return process.env.BIGBOARD_STATE_DIR || join(base, "bigboard-mcp-app");
}

function validateQuery(value: unknown): BoardQuery {
  if (!value || typeof value !== "object") {
    throw new BoardError("invalid_query", "The board query is invalid.");
  }
  const query = value as Record<string, unknown>;
  if ((query.days !== 7 && query.days !== 14) ||
      !REPOSITORIES.includes(query.repository as BoardQuery["repository"]) ||
      typeof query.compare !== "boolean") {
    throw new BoardError("invalid_query", "Use days 7 or 14, a fixture repository, and a boolean comparison setting.");
  }
  return { days: query.days, repository: query.repository as BoardQuery["repository"], compare: query.compare };
}

function recordsFor(query: BoardQuery, since: number, until: number): ActivityRecord[] {
  return FIXTURE_RECORDS.filter((record) =>
    (query.repository === "all" || record.repository === query.repository) &&
    Date.parse(record.timestamp) >= since && Date.parse(record.timestamp) < until,
  ).map((record) => ({ ...record }));
}

function snapshot(saved: SavedBoard): BoardSnapshot {
  const since = FIXTURE_END - saved.query.days * DAY;
  const previousSince = since - saved.query.days * DAY;
  const comparisonComplete = !saved.query.compare || previousSince >= FIXTURE_START;
  const current = recordsFor(saved.query, since, FIXTURE_END);
  const previous = saved.query.compare && comparisonComplete
    ? recordsFor(saved.query, previousSince, since) : null;
  const buckets = Array.from({ length: saved.query.days }, (_, day) => {
    const start = since + day * DAY;
    const records = current.filter((record) => Date.parse(record.timestamp) >= start && Date.parse(record.timestamp) < start + DAY);
    const previousStart = previousSince + day * DAY;
    return {
      start: new Date(start).toISOString(),
      end: new Date(start + DAY).toISOString(),
      commits: records.length,
      additions: records.reduce((sum, record) => sum + record.additions, 0),
      deletions: records.reduce((sum, record) => sum + record.deletions, 0),
      previousCommits: previous?.filter((record) =>
        Date.parse(record.timestamp) >= previousStart && Date.parse(record.timestamp) < previousStart + DAY,
      ).length ?? null,
      recordIds: records.map((record) => record.id),
    };
  });
  return {
    boardId: saved.boardId,
    revision: saved.revision,
    snapshotId: `fixture-v1:${saved.boardId}:${saved.revision}`,
    updatedAt: saved.updatedAt,
    query: { ...saved.query },
    scope: { since: new Date(since).toISOString(), until: new Date(FIXTURE_END).toISOString(), timezone: "UTC" },
    coverage: {
      source: "fixture",
      complete: comparisonComplete,
      note: comparisonComplete ? COVERAGE_NOTE : `${COVERAGE_NOTE} The prior 14-day comparison is unavailable; its counts are unknown.`,
    },
    buckets,
    totals: {
      commits: current.length,
      additions: current.reduce((sum, record) => sum + record.additions, 0),
      deletions: current.reduce((sum, record) => sum + record.deletions, 0),
      previousCommits: previous?.length ?? null,
    },
  };
}

export class BoardStore {
  private database?: DatabaseSync;

  constructor(public readonly directory = stateDirectory()) {}

  private db(): DatabaseSync {
    if (!this.database) {
      mkdirSync(this.directory, { recursive: true, mode: 0o700 });
      this.database = new DatabaseSync(join(this.directory, "boards.sqlite"));
      this.database.exec("PRAGMA busy_timeout = 2000; CREATE TABLE IF NOT EXISTS boards (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state TEXT NOT NULL)");
    }
    return this.database;
  }

  private load(boardId: string): SavedBoard {
    if (!UUID.test(boardId)) {
      throw new BoardError("invalid_board_id", "Use the boardId returned by open_board.");
    }
    const row = this.db().prepare("SELECT revision, state FROM boards WHERE id = ?").get(boardId);
    if (!row) throw new BoardError("board_not_found", "This board was not found. Call open_board without an ID to create one.");
    let parsed: unknown;
    try {
      parsed = JSON.parse(String(row.state));
    } catch {
      throw new BoardError("board_unreadable", "The saved board could not be read. Create a new board to continue.");
    }
    if (!parsed || typeof parsed !== "object") throw new BoardError("board_unreadable", "The saved board is invalid. Create a new board to continue.");
    const saved = parsed as Record<string, unknown>;
    if (saved.version !== 1 || saved.boardId !== boardId || !Number.isSafeInteger(saved.revision) ||
        (saved.revision as number) < 1 || saved.revision !== row.revision ||
        typeof saved.updatedAt !== "string" || !Number.isFinite(Date.parse(saved.updatedAt))) {
      throw new BoardError("board_unreadable", "The saved board is invalid. Create a new board to continue.");
    }
    try {
      return {
        version: 1,
        boardId,
        revision: saved.revision as number,
        updatedAt: saved.updatedAt,
        query: validateQuery(saved.query),
      };
    } catch {
      throw new BoardError("board_unreadable", "The saved board query is invalid. Create a new board to continue.");
    }
  }

  async open(boardId?: string): Promise<BoardSnapshot> {
    if (boardId !== undefined) return this.read(boardId);
    const saved: SavedBoard = {
      version: 1,
      boardId: randomUUID(),
      revision: 1,
      updatedAt: new Date().toISOString(),
      query: { ...DEFAULT_QUERY },
    };
    this.db().prepare("INSERT INTO boards (id, revision, state) VALUES (?, ?, ?)")
      .run(saved.boardId, saved.revision, JSON.stringify(saved));
    return snapshot(saved);
  }

  async read(boardId: string): Promise<BoardSnapshot> {
    return snapshot(this.load(boardId));
  }

  async update(boardId: string, expectedRevision: number, patch: Partial<BoardQuery>): Promise<BoardSnapshot> {
    if (!Number.isSafeInteger(expectedRevision) || expectedRevision < 1) {
      throw new BoardError("invalid_revision", "Use the revision returned by read_board.");
    }
    const saved = this.load(boardId);
    if (saved.revision !== expectedRevision) this.conflict(saved.revision);
    const query = validateQuery({ ...saved.query, ...patch });
    if (JSON.stringify(query) === JSON.stringify(saved.query)) return snapshot(saved);
    const next: SavedBoard = { ...saved, query, revision: saved.revision + 1, updatedAt: new Date().toISOString() };
    const result = this.db().prepare("UPDATE boards SET revision = ?, state = ? WHERE id = ? AND revision = ?")
      .run(next.revision, JSON.stringify(next), boardId, expectedRevision);
    if (Number(result.changes) !== 1) this.conflict(this.load(boardId).revision);
    return snapshot(next);
  }

  private conflict(revision: number): never {
    throw new BoardError("revision_conflict", `Board revision ${revision} is current. Call read_board, then retry with that revision.`);
  }

  async evidence(boardId: string, snapshotId: string, bucketStart?: string): Promise<EvidenceResult> {
    const board = await this.read(boardId);
    if (board.snapshotId !== snapshotId) {
      throw new BoardError("stale_snapshot", "The selection belongs to an older board revision. Read the board and select the current evidence.");
    }
    const bucket = bucketStart !== undefined ? board.buckets.find((candidate) => candidate.start === bucketStart) : undefined;
    if (bucketStart !== undefined && !bucket) {
      throw new BoardError("invalid_selection", "The selected bucket is outside this board's current scope.");
    }
    return {
      boardId: board.boardId,
      revision: board.revision,
      snapshotId: board.snapshotId,
      query: board.query,
      scope: board.scope,
      coverage: board.coverage,
      bucketStart: bucketStart ?? null,
      records: recordsFor(board.query, Date.parse(bucket?.start ?? board.scope.since), Date.parse(bucket?.end ?? board.scope.until)),
    };
  }

  close(): void {
    this.database?.close();
    this.database = undefined;
  }
}
