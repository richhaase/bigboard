import { useSyncExternalStore } from "react";
import type { BoardController } from "./controller";

const count = new Intl.NumberFormat("en-US");

export function dateLabel(value: string, timezone = "UTC"): string {
  return new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", timeZone: timezone }).format(new Date(value));
}

function moment(value: string, timezone: string): string {
  return new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZone: timezone, timeZoneName: "short" }).format(new Date(value));
}

function githubLink(value: string): string | undefined {
  try {
    const url = new URL(value);
    return url.protocol === "https:" && url.hostname === "github.com" ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export function BoardView({ controller }: { controller: BoardController }) {
  const state = useSyncExternalStore(controller.subscribe, controller.getState);
  const board = state.snapshot;
  const selected = board?.buckets.find((bucket) => bucket.start === state.selectedBucket);
  const chartMax = Math.max(1, ...board?.buckets.flatMap((bucket) => [bucket.commits, bucket.previousCommits ?? 0]) ?? []);
  const totalChange = board ? board.totals.additions + board.totals.deletions : 0;
  const timezone = board?.scope.timezone ?? "UTC";
  const hasSources = Boolean(board?.configuration.repositories.length);
  const hasCollection = hasSources && Boolean(board?.collectedAt);
  const historyComplete = Boolean(board?.coverage.repositories.every(source => source.historyComplete));
  const mergedPRsComplete = Boolean(board?.coverage.repositories.every(source => source.mergedPRsComplete));
  const openPRsComplete = Boolean(board?.coverage.repositories.every(source => source.openPRsComplete));
  const label = (value: string) => dateLabel(value, timezone);

  return (
    <main className="bigboard" aria-label="BigBoard activity">
      <header className="board-header">
        <div className="wordmark">
          <svg aria-hidden="true" viewBox="0 0 28 28" width="26" height="26" fill="none">
            <rect x="1" y="1" width="26" height="26" rx="6" stroke="currentColor" strokeWidth="1.5" />
            <path d="M7 20v-7m7 7V7m7 13v-10" stroke="currentColor" strokeWidth="3" />
          </svg>
          <span>BigBoard</span>
        </div>
        <span className="workspace-label"><span className="source-dot" />{board?.account ? `GitHub · ${board.account.login}` : "Git history + GitHub"}</span>
      </header>

      <div className="view-heading">
        <div><p className="eyebrow">Activity</p><h1>See the change. Explore the evidence.</h1></div>
        {board && hasCollection && <p className="date-scope">{label(board.scope.since)} – {label(board.scope.until)}<span>{timezone}</span></p>}
      </div>

      {state.error && <div className="message error" role="alert"><span>{state.error}</span><button onClick={() => void controller.retry()}>Try again</button></div>}
      {state.notice && <div className="message success" role="status">{state.notice}</div>}

      {!board ? (
        <div className="waiting" role="status">
          <span className="loading-mark" />
          <h2>{state.phase === "connecting" ? "Connecting to your conversation" : "Waiting for the board"}</h2>
          <p>{state.error ? "Reopen BigBoard from the conversation if the connection does not recover." : "Your activity view will appear here when BigBoard is ready."}</p>
        </div>
      ) : (
        <>
          <section className="toolbar" aria-label="Activity controls">
            <label className="repository-filter">Repository
              <select value={board.query.repository} disabled={state.busy || !hasSources} onChange={(event) => void controller.changeQuery({ repository: event.target.value })}>
                <option value="all">All repositories</option>
                {board.configuration.repositories.map((repository) => <option key={repository} value={repository}>{repository}</option>)}
              </select>
            </label>
            <div className="period-control" role="group" aria-label="Date range">
              {[7, 14].map((days) => <button key={days} aria-pressed={board.query.days === days} disabled={state.busy || !hasSources} onClick={() => void controller.changeQuery({ days: days as 7 | 14 })}>{days} days</button>)}
            </div>
            <label className="compare-control"><input type="checkbox" checked={board.query.compare} disabled={state.busy || !hasSources} onChange={(event) => void controller.changeQuery({ compare: event.target.checked })} />Compare previous period</label>
            <button className="refresh-sources" disabled={state.busy || !hasSources} onClick={() => void controller.refreshSources()}>{state.sourceRefreshing ? "Collecting…" : "Refresh sources"}</button>
          </section>

          {!hasSources ? (
            <section className="setup-state" aria-label="Configure repositories"><h2>Choose the repositories to collect</h2><p>Ask the agent to import your native BigBoard selection, or configure the paths to your Git clones.</p><p>Opening this board does not scan folders or make GitHub requests.</p></section>
          ) : !hasCollection ? (
            <section className="setup-state" aria-label="Collect repository evidence"><h2>Sources are ready</h2><p>Refresh sources to read Git history and collect GitHub pull requests for your {board.configuration.repositories.length} selected repositories.</p></section>
          ) : (
            <>
              {(board.configuration.needsRefresh || !board.coverage.complete) && <div className="coverage-warning" role="status"><strong>{board.configuration.needsRefresh ? "Sources need a refresh" : "Some evidence is incomplete"}</strong><span>{board.coverage.note}</span></div>}
              <section className="activity-summary" aria-label="Activity totals">
                <div className="primary-total"><strong>{count.format(board.totals.commits)}{historyComplete ? "" : "+"}</strong><span>commits</span></div>
                <div className="line-total"><span className="additions">+{count.format(board.totals.additions)}</span><span className="deletions">−{count.format(board.totals.deletions)}</span><span>{count.format(totalChange)}{board.totals.unknownLineCommits ? "+ known" : ""} lines changed</span></div>
                <div className="pr-totals"><span><strong>{board.totals.mergedPRs}{mergedPRsComplete ? "" : "+"}</strong> merged PRs</span><span><strong>{board.totals.openPRs}{openPRsComplete ? "" : "+"}</strong> open now</span></div>
                {board.query.compare && <div className="comparison-total">{board.totals.previousCommits === null ? "Previous period unavailable" : `${count.format(board.totals.previousCommits)} commits in previous period`}</div>}
              </section>
              {board.totals.unknownLineCommits > 0 && <p className="count-note">Line totals are a lower bound: {board.totals.unknownLineCommits} commits have unavailable line evidence.</p>}

              <section className="activity-chart" aria-label="Daily commits">
                <div className="chart-heading"><h2>Commits by author date · 24-hour buckets</h2><div className="legend"><span><i className="current-key" />Selected period</span>{board.query.compare && <span><i className="previous-key" />Previous period</span>}</div></div>
                <div className="chart-frame">
                  <div className="chart-scale" aria-hidden="true"><span>{chartMax}</span><span>0</span></div>
                  <div className={`chart-columns days-${board.query.days}`}>
                    {board.buckets.map((bucket) => (
                      <button
                        className={`day-column${state.selectedBucket === bucket.start ? " selected" : ""}`}
                        key={bucket.start}
                        aria-label={`${label(bucket.start)}: ${bucket.commits} commits${bucket.previousCommits !== null ? `, ${bucket.previousCommits} in previous period` : ""}`}
                        title={`${moment(bucket.start, timezone)} to ${moment(bucket.end, timezone)}`}
                        aria-pressed={state.selectedBucket === bucket.start}
                        onClick={() => void controller.selectBucket(state.selectedBucket === bucket.start ? null : bucket.start)}
                      >
                        <span className="column-count">{bucket.commits}</span><span className="bar-track">
                          {board.query.compare && bucket.previousCommits !== null && <span className="bar previous-bar" style={{ height: `${bucket.previousCommits / chartMax * 100}%` }} />}
                          <span className={`bar current-bar${bucket.commits === 0 ? " empty-bar" : ""}`} style={{ height: `${bucket.commits / chartMax * 100}%` }} />
                        </span><span className="day-label">{label(bucket.start)}</span><span className="selection-tick" />
                      </button>
                    ))}
                  </div>
                </div>
                <p className="chart-instruction">Select a bucket to focus its commits and merged PRs. Open PRs remain current.</p>
              </section>

              <section className="evidence-section" aria-label="Selected evidence">
                <div className="evidence-heading">
                  <div><p className="eyebrow">Evidence</p><h2>{selected ? label(selected.start) : "Selected period"}<span>{state.totalRecords} commits</span></h2></div>
                  <div className="evidence-actions">{selected && <button className="text-button" onClick={() => void controller.selectBucket(null)}>Show whole period</button>}<button className="explain-button" disabled={state.busy} onClick={() => void controller.explainSelection()}><span aria-hidden="true">↗</span> Explain selection</button></div>
                </div>
                {selected && <p className="selection-interval">{moment(selected.start, timezone)} – {moment(selected.end, timezone)}</p>}
                <div className="context-status" role="status"><span className={state.contextReady ? "ready-dot" : "pending-dot"} />{state.contextReady ? "Selection ready for your next question" : state.contextFailed ? "Use Explain selection to include this scope in your message" : "Sharing selected scope with the conversation…"}</div>
                {state.records.length === 0 ? <p className="evidence-empty">{state.evidenceLoading ? "Loading the selected records…" : board.coverage.complete ? "No commits in this selection. Choose another bucket or widen the view." : "No commits are available in this selection. Check source coverage before treating this as zero activity."}</p> : (
                  <div className="evidence-list" tabIndex={0} aria-label="Commit records">
                    {state.records.map((record) => <article className="evidence-row" key={record.id}>
                      <div className="record-main"><strong><a href={githubLink(record.url)} target="_blank" rel="noopener noreferrer">{record.subject}</a></strong><div><span className="repository-name">{record.repository}</span><span>{record.contributor}</span><time dateTime={record.timestamp}>{moment(record.timestamp, timezone)}</time></div></div>
                      <div className="record-numbers"><span className="additions">{record.additions === null ? "unknown" : `+${record.additions}`}</span><span className="deletions">{record.deletions === null ? "unknown" : `−${record.deletions}`}</span><span className="record-id">{record.sha.slice(0, 10)}</span></div>
                    </article>)}
                  </div>
                )}
              </section>

              <section className="pr-section" aria-label="Pull request evidence">
                <div className="evidence-heading"><h2>Pull requests <span>{state.totalPullRequests}</span></h2></div>
                <p className="count-note">Merged PRs use merge dates in the selection. Open PRs show current work, including older drafts.</p>
                {state.pullRequests.length === 0 ? <p className="evidence-empty">{state.evidenceLoading ? "Loading pull requests…" : "No pull requests are available in this page. Check coverage and load more when available."}</p> : <div className="evidence-list" tabIndex={0} aria-label="Pull request records">{state.pullRequests.map((pr) => <article className="evidence-row" key={pr.id}>
                  <div className="record-main"><strong><a href={githubLink(pr.url)} target="_blank" rel="noopener noreferrer">#{pr.number} {pr.title}</a></strong><div><span className="repository-name">{pr.repository}</span><span>{pr.author}</span><time dateTime={pr.mergedAt ?? pr.updatedAt}>{moment(pr.mergedAt ?? pr.updatedAt, timezone)}</time></div></div>
                  <span className={`pr-state ${pr.state}`}>{pr.state === "merged" ? "Merged" : pr.draft ? "Open draft" : "Open"}</span>
                </article>)}</div>}
              </section>
              {state.nextCursor && <div className="load-more"><span>{state.records.length} of {state.totalRecords} commits · {state.pullRequests.length} of {state.totalPullRequests} PRs</span><button disabled={state.evidenceLoading} onClick={() => void controller.loadMore()}>{state.evidenceLoading ? "Loading…" : "Load more evidence"}</button></div>}
            </>
          )}

          <footer className="board-footer">
            <p><span className="source-tag">Git + GitHub</span>{board.coverage.note}</p>
            <p>{board.collectedAt ? `Collected ${moment(board.collectedAt, timezone)}` : "No evidence collected yet"}</p>
            <details><summary>Source coverage</summary><div className="source-coverage">{board.coverage.repositories.map((source) => <article key={source.repository}><strong>{source.repository}</strong><p>{source.historyComplete ? "Git history available" : "Git history incomplete"} · {source.openPRsComplete ? "Open PRs checked" : "Open PRs incomplete"} · {source.mergedPRsComplete ? "Merged PRs checked" : "Merged PRs incomplete"}</p><p className="repository-name">{source.ref} · {source.revision?.slice(0, 12) ?? "revision unavailable"}{source.shallow ? " · shallow clone" : ""}{source.partial ? " · partial clone" : ""}</p>{source.errors.map((error) => <p className="source-error" key={error}>{error}</p>)}</article>)}</div></details>
            <details><summary>Board details</summary><dl><dt>Board</dt><dd>{board.boardId}</dd><dt>Revision</dt><dd>{board.revision}</dd><dt>Snapshot</dt><dd>{board.snapshotId}</dd><dt>Updated</dt><dd>{board.updatedAt}</dd></dl><button className="text-button" onClick={() => void controller.retry()}>Read saved board</button></details>
          </footer>
        </>
      )}
    </main>
  );
}
