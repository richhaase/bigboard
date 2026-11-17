import { useSyncExternalStore } from "react";
import type { BoardController } from "./controller";

const count = new Intl.NumberFormat("en-US");
const day = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
const time = new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit", timeZone: "UTC" });

export function dateLabel(value: string): string {
  return day.format(new Date(value));
}

export function BoardView({ controller }: { controller: BoardController }) {
  const state = useSyncExternalStore(controller.subscribe, controller.getState);
  const board = state.snapshot;
  const selected = board?.buckets.find((bucket) => bucket.start === state.selectedBucket);
  const chartMax = Math.max(1, ...board?.buckets.flatMap((bucket) => [bucket.commits, bucket.previousCommits ?? 0]) ?? []);
  const totalChange = board ? board.totals.additions + board.totals.deletions : 0;
  const shownEnd = board ? new Date(new Date(board.scope.until).getTime() - 1).toISOString() : "";

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
        <span className="fixture-label"><span className="fixture-dot" />Sample workspace</span>
      </header>

      <div className="view-heading">
        <div>
          <p className="eyebrow">Activity</p>
          <h1>See the change. Explore the evidence.</h1>
        </div>
        {board && <p className="date-scope">{dateLabel(board.scope.since)} – {dateLabel(shownEnd)}<span>UTC</span></p>}
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
              <select value={board.query.repository} disabled={state.busy} onChange={(event) => void controller.changeQuery({ repository: event.target.value as typeof board.query.repository })}>
                <option value="all">All repositories</option>
                <option value="sample/api">sample/api</option>
                <option value="sample/web">sample/web</option>
              </select>
            </label>
            <div className="period-control" role="group" aria-label="Date range">
              {[7, 14].map((days) => <button key={days} aria-pressed={board.query.days === days} disabled={state.busy} onClick={() => void controller.changeQuery({ days: days as 7 | 14 })}>{days} days</button>)}
            </div>
            <label className="compare-control"><input type="checkbox" checked={board.query.compare} disabled={state.busy} onChange={(event) => void controller.changeQuery({ compare: event.target.checked })} />Compare previous period</label>
          </section>

          <section className="activity-summary" aria-label="Activity totals">
            <div className="primary-total"><strong>{count.format(board.totals.commits)}</strong><span>commits</span></div>
            <div className="line-total"><span className="additions">+{count.format(board.totals.additions)}</span><span className="deletions">−{count.format(board.totals.deletions)}</span><span>{count.format(totalChange)} lines changed</span></div>
            {board.query.compare && <div className="comparison-total">{board.totals.previousCommits === null ? "Previous period unavailable" : `${count.format(board.totals.previousCommits)} commits in previous period`}</div>}
          </section>

          <section className="activity-chart" aria-label="Daily commits">
            <div className="chart-heading"><h2>Daily commits</h2><div className="legend"><span><i className="current-key" />Selected period</span>{board.query.compare && <span><i className="previous-key" />Previous period</span>}</div></div>
            <div className="chart-frame">
              <div className="chart-scale" aria-hidden="true"><span>{chartMax}</span><span>0</span></div>
              <div className={`chart-columns days-${board.query.days}`}>
                {board.buckets.map((bucket) => (
                  <button
                    className={`day-column${state.selectedBucket === bucket.start ? " selected" : ""}`}
                    key={bucket.start}
                    aria-label={`${dateLabel(bucket.start)}: ${bucket.commits} commits${bucket.previousCommits !== null ? `, ${bucket.previousCommits} in previous period` : ""}`}
                    aria-pressed={state.selectedBucket === bucket.start}
                    onClick={() => void controller.selectBucket(state.selectedBucket === bucket.start ? null : bucket.start)}
                  >
                    <span className="column-count">{bucket.commits}</span>
                    <span className="bar-track">
                      {board.query.compare && bucket.previousCommits !== null && <span className="bar previous-bar" style={{ height: `${bucket.previousCommits / chartMax * 100}%` }} />}
                      <span className={`bar current-bar${bucket.commits === 0 ? " empty-bar" : ""}`} style={{ height: `${bucket.commits / chartMax * 100}%` }} />
                    </span>
                    <span className="day-label">{dateLabel(bucket.start)}</span>
                    <span className="selection-tick" />
                  </button>
                ))}
              </div>
            </div>
            <p className="chart-instruction">Select a day to focus the evidence and your next question.</p>
          </section>

          <section className="evidence-section" aria-label="Selected evidence">
            <div className="evidence-heading">
              <div><p className="eyebrow">Evidence</p><h2>{selected ? dateLabel(selected.start) : "Selected period"}<span>{selected ? selected.commits : board.totals.commits} commits</span></h2></div>
              <div className="evidence-actions">{selected && <button className="text-button" onClick={() => void controller.selectBucket(null)}>Show whole period</button>}<button className="explain-button" disabled={state.busy} onClick={() => void controller.explainSelection()}><span aria-hidden="true">↗</span> Explain selection</button></div>
            </div>
            <div className="context-status" role="status"><span className={state.contextReady ? "ready-dot" : "pending-dot"} />{state.contextReady ? "Selection ready for your next question" : state.contextFailed ? "Use Explain selection to include this scope in your message" : "Sharing selected scope with the conversation…"}</div>
            {state.evidenceLoading ? <p className="evidence-empty" role="status">Loading the selected records…</p> : state.records.length === 0 ? <p className="evidence-empty">No commits in this selection. Choose another day or widen the view.</p> : (
              <div className="evidence-list" tabIndex={0} aria-label="Commit records">
                {state.records.map((record) => <article className="evidence-row" key={record.id}>
                  <div className="record-main"><strong>{record.subject}</strong><div><span className="repository-name">{record.repository}</span><span>{record.contributor}</span><time dateTime={record.timestamp}>{dateLabel(record.timestamp)} · {time.format(new Date(record.timestamp))}</time></div></div>
                  <div className="record-numbers"><span className="additions">+{record.additions}</span><span className="deletions">−{record.deletions}</span><span className="record-id">{record.id}</span></div>
                </article>)}
              </div>
            )}
          </section>

          <footer className="board-footer">
            <p><span className="fixture-tag">Fixture data</span>{board.coverage.note}</p>
            <details><summary>Board details</summary><dl><dt>Board</dt><dd>{board.boardId}</dd><dt>Revision</dt><dd>{board.revision}</dd><dt>Snapshot</dt><dd>{board.snapshotId}</dd><dt>Updated</dt><dd>{board.updatedAt}</dd></dl><button className="text-button" onClick={() => void controller.retry()}>Refresh board</button></details>
          </footer>
        </>
      )}
    </main>
  );
}
