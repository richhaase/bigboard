import type { ActivityRecord, BoardQuery, BoardSnapshot, BoardViewMode, EvidenceResult, PeopleResult, PersonSummary, PullRequestRecord, PullRequestSort, SelectionContext, SortDirection } from "../shared";

export type { BoardViewMode } from "../shared";
export type PullRequestState = "open" | "merged";
export type PeopleSource = "git" | "github";
const MAX_SELECTED_PRS = 100;

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
  pullRequestSort: PullRequestSort;
  sortDirection: SortDirection;
  selectedBucket: string | null;
  selectedPullRequestId: string | null;
  selectedPullRequests: PullRequestRecord[];
  peopleSource: PeopleSource;
  selectedPersonId: string | null;
  people: PersonSummary[];
  totalPeople: number;
  peopleNextCursor: string | null;
  peopleLoading: boolean;
  records: ActivityRecord[];
  pullRequests: PullRequestRecord[];
  totalRecords: number;
  totalPullRequests: number;
  nextCursor: string | null;
  phase: "connecting" | "waiting" | "ready";
  busy: boolean;
  sourceRefreshing: boolean;
  sourceRefreshPreviousId: string | null;
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
    snapshot: null, view: "activity", pullRequestState: "open", pullRequestSort: "updated", sortDirection: "desc",
    selectedBucket: null, selectedPullRequestId: null, selectedPullRequests: [],
    peopleSource: "git", selectedPersonId: null, people: [], totalPeople: 0, peopleNextCursor: null, peopleLoading: false,
    records: [], pullRequests: [], totalRecords: 0, totalPullRequests: 0, nextCursor: null,
    phase: "connecting", busy: false, sourceRefreshing: false, sourceRefreshPreviousId: null, evidenceLoading: false,
    contextReady: false, contextFailed: false, error: null, notice: null,
  };
  private listeners = new Set<() => void>();
  private connected = false;
  private destroyed = false;
  private syncing = false;
  private boardReadSequence = 0;
  private evidenceSequence = 0;
  private peopleSequence = 0;
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

  private clearPeople(): void {
    this.peopleSequence += 1;
    this.setState({ people: [], totalPeople: 0, peopleNextCursor: null, peopleLoading: false, selectedPersonId: null });
  }

  private context(): SelectionContext | null {
    const snapshot = this.state.snapshot;
    if (!snapshot) return null;
    return {
      ...selectionContext(snapshot, this.state.selectedBucket, this.state.view, this.state.pullRequestState, this.state.selectedPullRequestId),
      ...(this.state.view === "pullRequests" ? { selectedPullRequests: this.state.selectedPullRequests, pullRequestSort: this.state.pullRequestSort, sortDirection: this.state.sortDirection } : {}),
      ...(this.state.view === "people" ? { personId: this.state.selectedPersonId, peopleSource: this.state.peopleSource } : {}),
    };
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
        void this.loadView();
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
    if (previous && (previous.boardId !== snapshot.boardId || snapshot.revision < previous.revision)) return;
    if (previous?.refresh && snapshot.refresh) {
      const prior = previous.refresh;
      const incoming = snapshot.refresh;
      if (incoming.startedAt < prior.startedAt || (incoming.id === prior.id &&
          (incoming.updatedAt < prior.updatedAt || (prior.status !== "running" && incoming.status === "running") ||
          incoming.git.completedRepositories < prior.git.completedRepositories ||
          incoming.github.completedRepositories < prior.github.completedRepositories || incoming.github.requests < prior.github.requests))) return;
    }
    const openingView = ["activity", "pullRequests", "people"].includes(snapshot.initialView ?? "") ? snapshot.initialView : undefined;
    if (previous?.snapshotId === snapshot.snapshotId) {
      if (JSON.stringify(previous.refresh ?? null) !== JSON.stringify(snapshot.refresh ?? null)) this.setState({ snapshot });
      if (openingView && openingView !== this.state.view) void this.setView(openingView);
      return;
    }
    const selectedBucket = snapshot.buckets.some((bucket) => bucket.start === this.state.selectedBucket) ? this.state.selectedBucket : null;
    const view = openingView ?? this.state.view;
    const clearedSelection = this.state.selectedPullRequests.length > 0;
    this.clearPeople();
    this.clearEvidence({ snapshot, view, selectedBucket, selectedPullRequestId: null, selectedPullRequests: [], phase: "ready", contextReady: false, contextFailed: false, ...(clearedSelection ? { notice: "PR selection cleared because the board snapshot changed." } : {}) });
    if (this.connected) {
      void this.loadView();
      void this.shareSelection();
    }
  }

  async refresh(): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!this.connected || !snapshot || this.syncing || this.destroyed) return;
    this.syncing = true;
    try {
      await this.readLatestBoard(snapshot.boardId);
      if (this.state.error === this.refreshError) this.setState({ error: null });
      this.refreshError = null;
    } catch {
      this.refreshError = "The board could not refresh. Your last view is still here. Try again.";
      this.setState({ error: this.refreshError });
    } finally { this.syncing = false; }
  }

  private async readLatestBoard(boardId: string): Promise<void> {
    const sequence = ++this.boardReadSequence;
    try {
      const snapshot = snapshotFromResult(await this.bridge.callTool("read_board", { board_id: boardId }));
      if (sequence === this.boardReadSequence && !this.destroyed) this.acceptSnapshot(snapshot);
    } catch (error) {
      if (sequence === this.boardReadSequence && !this.destroyed) throw error;
    }
  }

  async refreshSources(): Promise<void> {
    if (!this.connected || this.state.busy || this.destroyed || this.state.snapshot?.refresh?.status === "running") return;
    this.boardReadSequence += 1;
    this.setState({ busy: true, sourceRefreshing: true, sourceRefreshPreviousId: this.state.snapshot?.refresh?.id ?? null, error: null, notice: null });
    try {
      toolError(await this.bridge.callTool("refresh_sources", {}));
      const snapshot = this.state.snapshot;
      if (snapshot) await this.readLatestBoard(snapshot.boardId);
      this.setState({ notice: this.state.snapshot?.refresh ? null : "Sources refreshed." });
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
      if (this.state.view === "people" && this.state.people.length) {
        if (this.state.peopleNextCursor) await this.loadPeople(true);
        await this.loadEvidence();
      } else if (this.state.view === "pullRequests" && this.state.pullRequests.length) {
        if (this.state.nextCursor) await this.loadEvidence(true);
      } else await this.loadView();
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
    this.clearPeople();
    this.clearEvidence({ view, selectedPullRequestId: null, selectedPullRequests: [], contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadView(), this.shareSelection()]);
  }

  async setPeopleSource(peopleSource: PeopleSource): Promise<void> {
    if (peopleSource === this.state.peopleSource || this.destroyed) return;
    this.clearPeople();
    this.clearEvidence({ peopleSource, contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadPeople(), this.shareSelection()]);
  }

  async setPullRequestState(pullRequestState: PullRequestState): Promise<void> {
    if (pullRequestState === this.state.pullRequestState || this.destroyed) return;
    this.clearEvidence({ pullRequestState, selectedPullRequestId: null, selectedPullRequests: [], contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  async setPullRequestSort(pullRequestSort: PullRequestSort, sortDirection = this.state.sortDirection): Promise<void> {
    if ((pullRequestSort === this.state.pullRequestSort && sortDirection === this.state.sortDirection) || this.destroyed) return;
    this.clearEvidence({ pullRequestSort, sortDirection, selectedPullRequestId: null, contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  async selectBucket(start: string | null): Promise<void> {
    const snapshot = this.state.snapshot;
    if (!snapshot || this.state.view !== "activity" || (start && !snapshot.buckets.some((bucket) => bucket.start === start))) return;
    this.clearEvidence({ selectedBucket: start, contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  async selectPerson(id: string | null): Promise<void> {
    if (this.state.view !== "people" || (id && !this.state.people.some(person => person.id === id))) return;
    this.clearEvidence({ selectedPersonId: id, contextReady: false, contextFailed: false, error: null, notice: null });
    await Promise.all([this.loadEvidence(), this.shareSelection()]);
  }

  async selectPullRequest(id: string | null): Promise<void> {
    if (this.state.view !== "pullRequests" || (id && !this.state.pullRequests.some(pr => pr.id === id))) return;
    this.setState({ selectedPullRequestId: id, contextReady: false, contextFailed: false, error: null, notice: null });
    await this.shareSelection();
  }

  async togglePullRequest(id: string): Promise<void> {
    if (this.state.view !== "pullRequests") return;
    const selected = this.state.selectedPullRequests.some(pr => pr.id === id);
    const pr = this.state.pullRequests.find(pr => pr.id === id);
    if (!selected && !pr) return;
    const next = selected ? this.state.selectedPullRequests.filter(pr => pr.id !== id) : [...this.state.selectedPullRequests, pr!];
    await this.setPullRequestSelection(next);
  }

  async selectLoadedPullRequests(): Promise<void> {
    if (this.state.view !== "pullRequests") return;
    await this.setPullRequestSelection([...new Map([...this.state.selectedPullRequests, ...this.state.pullRequests].map(pr => [pr.id, pr])).values()]);
  }

  async clearPullRequestSelection(): Promise<void> {
    await this.setPullRequestSelection([]);
  }

  private async setPullRequestSelection(selectedPullRequests: PullRequestRecord[]): Promise<void> {
    if (selectedPullRequests.length > MAX_SELECTED_PRS) {
      this.setState({ error: `Select up to ${MAX_SELECTED_PRS} pull requests at a time. Your existing selection is unchanged.` });
      return;
    }
    this.setState({ selectedPullRequests, contextReady: false, contextFailed: false, error: null, notice: null });
    await this.shareSelection();
  }

  async loadMore(): Promise<void> {
    if (!this.state.nextCursor || this.state.evidenceLoading) return;
    await this.loadEvidence(true);
  }

  async loadMorePeople(): Promise<void> {
    if (!this.state.peopleNextCursor || this.state.peopleLoading) return;
    await this.loadPeople(true);
  }

  private async loadView(): Promise<void> {
    await Promise.all([this.loadPeople(), this.loadEvidence()]);
  }

  private async loadPeople(append = false): Promise<void> {
    const snapshot = this.state.snapshot;
    const source = this.state.peopleSource;
    if (this.state.view !== "people" || !snapshot || !this.connected || this.destroyed || !snapshot.collectedAt || !snapshot.configuration.repositories.length) return;
    const cursor = append ? this.state.peopleNextCursor : null;
    const sequence = ++this.peopleSequence;
    this.setState({ peopleLoading: true });
    try {
      const result = await this.bridge.callTool("query_people", { board_id: snapshot.boardId, snapshot_id: snapshot.snapshotId, source, ...(cursor ? { cursor } : {}) });
      if (sequence !== this.peopleSequence || this.destroyed) return;
      toolError(result);
      const people = result.structuredContent as PeopleResult | undefined;
      if (people?.snapshotId !== snapshot.snapshotId || people.source !== source || !Array.isArray(people.people) || people.people.some(person => person.source !== source)) throw new Error("The people view changed. Refresh the board and try again.");
      const rows = append ? [...this.state.people, ...people.people] : people.people;
      this.setState({ people: [...new Map(rows.map(person => [person.id, person])).values()], totalPeople: people.totalPeople, peopleNextCursor: people.nextCursor, peopleLoading: false });
    } catch (error) {
      if (sequence === this.peopleSequence) this.setState({ error: this.errorMessage(error), peopleLoading: false });
    }
  }

  private async loadEvidence(append = false): Promise<void> {
    const snapshot = this.state.snapshot;
    const view = this.state.view;
    const bucketStart = view === "activity" ? this.state.selectedBucket : null;
    const personId = view === "people" ? this.state.selectedPersonId : null;
    const pullRequestState = this.state.pullRequestState;
    const pullRequestSort = this.state.pullRequestSort;
    const sortDirection = this.state.sortDirection;
    const kind = view === "activity" || (view === "people" && this.state.peopleSource === "git") ? "activity" : "pull_requests";
    if (!snapshot || !this.connected || this.destroyed || !snapshot.collectedAt || !snapshot.configuration.repositories.length || (view === "activity" && !bucketStart) || (view === "people" && !personId)) return;
    const cursor = append ? this.state.nextCursor : null;
    const sequence = ++this.evidenceSequence;
    this.setState({ evidenceLoading: true });
    try {
      const result = await this.bridge.callTool("query_evidence", {
        board_id: snapshot.boardId, snapshot_id: snapshot.snapshotId, kind,
        ...(bucketStart ? { bucket_start: bucketStart } : {}),
        ...(personId ? { person_id: personId } : {}),
        ...(view === "pullRequests" ? { pull_request_state: pullRequestState, pull_request_sort: pullRequestSort, sort_direction: sortDirection } : {}),
        ...(cursor ? { cursor } : {}),
      });
      if (sequence !== this.evidenceSequence || this.destroyed) return;
      toolError(result);
      const evidence = result.structuredContent as EvidenceResult | undefined;
      if (evidence?.snapshotId !== snapshot.snapshotId || !Array.isArray(evidence.records) || !Array.isArray(evidence.pullRequests) ||
          (evidence.kind !== undefined && evidence.kind !== kind) ||
          (view === "pullRequests" && evidence.pullRequestState !== undefined && evidence.pullRequestState !== pullRequestState) ||
          (personId && evidence.selectedPersonId !== undefined && evidence.selectedPersonId !== personId)) throw new Error("The selected evidence changed. Refresh the board and select it again.");
      const records = append ? [...this.state.records, ...evidence.records] : evidence.records;
      const pullRequests = append ? [...this.state.pullRequests, ...evidence.pullRequests] : evidence.pullRequests;
      this.setState({ records: [...new Map(records.map(record => [record.id, record])).values()], pullRequests: [...new Map(pullRequests.map(record => [record.id, record])).values()], totalRecords: evidence.totalRecords, totalPullRequests: evidence.totalPullRequests, nextCursor: evidence.nextCursor, evidenceLoading: false });
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

  private async sendSelection(context: SelectionContext, message: string): Promise<void> {
    this.setState({ busy: true, error: null, notice: null });
    try {
      await this.shareSelection();
      if (JSON.stringify(this.context()) !== JSON.stringify(context)) throw new Error("The view changed. Select the evidence you want to discuss and try again.");
      if (!this.state.contextReady || this.state.contextFailed) throw new Error("The selection could not be shared with the conversation. Try again.");
      await this.bridge.sendMessage(`${message}\n\n${JSON.stringify(context)}`);
      this.setState({ notice: "Selection sent to the conversation.", error: null });
    } catch (error) { this.setState({ error: this.errorMessage(error) }); }
    finally { this.setState({ busy: false }); }
  }

  async sharePullRequestSelection(review = false): Promise<void> {
    const context = this.context();
    if (!context || context.view !== "pullRequests" || !context.selectedPullRequests?.length || !this.connected || this.state.busy) return;
    const request = review ? "Run a code review for each of these selected BigBoard pull requests using your normal review workflow. Use the exact repositories, numbers, and URLs below; verify their current state and head commits before reviewing." : "I selected these BigBoard pull requests for discussion. Use this exact selection as context for my next request.";
    await this.sendSelection(context, request);
  }

  async explainSelection(): Promise<void> {
    const context = this.context();
    const selected = this.state.view === "activity" ? this.state.selectedBucket : this.state.view === "people" ? this.state.selectedPersonId : this.state.selectedPullRequestId;
    if (!context || !selected || !this.connected || this.state.busy) return;
    const request = { board_id: context.boardId, snapshot_id: context.snapshotId, kind: context.view === "activity" || (context.view === "people" && this.state.peopleSource === "git") ? "activity" : "pull_requests", ...(context.bucketStart ? { bucket_start: context.bucketStart } : {}), ...(context.view === "pullRequests" ? { pull_request_state: context.pullRequestState, pull_request_id: context.pullRequestId } : {}), ...(context.view === "people" ? { person_id: context.personId } : {}) };
    await this.sendSelection(context, `Explain this BigBoard selection using query_evidence with ${JSON.stringify(request)}. Preserve this exact scope, include source links, and distinguish observations from interpretation.`);
  }

  async destroy(closeBridge = true): Promise<void> {
    this.destroyed = true;
    this.connected = false;
    this.boardReadSequence += 1;
    this.evidenceSequence += 1;
    this.peopleSequence += 1;
    this.contextSequence += 1;
    if (this.poll) clearInterval(this.poll);
    this.listeners.clear();
    if (closeBridge) await this.bridge.close();
  }

  private errorMessage(error: unknown): string {
    return error instanceof Error ? error.message : "BigBoard could not complete that request. Try again.";
  }
}
