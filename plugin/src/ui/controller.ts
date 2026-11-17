import type { ActivityRecord, BoardQuery, BoardSnapshot, SelectionContext } from "../shared";

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
  selectedBucket: string | null;
  records: ActivityRecord[];
  phase: "connecting" | "waiting" | "ready";
  busy: boolean;
  evidenceLoading: boolean;
  contextReady: boolean;
  contextFailed: boolean;
  error: string | null;
  notice: string | null;
}

export function selectionContext(snapshot: BoardSnapshot, bucketStart: string | null): SelectionContext {
  const bucket = snapshot.buckets.find((item) => item.start === bucketStart);
  return {
    boardId: snapshot.boardId,
    revision: snapshot.revision,
    snapshotId: snapshot.snapshotId,
    query: snapshot.query,
    scope: snapshot.scope,
    bucketStart: bucket?.start ?? null,
    recordIds: bucket ? bucket.recordIds : snapshot.buckets.flatMap((item) => item.recordIds),
  };
}

export function snapshotFromResult(result: ToolResult): BoardSnapshot {
  if (result.isError) {
    throw new Error(result.content?.find((item) => item.type === "text")?.text ?? "BigBoard could not complete the request.");
  }
  const value = result.structuredContent as BoardSnapshot | undefined;
  if (!value || typeof value.boardId !== "string" || typeof value.revision !== "number" ||
      typeof value.snapshotId !== "string" || !Array.isArray(value.buckets) || !value.query ||
      !value.scope || !value.coverage || !value.totals) {
    throw new Error("BigBoard returned an unreadable board. Reopen it from the conversation.");
  }
  return value;
}

export class BoardController {
  private state: BoardViewState = {
    snapshot: null,
    selectedBucket: null,
    records: [],
    phase: "connecting",
    busy: false,
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

  async start(): Promise<void> {
    try {
      await this.bridge.connect((result) => {
        try {
          this.acceptSnapshot(snapshotFromResult(result));
        } catch (error) {
          this.setState({ error: this.errorMessage(error) });
        }
      }, () => { void this.destroy(false); });
      if (this.destroyed) return;
      this.connected = true;
      if (this.state.snapshot) {
        await this.refresh();
        void this.loadEvidence();
        void this.shareSelection();
      } else {
        this.setState({ phase: "waiting" });
      }
      if (this.pollMilliseconds > 0) {
        this.poll = setInterval(() => {
          if (typeof document === "undefined" || document.visibilityState !== "hidden") {
            void this.refresh();
          }
        }, this.pollMilliseconds);
      }
    } catch {
      this.setState({ error: "BigBoard could not connect to this conversation. Reopen the board to try again." });
    }
  }

  acceptSnapshot(snapshot: BoardSnapshot): void {
    if (this.destroyed) return;
    const previous = this.state.snapshot;
    if (previous && previous.boardId !== snapshot.boardId) return;
    if (previous && snapshot.revision < previous.revision) return;
    if (previous && snapshot.snapshotId === previous.snapshotId) return;
    const selectedBucket = snapshot.buckets.some((bucket) => bucket.start === this.state.selectedBucket)
      ? this.state.selectedBucket : null;
    this.evidenceSequence += 1;
    this.setState({ snapshot, selectedBucket, records: [], phase: "ready", contextReady: false, contextFailed: false });
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
      const result = await this.bridge.callTool("read_board", { board_id: snapshot.boardId });
      this.acceptSnapshot(snapshotFromResult(result));
      if (this.state.error === this.refreshError) this.setState({ error: null });
      this.refreshError = null;
    } catch {
      this.refreshError = "The board could not refresh. Your last view is still here. Try again.";
      this.setState({ error: this.refreshError });
    } finally {
      this.syncing = false;
    }
  }

  async retry(): Promise<void> {
    this.setState({ error: null, notice: null });
    if (!this.connected) {
      await this.start();
      return;
    }
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
      const result = await this.bridge.callTool("update_board", {
        board_id: snapshot.boardId,
        expected_revision: snapshot.revision,
        ...patch,
      });
      this.acceptSnapshot(snapshotFromResult(result));
    } catch (error) {
      this.setState({ error: this.errorMessage(error) });
      await this.refresh();
    } finally {
      this.setState({ busy: false });
    }
  }

  async selectBucket(start: string | null): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!snapshot || (start && !snapshot.buckets.some((bucket) => bucket.start === start))) return;
    this.setState({ selectedBucket: start, records: [], contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  private async loadEvidence(): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!snapshot || !this.connected || this.destroyed) return;
    const sequence = ++this.evidenceSequence;
    this.setState({ evidenceLoading: true });
    try {
      const result = await this.bridge.callTool("query_evidence", {
        board_id: snapshot.boardId,
        snapshot_id: snapshot.snapshotId,
        ...(this.state.selectedBucket ? { bucket_start: this.state.selectedBucket } : {}),
      });
      if (sequence !== this.evidenceSequence || this.destroyed) return;
      if (result.isError) throw new Error("Could not load the selected evidence.");
      const evidence = result.structuredContent as { snapshotId?: string; records?: ActivityRecord[] } | undefined;
      if (evidence?.snapshotId !== snapshot.snapshotId || !Array.isArray(evidence.records)) {
        throw new Error("The selected evidence changed. Refresh the board and select it again.");
      }
      this.setState({ records: evidence.records, evidenceLoading: false });
    } catch (error) {
      if (sequence === this.evidenceSequence) {
        this.setState({ error: this.errorMessage(error), evidenceLoading: false });
      }
    }
  }

  private shareSelection(): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!snapshot || !this.connected) return Promise.resolve();
    const sequence = ++this.contextSequence;
    const context = selectionContext(snapshot, this.state.selectedBucket);
    this.contextQueue = this.contextQueue.catch(() => {}).then(async () => {
      if (this.destroyed || sequence !== this.contextSequence) return;
      try {
        await this.bridge.updateContext(context);
        if (sequence === this.contextSequence) this.setState({ contextReady: true, contextFailed: false });
      } catch {
        if (sequence === this.contextSequence) {
          this.setState({ contextReady: false, contextFailed: true, error: "The selection could not be shared with the conversation. Try again." });
        }
      }
    });
    return this.contextQueue;
  }

  async explainSelection(): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!snapshot || !this.connected || this.state.busy) return;
    const context = selectionContext(snapshot, this.state.selectedBucket);
    this.setState({ busy: true, error: null, notice: null });
    try {
      await this.shareSelection();
      const current = this.state.snapshot;
      if (!current || current.snapshotId !== context.snapshotId ||
          this.state.selectedBucket !== context.bucketStart) {
        throw new Error("The view changed. Select the activity you want to discuss and try again.");
      }
      await this.bridge.sendMessage(
        `Explain this BigBoard selection using query_evidence with board_id ${context.boardId}, ` +
        `snapshot_id ${context.snapshotId}` + (context.bucketStart ? `, bucket_start ${context.bucketStart}` : "") +
        `. This is synthetic fixture data. Preserve the selected scope and distinguish observations from interpretation.\n\n` +
        JSON.stringify(context),
      );
      this.setState({ notice: "Selection sent to the conversation.", error: null });
    } catch (error) {
      this.setState({ error: this.errorMessage(error) });
    } finally {
      this.setState({ busy: false });
    }
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
