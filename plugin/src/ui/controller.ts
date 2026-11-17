import type { ActivityRecord, BoardQuery, BoardSnapshot, EvidenceResult, PullRequestRecord, SelectionContext } from "../shared";

export type BoardViewMode = "activity" | "pullRequests";
export type PullRequestState = "open" | "merged";

export interface ToolResult {
  isError?: boolean;
  content?: Array<{ type: string; text?: string }>;
  structuredContent?: unknown;
}

export interface BoardBridge {
  connect(onResult: (result: ToolResult) => void, onTeardown?: () => void): Promise<void>;
  callTool(name: string, args: Record<string, unknown>): Promise<ToolResult>;
  updateContext(context: SelectionContext): Promise<void>;
  sendMessage(text: string): Promise<void>;
  close(): Promise<void>;
}

export interface BoardViewState {
  snapshot: BoardSnapshot | null;
  view: BoardViewMode;
  pullRequestState: PullRequestState;
  selectedBucket: string | null;
  selectedPullRequestId: string | null;
  records: ActivityRecord[];
  pullRequests: PullRequestRecord[];
  totalRecords: number;
  totalPullRequests: number;
  nextCursor: string | null;
  phase: "connecting" | "waiting" | "ready";
  busy: boolean;
  sourceRefreshing: boolean;
  evidenceLoading: boolean;
  contextReady: boolean;
  contextFailed: boolean;
  error: string | null;
  notice: string | null;
}

export function selectionContext(snapshot: BoardSnapshot, bucketStart: string | null, view: BoardViewMode = "activity", pullRequestState: PullRequestState = "open", pullRequestId: string | null = null): SelectionContext {
  const bucket = view === "activity" ? snapshot.buckets.find((item) => item.start === bucketStart) : undefined;
  return {
    boardId: snapshot.boardId,
    revision: snapshot.revision,
    snapshotId: snapshot.snapshotId,
    query: snapshot.query,
    scope: snapshot.scope,
    view,
    ...(view === "pullRequests" ? { pullRequestState, pullRequestId } : {}),
    bucketStart: bucket?.start ?? null,
    recordIds: bucket?.recordIds.slice(0, 100) ?? [],
    recordIdsTruncated: bucket?.recordIdsTruncated === true || (bucket?.recordIds.length ?? 0) > 100,
  };
}

function toolError(result: ToolResult): void {
  if (result.isError) throw new Error(result.content?.find((item) => item.type === "text")?.text ?? "BigBoard could not complete the request.");
}

export function snapshotFromResult(result: ToolResult): BoardSnapshot {
  toolError(result);
  const value = result.structuredContent as BoardSnapshot | undefined;
  if (!value || typeof value.boardId !== "string" || typeof value.revision !== "number" ||
      typeof value.snapshotId !== "string" || !Array.isArray(value.buckets) || !value.query ||
      !value.scope || !value.coverage || !value.totals || !value.configuration) {
    throw new Error("BigBoard returned an unreadable board. Reopen it from the conversation.");
  }
  return value;
}

export class BoardController {
  private state: BoardViewState = {
    snapshot: null,
    view: "activity",
    pullRequestState: "open",
    selectedBucket: null,
    selectedPullRequestId: null,
    records: [],
    pullRequests: [],
    totalRecords: 0,
    totalPullRequests: 0,
    nextCursor: null,
    phase: "connecting",
    busy: false,
    sourceRefreshing: false,
    evidenceLoading: false,
    contextReady: false,
    contextFailed: false,
    error: null,
    notice: null,
  };
  private listeners = new Set<() => void>();
  private connected = false;
  private destroyed = false;
  private syncing = false;
  private evidenceSequence = 0;
  private contextSequence = 0;
  private contextQueue: Promise<void> = Promise.resolve();
  private poll: ReturnType<typeof setInterval> | null = null;
  private refreshError: string | null = null;

  constructor(private bridge: BoardBridge, private pollMilliseconds = 2000) {}

  getState = (): BoardViewState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  private setState(patch: Partial<BoardViewState>): void {
    if (this.destroyed) return;
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((listener) => listener());
  }

  private clearEvidence(patch: Partial<BoardViewState> = {}): void {
    this.evidenceSequence += 1;
    this.setState({ records: [], pullRequests: [], totalRecords: 0, totalPullRequests: 0, nextCursor: null, evidenceLoading: false, ...patch });
  }

  private context(): SelectionContext | null {
    const snapshot = this.state.snapshot;
    return snapshot ? selectionContext(snapshot, this.state.selectedBucket, this.state.view, this.state.pullRequestState, this.state.selectedPullRequestId) : null;
  }

  async start(): Promise<void> {
    try {
      await this.bridge.connect((result) => {
        try { this.acceptSnapshot(snapshotFromResult(result)); }
        catch (error) { this.setState({ error: this.errorMessage(error) }); }
      }, () => { void this.destroy(false); });
      if (this.destroyed) return;
      this.connected = true;
      if (this.state.snapshot) {
        await this.refresh();
        void this.loadEvidence();
        void this.shareSelection();
      } else this.setState({ phase: "waiting" });
      if (this.pollMilliseconds > 0) {
        this.poll = setInterval(() => {
          if (typeof document === "undefined" || document.visibilityState !== "hidden") void this.refresh();
        }, this.pollMilliseconds);
      }
    } catch {
      this.setState({ error: "BigBoard could not connect to this conversation. Reopen the board to try again." });
    }
  }

  acceptSnapshot(snapshot: BoardSnapshot): void {
    if (this.destroyed) return;
    const previous = this.state.snapshot;
    if (previous && (previous.boardId !== snapshot.boardId || snapshot.revision < previous.revision || snapshot.snapshotId === previous.snapshotId)) return;
    const selectedBucket = snapshot.buckets.some((bucket) => bucket.start === this.state.selectedBucket) ? this.state.selectedBucket : null;
    this.clearEvidence({ snapshot, selectedBucket, selectedPullRequestId: null, phase: "ready", contextReady: false, contextFailed: false });
    if (this.connected) {
      void this.loadEvidence();
      void this.shareSelection();
    }
  }

  async refresh(): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!this.connected || !snapshot || this.syncing || this.destroyed) return;
    this.syncing = true;
    try {
      this.acceptSnapshot(snapshotFromResult(await this.bridge.callTool("read_board", { board_id: snapshot.boardId })));
      if (this.state.error === this.refreshError) this.setState({ error: null });
      this.refreshError = null;
    } catch {
      this.refreshError = "The board could not refresh. Your last view is still here. Try again.";
      this.setState({ error: this.refreshError });
    } finally { this.syncing = false; }
  }

  async refreshSources(): Promise<void> {
    if (!this.connected || this.state.busy || this.destroyed) return;
    this.setState({ busy: true, sourceRefreshing: true, error: null, notice: null });
    try {
      toolError(await this.bridge.callTool("refresh_sources", {}));
      const snapshot = this.state.snapshot;
      if (snapshot) this.acceptSnapshot(snapshotFromResult(await this.bridge.callTool("read_board", { board_id: snapshot.boardId })));
      this.setState({ notice: "Sources refreshed." });
    } catch (error) {
      this.setState({ error: this.errorMessage(error) });
      await this.refresh();
    } finally { this.setState({ busy: false, sourceRefreshing: false }); }
  }

  async retry(): Promise<void> {
    this.setState({ error: null, notice: null });
    if (!this.connected) { await this.start(); return; }
    await this.refresh();
    if (this.state.snapshot) {
      await this.loadEvidence();
      await this.shareSelection();
    }
  }

  async changeQuery(patch: Partial<BoardQuery>): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!snapshot || this.state.busy || !this.connected) return;
    this.setState({ busy: true, error: null, notice: null });
    try {
      this.acceptSnapshot(snapshotFromResult(await this.bridge.callTool("update_board", { board_id: snapshot.boardId, expected_revision: snapshot.revision, ...patch })));
    } catch (error) {
      this.setState({ error: this.errorMessage(error) });
      await this.refresh();
    } finally { this.setState({ busy: false }); }
  }

  async setView(view: BoardViewMode): Promise<void> {
    if (view === this.state.view || this.destroyed) return;
    this.clearEvidence({ view, selectedPullRequestId: null, contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  async setPullRequestState(pullRequestState: PullRequestState): Promise<void> {
    if (pullRequestState === this.state.pullRequestState || this.destroyed) return;
    this.clearEvidence({ pullRequestState, selectedPullRequestId: null, contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  async selectBucket(start: string | null): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!snapshot || this.state.view !== "activity" || (start && !snapshot.buckets.some((bucket) => bucket.start === start))) return;
    this.clearEvidence({ selectedBucket: start, contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  async selectPullRequest(id: string | null): Promise<void> {
    if (this.state.view !== "pullRequests" || (id && !this.state.pullRequests.some((pr) => pr.id === id))) return;
    this.setState({ selectedPullRequestId: id, contextReady: false, contextFailed: false, error: null, notice: null });
    await this.shareSelection();
  }

  async loadMore(): Promise<void> {
    if (!this.state.nextCursor || this.state.evidenceLoading) return;
    await this.loadEvidence(true);
  }

  private async loadEvidence(append = false): Promise<void> {
    const snapshot = this.state.snapshot;
    const view = this.state.view;
    const bucketStart = view === "activity" ? this.state.selectedBucket : null;
    const pullRequestState = this.state.pullRequestState;
    if (!snapshot || !this.connected || this.destroyed || !snapshot.collectedAt || !snapshot.configuration.repositories.length || (view === "activity" && !bucketStart)) return;
    const cursor = append ? this.state.nextCursor : null;
    const sequence = ++this.evidenceSequence;
    this.setState({ evidenceLoading: true });
    try {
      const result = await this.bridge.callTool("query_evidence", {
        board_id: snapshot.boardId,
        snapshot_id: snapshot.snapshotId,
        kind: view === "activity" ? "activity" : "pull_requests",
        ...(bucketStart ? { bucket_start: bucketStart } : {}),
        ...(view === "pullRequests" ? { pull_request_state: pullRequestState } : {}),
        ...(cursor ? { cursor } : {}),
      });
      if (sequence !== this.evidenceSequence || this.destroyed) return;
      toolError(result);
      const evidence = result.structuredContent as EvidenceResult | undefined;
      const kind = view === "activity" ? "activity" : "pull_requests";
      if (evidence?.snapshotId !== snapshot.snapshotId || !Array.isArray(evidence.records) || !Array.isArray(evidence.pullRequests) ||
          (evidence.kind !== undefined && evidence.kind !== kind) ||
          (view === "pullRequests" && evidence.pullRequestState !== undefined && evidence.pullRequestState !== pullRequestState)) {
        throw new Error("The selected evidence changed. Refresh the board and select it again.");
      }
      const records = append ? [...this.state.records, ...evidence.records] : evidence.records;
      const pullRequests = append ? [...this.state.pullRequests, ...evidence.pullRequests] : evidence.pullRequests;
      this.setState({ records: [...new Map(records.map((record) => [record.id, record])).values()], pullRequests: [...new Map(pullRequests.map((record) => [record.id, record])).values()], totalRecords: evidence.totalRecords, totalPullRequests: evidence.totalPullRequests, nextCursor: evidence.nextCursor, evidenceLoading: false });
    } catch (error) {
      if (sequence === this.evidenceSequence) this.setState({ error: this.errorMessage(error), evidenceLoading: false });
    }
  }

  private shareSelection(): Promise<void> {
    const context = this.context();
    if (!context || !this.connected) return Promise.resolve();
    const sequence = ++this.contextSequence;
    this.contextQueue = this.contextQueue.catch(() => {}).then(async () => {
      if (this.destroyed || sequence !== this.contextSequence) return;
      try {
        await this.bridge.updateContext(context);
        if (sequence === this.contextSequence) this.setState({ contextReady: true, contextFailed: false });
      } catch {
        if (sequence === this.contextSequence) this.setState({ contextReady: false, contextFailed: true, error: "The selection could not be shared with the conversation. Try again." });
      }
    });
    return this.contextQueue;
  }

  async explainSelection(): Promise<void> {
    const context = this.context();
    const selected = this.state.view === "activity" ? this.state.selectedBucket : this.state.selectedPullRequestId;
    if (!context || !selected || !this.connected || this.state.busy) return;
    this.setState({ busy: true, error: null, notice: null });
    try {
      await this.shareSelection();
      if (JSON.stringify(this.context()) !== JSON.stringify(context)) throw new Error("The view changed. Select the evidence you want to discuss and try again.");
      const request = { board_id: context.boardId, snapshot_id: context.snapshotId, kind: context.view === "pullRequests" ? "pull_requests" : "activity", ...(context.bucketStart ? { bucket_start: context.bucketStart } : {}), ...(context.view === "pullRequests" ? { pull_request_state: context.pullRequestState, pull_request_id: context.pullRequestId } : {}) };
      await this.bridge.sendMessage(`Explain this BigBoard selection using query_evidence with ${JSON.stringify(request)}. Preserve this exact scope, include source links, and distinguish observations from interpretation.\n\n${JSON.stringify(context)}`);
      this.setState({ notice: "Selection sent to the conversation.", error: null });
    } catch (error) { this.setState({ error: this.errorMessage(error) }); }
    finally { this.setState({ busy: false }); }
  }

  async destroy(closeBridge = true): Promise<void> {
    this.destroyed = true;
    this.connected = false;
    this.evidenceSequence += 1;
    this.contextSequence += 1;
    if (this.poll) clearInterval(this.poll);
    this.listeners.clear();
    if (closeBridge) await this.bridge.close();
  }

  private errorMessage(error: unknown): string {
    return error instanceof Error ? error.message : "BigBoard could not complete that request. Try again.";
  }
}
