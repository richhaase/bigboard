import { randomUUID } from "node:crypto";
import { mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { collectGitHistory, describeRepository, importNativeSources } from "./sources/git";
import { collectGitHub } from "./sources/github";
import type { ActivityRecord, BoardQuery, BoardSnapshot, CollectedWorkspace, ConfiguredRepository, EvidenceFilter, EvidenceResult, GitCollectionResult, GitHubCollectionResult, RepositoryCoverage, SourceConfiguration } from "./shared";

export type { EvidenceResult } from "./shared";
const DAY = 86_400_000;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const DEFAULT_QUERY: BoardQuery = { days: 7, repository: "all", compare: false };

export class BoardError extends Error {
  constructor(public readonly code: string, message: string) { super(message); this.name = "BoardError"; }
}
interface SavedBoard { version: 2; boardId: string; revision: number; updatedAt: string; query: BoardQuery }
interface Dependencies {
  now?: () => Date;
  describeRepository?: typeof describeRepository;
  importNativeSources?: typeof importNativeSources;
  collectGitHistory?: typeof collectGitHistory;
  collectGitHub?: typeof collectGitHub;
}
export interface RefreshResult { collectionId: string; collectedAt: string; configurationRevision: number; repositoryCount: number; complete: boolean; note: string }

function stateDirectory(): string {
  const configuredBase = process.env.XDG_STATE_HOME;
  const base = configuredBase && isAbsolute(configuredBase) ? configuredBase : join(homedir(), ".local", "state");
  return process.env.BIGBOARD_STATE_DIR || join(base, "bigboard-mcp-app");
}
function validateQuery(value: unknown): BoardQuery {
  const query = value as Partial<BoardQuery> | null;
  if (!query || (query.days !== 7 && query.days !== 14) || typeof query.repository !== "string" || !query.repository || typeof query.compare !== "boolean") {
    throw new BoardError("invalid_query", "Use days 7 or 14, a configured repository or all, and a boolean comparison setting.");
  }
  return { days: query.days, repository: query.repository, compare: query.compare };
}
function blankGitHub(repositories: ConfiguredRepository[], message: string): GitHubCollectionResult {
  return { account: null, repositories: repositories.map(repo => ({ id: repo.id, openComplete: false, mergedComplete: false, accessDenied: false, error: message })), pullRequests: [], error: message };
}

export class BoardStore {
  private database?: DatabaseSync;
  private refreshing = false;
  private verifiedCollection: string | null = null;
  private verifiedEpoch: number | null = null;
  private deps: Required<Dependencies>;

  constructor(public readonly directory = stateDirectory(), dependencies: Dependencies = {}) {
    this.deps = { now: () => new Date(), describeRepository, importNativeSources, collectGitHistory, collectGitHub, ...dependencies };
  }
  private db(): DatabaseSync {
    if (!this.database) {
      mkdirSync(this.directory, { recursive: true, mode: 0o700 });
      this.database = new DatabaseSync(join(this.directory, "boards.sqlite"));
      this.database.exec("PRAGMA busy_timeout = 2000; CREATE TABLE IF NOT EXISTS boards (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state TEXT NOT NULL); CREATE TABLE IF NOT EXISTS workspace (id INTEGER PRIMARY KEY CHECK (id = 1), revision INTEGER NOT NULL, generation INTEGER NOT NULL, verification INTEGER NOT NULL DEFAULT 0, configuration TEXT NOT NULL, collection TEXT)");
      this.database.prepare("INSERT OR IGNORE INTO workspace (id,revision,generation,configuration) VALUES (1,0,0,?)").run(JSON.stringify({ revision: 0, repositories: [] }));
    }
    return this.database;
  }
  sources(): SourceConfiguration {
    const row = this.db().prepare("SELECT configuration FROM workspace WHERE id = 1").get()!;
    return JSON.parse(String(row.configuration)) as SourceConfiguration;
  }
  async configure(paths: string[] | undefined, importNative: boolean, expectedRevision?: number): Promise<SourceConfiguration> {
    const current = this.sources();
    if (expectedRevision !== undefined && expectedRevision !== current.revision) throw new BoardError("configuration_conflict", "Source settings changed. Call list_sources before updating them.");
    if (importNative === (paths !== undefined)) throw new BoardError("invalid_sources", "Provide paths or import_native, choosing one source of repository selection.");
    let repositories: ConfiguredRepository[];
    const signal = AbortSignal.timeout(45_000);
    try {
      if (importNative) repositories = await this.deps.importNativeSources(signal);
      else {
        repositories = [];
        for (let offset = 0; offset < paths!.length; offset += 4) {
          signal.throwIfAborted();
          repositories.push(...await Promise.all(paths!.slice(offset,offset+4).map(path => this.deps.describeRepository(path,signal))));
        }
      }
    } catch (error) {
      throw new BoardError("invalid_sources", signal.aborted ? "Repository selection import timed out. Configure explicit clone paths or narrow the native selection." : error instanceof Error ? error.message : "The repository selection could not be imported.");
    }
    const unique = new Map<string, ConfiguredRepository>();
    for (const repo of repositories) if (!unique.has(repo.id)) unique.set(repo.id, repo);
    if (unique.size > 100) throw new BoardError("too_many_sources", "Select at most 100 repositories per workspace.");
    const configuration: SourceConfiguration = { revision: current.revision + 1, repositories: [...unique.values()].sort((a,b) => a.id.localeCompare(b.id)) };
    this.db().exec("BEGIN IMMEDIATE");
    try {
      const result = this.db().prepare("UPDATE workspace SET revision=?,generation=generation+1,configuration=?,collection=NULL WHERE id=1 AND revision=?")
        .run(configuration.revision, JSON.stringify(configuration), current.revision);
      if (Number(result.changes) !== 1) throw new BoardError("configuration_conflict", "Source settings changed. Call list_sources before updating them.");
      const retained = new Set(configuration.repositories.map(repo => repo.id));
      for (const row of this.db().prepare("SELECT id,state FROM boards").all()) {
        try {
          const saved = JSON.parse(String(row.state));
          if (saved.query.repository !== "all" && !retained.has(saved.query.repository)) {
            saved.query.repository = "all"; saved.revision += 1; saved.updatedAt = this.deps.now().toISOString();
            this.db().prepare("UPDATE boards SET revision=?,state=? WHERE id=?").run(saved.revision, JSON.stringify(saved), String(row.id));
          }
        } catch {}
      }
      this.db().exec("COMMIT");
    } catch (error) { this.db().exec("ROLLBACK"); throw error; }
    this.verifiedCollection = null;
    return configuration;
  }
  async refresh(signal?: AbortSignal): Promise<RefreshResult> {
    if (this.refreshing) throw new BoardError("refresh_in_progress", "A source refresh is already running. Read the board after it finishes.");
    const configuration = this.sources();
    if (!configuration.repositories.length) throw new BoardError("sources_required", "Configure repository paths or import the native BigBoard selection before refreshing.");
    const started = this.db().prepare("UPDATE workspace SET verification=verification+1 WHERE id=1 AND revision=? RETURNING generation,verification").get(configuration.revision);
    if (!started) throw new BoardError("configuration_conflict", "Source settings changed. Read the settings and retry refresh.");
    const generation = Number(started.generation);
    const verification = Number(started.verification);
    this.verifiedCollection = null;
    this.verifiedEpoch = null;
    const until = this.deps.now().toISOString();
    const since = new Date(Date.parse(until) - 28 * DAY).toISOString();
    const boundedSignal = signal ? AbortSignal.any([signal, AbortSignal.timeout(150_000)]) : AbortSignal.timeout(150_000);
    this.refreshing = true;
    try {
      const git: GitCollectionResult[] = [];
      const queue = [...configuration.repositories];
      const githubPromise = this.deps.collectGitHub(configuration.repositories, { since, until, signal: boundedSignal }).catch(() => blankGitHub(configuration.repositories, "GitHub collection could not complete. Refresh to retry."));
      await Promise.all(Array.from({ length: Math.min(4, queue.length) }, async () => {
        for (let repo = queue.shift(); repo; repo = queue.shift()) {
          boundedSignal.throwIfAborted();
          try { git.push(await this.deps.collectGitHistory(repo, { since, until, signal: boundedSignal })); }
          catch { git.push({ repository: repo, revision: null, ref: `refs/remotes/${repo.remote}/${repo.defaultBranch}`, shallow: false, partial: false, complete: false, linesComplete: false, records: [], error: "Git history could not be read. Check the selected clone and default remote branch." }); }
        }
      }));
      const github = await githubPromise;
      boundedSignal.throwIfAborted();
      const collection: CollectedWorkspace = { id: randomUUID(), configRevision: configuration.revision, generatedAt: this.deps.now().toISOString(), since, until, git: git.sort((a,b) => a.repository.id.localeCompare(b.repository.id)), github };
      const result = this.db().prepare("UPDATE workspace SET collection=?,generation=generation+1 WHERE id=1 AND revision=? AND generation=? AND verification=?")
        .run(JSON.stringify(collection), configuration.revision, generation, verification);
      if (Number(result.changes) !== 1) throw new BoardError("refresh_superseded", "Source settings or collected data changed while refreshing. Read the board and refresh again if needed.");
      this.verifiedCollection = collection.id;
      this.verifiedEpoch = verification;
      const coverage = this.coverage(configuration.repositories, collection, true);
      const complete = coverage.every(repo => repo.historyComplete && repo.linesComplete && repo.openPRsComplete && repo.mergedPRsComplete);
      return { collectionId: collection.id, collectedAt: collection.generatedAt, configurationRevision: configuration.revision, repositoryCount: configuration.repositories.length, complete, note: complete ? "Git history and GitHub PR collection completed." : "Collection completed with coverage gaps; inspect the board's repository coverage." };
    } catch (error) {
      if (error instanceof BoardError) throw error;
      throw new BoardError("refresh_cancelled", "Source refresh was cancelled or timed out. Previous Git evidence remains available; refresh again to verify GitHub access.");
    } finally { this.refreshing = false; }
  }
  private workspace(): { configuration: SourceConfiguration; collection: CollectedWorkspace | null; verification: number } {
    const row = this.db().prepare("SELECT configuration,collection,verification FROM workspace WHERE id=1").get()!;
    try {
      return { verification: Number(row.verification), configuration: JSON.parse(String(row.configuration)), collection: row.collection === null ? null : JSON.parse(String(row.collection)) };
    } catch { throw new BoardError("collection_unreadable", "The collected evidence could not be read. Reconfigure sources to rebuild it."); }
  }
  private load(boardId: string): SavedBoard {
    if (!UUID.test(boardId)) throw new BoardError("invalid_board_id", "Use the boardId returned by open_board.");
    const row = this.db().prepare("SELECT revision,state FROM boards WHERE id=?").get(boardId);
    if (!row) throw new BoardError("board_not_found", "This board was not found. Call open_board without an ID to create one.");
    try {
      const saved = JSON.parse(String(row.state));
      if ((saved.version !== 1 && saved.version !== 2) || saved.boardId !== boardId || !Number.isSafeInteger(saved.revision) || saved.revision < 1 || saved.revision !== row.revision || !Number.isFinite(Date.parse(saved.updatedAt))) throw new Error();
      if (saved.version === 1) {
        saved.version = 2; saved.query.repository = "all"; saved.revision += 1;
        saved.updatedAt = this.deps.now().toISOString();
        this.db().prepare("UPDATE boards SET revision=?,state=? WHERE id=? AND revision=?").run(saved.revision, JSON.stringify(saved), boardId, row.revision);
        return this.load(boardId);
      }
      return { ...saved, query: validateQuery(saved.query) };
    } catch { throw new BoardError("board_unreadable", "The saved board could not be read. Create a new board to continue."); }
  }
  private coverage(repos: ConfiguredRepository[], collection: CollectedWorkspace | null, verified: boolean): RepositoryCoverage[] {
    return repos.map(repo => {
      const git = collection?.git.find(value => value.repository.id === repo.id);
      const github = collection?.github.repositories.find(value => value.id === repo.id);
      return { repository: repo.id, revision: git?.revision ?? null, ref: git?.ref ?? `refs/remotes/${repo.remote}/${repo.defaultBranch}`, shallow: git?.shallow ?? false, partial: git?.partial ?? false, historyComplete: git?.complete ?? false, linesComplete: git?.linesComplete ?? false, openPRsComplete: verified && (github?.openComplete ?? false), mergedPRsComplete: verified && (github?.mergedComplete ?? false), errors: [git?.error, verified ? github?.error : collection ? "Refresh sources to verify cached GitHub access in this session." : "Sources have not been collected.", verified ? collection?.github.error : null].filter((value): value is string => Boolean(value)) };
    });
  }
  private snapshot(saved: SavedBoard, workspace = this.workspace()): BoardSnapshot {
    const config = workspace.configuration;
    const collection = workspace.collection;
    const until = collection?.until ?? saved.updatedAt;
    const since = new Date(Date.parse(until) - saved.query.days * DAY).toISOString();
    const previousSince = new Date(Date.parse(since) - saved.query.days * DAY).toISOString();
    const repos = config.repositories.filter(repo => saved.query.repository === "all" || repo.id === saved.query.repository);
    const ids = new Set(repos.map(repo => repo.id));
    const verified = collection?.id === this.verifiedCollection && workspace.verification === this.verifiedEpoch;
    const coverage = this.coverage(repos, collection, verified);
    const records = (collection?.git ?? []).filter(value => ids.has(value.repository.id)).flatMap(value => value.records);
    const current = records.filter(record => record.timestamp >= since && record.timestamp < until);
    const priorCovered = Boolean(collection && collection.since <= previousSince && repos.length && coverage.every(repo => repo.historyComplete));
    const previous = saved.query.compare && priorCovered ? records.filter(record => record.timestamp >= previousSince && record.timestamp < since) : null;
    const prs = verified ? collection!.github.pullRequests.filter(pr => ids.has(pr.repository)) : [];
    const merged = prs.filter(pr => pr.state === "merged" && pr.mergedAt !== null && pr.mergedAt >= since && pr.mergedAt < until);
    const complete = Boolean(collection && repos.length && coverage.every(repo => repo.historyComplete && repo.linesComplete && repo.openPRsComplete && repo.mergedPRsComplete));
    const buckets = Array.from({ length: saved.query.days }, (_, index) => {
      const start = new Date(Date.parse(since) + index * DAY).toISOString();
      const end = new Date(Date.parse(start) + DAY).toISOString();
      const selected = current.filter(record => record.timestamp >= start && record.timestamp < end);
      const priorStart = new Date(Date.parse(start) - saved.query.days * DAY).toISOString();
      const priorEnd = new Date(Date.parse(end) - saved.query.days * DAY).toISOString();
      return { start, end, commits: selected.length, additions: selected.reduce((sum,r) => sum + (r.additions ?? 0),0), deletions: selected.reduce((sum,r) => sum + (r.deletions ?? 0),0), unknownLineCommits: selected.filter(r => r.additions === null || r.deletions === null).length, previousCommits: previous?.filter(r => r.timestamp >= priorStart && r.timestamp < priorEnd).length ?? null, mergedPRs: merged.filter(pr => pr.mergedAt! >= start && pr.mergedAt! < end).length, recordIds: selected.slice(0,30).map(r => r.id), recordIdsTruncated: selected.length > 30 };
    });
    const needsRefresh = !collection || collection.configRevision !== config.revision || !verified;
    const note = !config.repositories.length ? "Configure repository paths or import the native BigBoard selection, then refresh sources." : !collection ? "Sources are configured. Refresh sources to collect Git history and GitHub PRs." : `Git author-date activity from local remote-tracking default branches; GitHub PRs as of ${collection.generatedAt}. Local clones are read without fetching. ${complete ? "Coverage is complete for this collection." : "Coverage is incomplete; zero values do not establish absence of activity."}${!verified ? " Refresh to verify GitHub access for this session." : ""}`;
    return { boardId: saved.boardId, revision: saved.revision, snapshotId: `${collection?.id ?? "uncollected"}:${config.revision}:${verified ? "verified" : "unverified"}:${workspace.verification}:${saved.boardId}:${saved.revision}`, updatedAt: saved.updatedAt, query: saved.query, scope: { since, until, timezone: "UTC" }, coverage: { source: "git+github", complete, note, repositories: coverage }, configuration: { revision: config.revision, repositories: config.repositories.map(repo => repo.id), needsRefresh }, account: verified ? collection!.github.account : null, collectedAt: collection?.generatedAt ?? null, buckets, totals: { commits: current.length, additions: current.reduce((sum,r) => sum + (r.additions ?? 0),0), deletions: current.reduce((sum,r) => sum + (r.deletions ?? 0),0), unknownLineCommits: current.filter(r => r.additions === null || r.deletions === null).length, previousCommits: previous?.length ?? null, mergedPRs: merged.length, openPRs: prs.filter(pr => pr.state === "open").length } };
  }
  async open(boardId?: string): Promise<BoardSnapshot> {
    if (boardId !== undefined) return this.read(boardId);
    const saved: SavedBoard = { version: 2, boardId: randomUUID(), revision: 1, updatedAt: this.deps.now().toISOString(), query: { ...DEFAULT_QUERY } };
    this.db().prepare("INSERT INTO boards (id,revision,state) VALUES (?,?,?)").run(saved.boardId,saved.revision,JSON.stringify(saved));
    return this.snapshot(saved);
  }
  async read(boardId: string): Promise<BoardSnapshot> { return this.snapshot(this.load(boardId)); }
  async update(boardId: string, expectedRevision: number, patch: Partial<BoardQuery>): Promise<BoardSnapshot> {
    if (!Number.isSafeInteger(expectedRevision) || expectedRevision < 1) throw new BoardError("invalid_revision", "Use the revision returned by read_board.");
    const saved = this.load(boardId);
    if (saved.revision !== expectedRevision) this.conflict(saved.revision);
    const query = validateQuery({ ...saved.query, ...patch });
    if (query.repository !== "all" && !this.sources().repositories.some(repo => repo.id === query.repository)) throw new BoardError("repository_not_configured", "Choose a repository returned by list_sources.");
    if (JSON.stringify(query) === JSON.stringify(saved.query)) return this.snapshot(saved);
    const next: SavedBoard = { ...saved,query,revision:saved.revision+1,updatedAt:this.deps.now().toISOString() };
    const changed = this.db().prepare("UPDATE boards SET revision=?,state=? WHERE id=? AND revision=?").run(next.revision,JSON.stringify(next),boardId,expectedRevision);
    if (Number(changed.changes) !== 1) this.conflict(this.load(boardId).revision);
    return this.snapshot(next);
  }
  private conflict(revision: number): never { throw new BoardError("revision_conflict", `Board revision ${revision} is current. Call read_board, then retry with that revision.`); }
  async evidence(boardId: string, snapshotId: string, bucketStart?: string, cursor?: string, filter: EvidenceFilter = {}): Promise<EvidenceResult> {
    const workspace = this.workspace();
    const board = this.snapshot(this.load(boardId),workspace);
    if (board.snapshotId !== snapshotId) throw new BoardError("stale_snapshot", "The selected evidence changed. Read the board and select the current snapshot.");
    const bucket = bucketStart !== undefined ? board.buckets.find(value => value.start === bucketStart) : undefined;
    if (bucketStart !== undefined && !bucket) throw new BoardError("invalid_selection", "The selected bucket is outside this board's scope.");
    if ((filter.kind !== undefined && !["all", "activity", "pull_requests"].includes(filter.kind)) ||
        (filter.pullRequestState !== undefined && !["open", "merged"].includes(filter.pullRequestState)) ||
        (filter.pullRequestId !== undefined && (typeof filter.pullRequestId !== "string" || filter.pullRequestId.length === 0 || filter.pullRequestId.length > 256))) {
      throw new BoardError("invalid_filter", "Choose activity or pull_requests evidence, an open or merged PR state, and an exact returned PR ID.");
    }
    if (filter.kind === "activity" && (filter.pullRequestState !== undefined || filter.pullRequestId !== undefined)) {
      throw new BoardError("invalid_filter", "Activity evidence cannot include pull request filters.");
    }
    const kind = filter.pullRequestId !== undefined ? "pull_requests" : filter.kind ?? (filter.pullRequestState === undefined ? "all" : "pull_requests");
    const filterKey = JSON.stringify([kind, filter.pullRequestState ?? null, filter.pullRequestId ?? null]);
    const collection = workspace.collection;
    const since = bucket?.start ?? board.scope.since;
    const until = bucket?.end ?? board.scope.until;
    const selected = (id:string) => board.configuration.repositories.includes(id) && (board.query.repository === "all" || board.query.repository === id);
    const records = kind === "pull_requests" ? [] : (collection?.git ?? []).filter(repo => selected(repo.repository.id)).flatMap(repo => repo.records).filter(r => r.timestamp >= since && r.timestamp < until).sort((a,b) => b.timestamp.localeCompare(a.timestamp) || a.id.localeCompare(b.id));
    const pullRequests = kind !== "activity" && collection?.id === this.verifiedCollection && workspace.verification === this.verifiedEpoch
      ? collection.github.pullRequests.filter(pr => selected(pr.repository) &&
        (filter.pullRequestState === undefined || pr.state === filter.pullRequestState) &&
        (filter.pullRequestId === undefined || pr.id === filter.pullRequestId) &&
        (pr.state === "open" || (pr.mergedAt !== null && pr.mergedAt >= since && pr.mergedAt < until)))
        .sort((a,b) => b.updatedAt.localeCompare(a.updatedAt) || a.id.localeCompare(b.id)) : [];
    if (filter.pullRequestId !== undefined && pullRequests.length === 0) {
      throw new BoardError("selection_not_found", "The selected pull request is unavailable in this board's current repository, date, state, or verified account scope. Refresh the selection before discussing it.");
    }
    let offset = 0;
    if (cursor !== undefined) {
      try {
        if (cursor.length > 1024 || !/^[a-zA-Z0-9_-]+$/.test(cursor)) throw new Error();
        const decoded = JSON.parse(Buffer.from(cursor,"base64url").toString("utf8"));
        if (!Array.isArray(decoded) || decoded.length !== 4 || decoded[0] !== snapshotId || decoded[1] !== (bucketStart ?? null) || decoded[2] !== filterKey || !Number.isSafeInteger(decoded[3]) || decoded[3] < 0 || decoded[3] > Math.max(records.length,pullRequests.length)) throw new Error();
        offset = decoded[3];
      } catch { throw new BoardError("invalid_cursor", "Use the nextCursor returned for this snapshot, selection, and evidence filters."); }
    }
    const pageSize = 25;
    const more = offset+pageSize < Math.max(records.length,pullRequests.length);
    return { boardId, revision:board.revision,snapshotId,query:board.query,scope:board.scope,coverage:board.coverage,bucketStart:bucketStart ?? null,records:records.slice(offset,offset+pageSize).map(record => ({ ...record, files: record.files.slice(0,25), filesTruncated: record.files.length > 25 })),pullRequests:pullRequests.slice(offset,offset+pageSize),nextCursor:more ? Buffer.from(JSON.stringify([snapshotId,bucketStart ?? null,filterKey,offset+pageSize])).toString("base64url") : null,totalRecords:records.length,totalPullRequests:pullRequests.length,kind,pullRequestState:filter.pullRequestState,selectedPullRequestId:filter.pullRequestId };
  }
  close(): void { this.database?.close(); this.database=undefined; this.verifiedCollection=null; this.verifiedEpoch=null; }
}
