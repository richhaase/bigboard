import { useSyncExternalStore } from "react";
import type { ActivityRecord, BoardSnapshot, PullRequestRecord, PullRequestSort, RefreshProgress, SortDirection } from "../shared";
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
  } catch { return undefined; }
}

function CommitRow({ record, timezone }: { record: ActivityRecord; timezone: string }) {
  return <article className="evidence-row">
    <div className="record-main"><strong><a href={githubLink(record.url)} target="_blank" rel="noopener noreferrer">{record.subject}</a></strong><div><span className="repository-name">{record.repository}</span><span>{record.contributor}</span><time dateTime={record.timestamp}>{moment(record.timestamp, timezone)}</time></div></div>
    <div className="record-numbers"><span className="additions">{record.additions === null ? "unknown" : `+${record.additions}`}</span><span className="deletions">{record.deletions === null ? "unknown" : `−${record.deletions}`}</span><span className="record-id">{record.sha.slice(0, 10)}</span></div>
  </article>;
}

function PullRequestDetails({ pr, timezone }: { pr: PullRequestRecord; timezone: string }) {
  return <>
    <p className="repository-name selected-repository">{pr.repository} · #{pr.number}</p>
    <h3 className="selected-pr-title">{pr.title}</h3>
    <dl className="pr-metadata">
      <dt>Author</dt><dd>{pr.author}</dd><dt>Status</dt><dd>{pr.state === "merged" ? "Merged" : pr.draft ? "Open draft" : "Open"}</dd>
      <dt>Created</dt><dd>{moment(pr.createdAt, timezone)}</dd><dt>Updated</dt><dd>{moment(pr.updatedAt, timezone)}</dd>
      {pr.mergedAt && <><dt>Merged</dt><dd>{moment(pr.mergedAt, timezone)}</dd></>}
      <dt>Base branch</dt><dd className="repository-name">{pr.baseBranch}</dd><dt>Head commit</dt><dd className="repository-name">{pr.headSha.slice(0, 12)}</dd>
    </dl>
    <a className="source-link" href={githubLink(pr.url)} target="_blank" rel="noopener noreferrer">Open pull request on GitHub ↗</a>
  </>;
}

function RefreshStatus({ progress, starting }: { progress: RefreshProgress | null | undefined; starting: boolean }) {
  if (!progress && !starting) return null;
  const running = starting || progress?.status === "running";
  const status = running ? "running" : progress!.status;
  return <section className={`refresh-progress ${status}`} aria-label="Source refresh progress" role="status" aria-atomic="true">
    <div className="refresh-progress-heading"><strong>{starting ? "Starting source refresh…" : running ? "Refreshing sources" : status === "complete" ? "Refresh finished" : status === "cancelled" ? "Refresh cancelled" : "Refresh stopped"}</strong>{running && !starting && <span>Repositories processed</span>}</div>
    {starting ? <progress aria-label="Starting source refresh" /> : <>
      <p>{progress!.message}</p>
      {running && <div className="refresh-progress-sources">
        <label><span>Git history <span>{progress!.git.completedRepositories} / {progress!.git.totalRepositories}</span></span><progress aria-label="Git repositories processed" value={progress!.git.completedRepositories} max={Math.max(1, progress!.git.totalRepositories)} /></label>
        <label><span>GitHub PRs <span>{progress!.github.completedRepositories} / {progress!.github.totalRepositories}</span></span><progress aria-label="GitHub repositories processed" value={progress!.github.completedRepositories} max={Math.max(1, progress!.github.totalRepositories)} /></label>
      </div>}
    </>}
  </section>;
}

function SourceStatus({ board, timezone }: { board: BoardSnapshot; timezone: string }) {
  const label = !board.configuration.repositories.length ? "No sources" : board.refresh?.status === "running" ? "Refreshing" : !board.collectedAt ? "Not collected" : board.configuration.needsRefresh ? "Needs refresh" : board.coverage.complete ? "Collected" : "Partial coverage";
  return <details className="source-status">
    <summary aria-label={`Source status: ${label}`}><span className={board.coverage.complete && !board.configuration.needsRefresh ? "ready-dot" : "attention-dot"} />{label}</summary>
    <section className="status-panel" aria-label="Source coverage">
      <h2>Source coverage</h2><p>{board.coverage.note}</p>
      <p>{board.collectedAt ? `Collected ${moment(board.collectedAt, timezone)}` : "No evidence collected yet"}</p>
      {board.account && <p>GitHub account · {board.account.login}</p>}
      <div className="source-coverage">{board.coverage.repositories.map((source) => <article key={source.repository}>
        <strong>{source.repository}</strong><p>{source.historyComplete ? "Git history available" : "Git history incomplete"} · {source.openPRsComplete ? "Open PRs checked" : "Open PRs incomplete"} · {source.mergedPRsComplete ? "Merged PRs checked" : "Merged PRs incomplete"}</p>
        <p className="repository-name">{source.ref} · {source.revision?.slice(0, 12) ?? "revision unavailable"}{source.shallow ? " · shallow clone" : ""}{source.partial ? " · partial clone" : ""}</p>
        {source.errors.map((error) => <p className="source-error" key={error}>{error}</p>)}
      </article>)}</div>
      <details className="board-details"><summary>Board details</summary><dl><dt>Board</dt><dd>{board.boardId}</dd><dt>Revision</dt><dd>{board.revision}</dd><dt>Snapshot</dt><dd>{board.snapshotId}</dd></dl></details>
    </section>
  </details>;
}

export function BoardView({ controller }: { controller: BoardController }) {
  const state = useSyncExternalStore(controller.subscribe, controller.getState);
  const board = state.snapshot;
  const activity = state.view === "activity";
  const peopleView = state.view === "people";
  const prView = state.view === "pullRequests";
  const selected = activity ? board?.buckets.find((bucket) => bucket.start === state.selectedBucket) : undefined;
  const selectedPR = prView ? state.pullRequests.find((pr) => pr.id === state.selectedPullRequestId) : undefined;
  const selectedPerson = peopleView ? state.people.find(person => person.id === state.selectedPersonId) : undefined;
  const openPRsComplete = Boolean(board?.coverage.repositories.every(source => source.openPRsComplete));
  const mergedPRsComplete = Boolean(board?.coverage.repositories.every(source => source.mergedPRsComplete));
  const peopleComplete = state.peopleSource === "git" ? Boolean(board?.coverage.repositories.every(source => source.historyComplete)) : Boolean(board?.coverage.repositories.every(source => source.openPRsComplete && source.mergedPRsComplete));
  const peopleMax = Math.max(1, ...state.people.map(person => state.peopleSource === "git" ? person.commits : person.openPRs + person.mergedPRs));
  const chartMax = Math.max(1, ...board?.buckets.flatMap((bucket) => [bucket.commits, bucket.previousCommits ?? 0]) ?? []);
  const timezone = board?.scope.timezone ?? "UTC";
  const hasSources = Boolean(board?.configuration.repositories.length);
  const hasCollection = hasSources && Boolean(board?.collectedAt);
  const refreshRunning = state.sourceRefreshing || board?.refresh?.status === "running";
  const historyComplete = Boolean(board?.coverage.repositories.every(source => source.historyComplete));
  const prComplete = Boolean(board?.coverage.repositories.every(source => state.pullRequestState === "open" ? source.openPRsComplete : source.mergedPRsComplete));
  const label = (value: string) => dateLabel(value, timezone);
  const hasDetails = Boolean(selected || selectedPR || selectedPerson);
  const showDates = activity || peopleView || state.pullRequestState === "merged";
  const closeActivity = () => {
    const start = selected?.start;
    void controller.selectBucket(null).then(() => {
      if (start) document.querySelector<HTMLButtonElement>(`[data-bucket-start="${start}"]`)?.focus();
    });
  };
  const closePR = () => {
    const id = selectedPR?.id;
    void controller.selectPullRequest(null).then(() => {
      if (id) [...document.querySelectorAll<HTMLButtonElement>("[data-pr-id]")].find(button => button.dataset.prId === id)?.focus();
    });
  };

  const closePerson = () => {
    const id = selectedPerson?.id;
    void controller.selectPerson(null).then(() => {
      if (id) [...document.querySelectorAll<HTMLButtonElement>("[data-person-id]")].find(button => button.dataset.personId === id)?.focus();
    });
  };

  return <main className="bigboard" aria-label="BigBoard">
    <header className="board-header">
      <div className="wordmark"><svg aria-hidden="true" viewBox="0 0 28 28" width="25" height="25" fill="none"><rect x="1" y="1" width="26" height="26" rx="6" stroke="currentColor" strokeWidth="1.5" /><path d="M7 20v-7m7 7V7m7 13v-10" stroke="currentColor" strokeWidth="3" /></svg><span>BigBoard</span></div>
      <div className="source-controls">{board && <SourceStatus board={board} timezone={timezone} />}<button className="refresh-sources" disabled={state.busy || refreshRunning || !hasSources} onClick={() => void controller.refreshSources()}>{refreshRunning ? "Collecting…" : "Refresh sources"}</button></div>
    </header>
    <nav className="board-navigation" aria-label="Board views"><button aria-pressed={activity} onClick={() => void controller.setView("activity")}>Activity</button><button aria-pressed={prView} onClick={() => void controller.setView("pullRequests")}>Pull requests</button><button aria-pressed={peopleView} onClick={() => void controller.setView("people")}>People</button></nav>
    {state.error && <div className="message error" role="alert"><span>{state.error}</span><button onClick={() => void controller.retry()}>Try again</button></div>}
    {state.notice && <div className="message success" role="status">{state.notice}</div>}
    <RefreshStatus progress={board?.refresh} starting={state.sourceRefreshing && (board?.refresh?.id ?? null) === state.sourceRefreshPreviousId} />
    {!board ? <div className="waiting" role="status"><span className="loading-mark" /><h2>{state.phase === "connecting" ? "Connecting to your conversation" : "Waiting for the board"}</h2><p>{state.error ? "Reopen BigBoard from the conversation if the connection does not recover." : "Your board will appear here when BigBoard is ready."}</p></div> : <>
      <div className="view-heading"><h1>{activity ? "Activity" : peopleView ? "People" : "Pull requests"}</h1>{hasCollection && <p className="date-scope">{showDates ? `${label(board.scope.since)} – ${label(board.scope.until)}` : `As of ${moment(board.collectedAt!, timezone)}`}{showDates && <span>{timezone}</span>}</p>}</div>
      <section className="toolbar" aria-label={activity ? "Activity controls" : peopleView ? "People controls" : "Pull request controls"}>
        <label className="repository-filter">Repository<select value={board.query.repository} disabled={state.busy || !hasSources} onChange={(event) => void controller.changeQuery({ repository: event.target.value })}><option value="all">All repositories</option>{board.configuration.repositories.map(repository => <option key={repository} value={repository}>{repository}</option>)}</select></label>
        {showDates && <div className="period-control" role="group" aria-label="Date range">{[7, 14].map(days => <button key={days} aria-pressed={board.query.days === days} disabled={state.busy || !hasSources} onClick={() => void controller.changeQuery({ days: days as 7 | 14 })}>{days} days</button>)}</div>}
        {activity && <label className="compare-control"><input type="checkbox" checked={board.query.compare} disabled={state.busy || !hasSources} onChange={(event) => void controller.changeQuery({ compare: event.target.checked })} />Compare previous period</label>}
      </section>
      {!hasSources ? <section className="setup-state" aria-label="Configure repositories"><h2>Choose the repositories to collect</h2><p>Ask the agent to import your native BigBoard selection, or configure the paths to your Git clones.</p><p>Opening this board does not scan folders or make GitHub requests.</p></section> : !hasCollection ? <section className="setup-state" aria-label="Collect repository evidence"><h2>Sources are ready</h2><p>Refresh sources to collect evidence for your {board.configuration.repositories.length} selected repositories.</p></section> : <div className={`workspace-layout${hasDetails ? " has-details" : ""}`}>
        <section className="view-content" aria-label={activity ? "Activity overview" : peopleView ? "People view" : "Pull request view"}>
          {activity ? <>
            <div className="activity-summary" aria-label="Activity totals"><p className="primary-total"><strong>{count.format(board.totals.commits)}{historyComplete ? "" : "+"}</strong><span>commits</span></p><p className="line-total"><span className="additions">+{count.format(board.totals.additions)}</span><span className="deletions">−{count.format(board.totals.deletions)}</span><span>{board.totals.unknownLineCommits ? "known lines · lower bound" : "lines changed"}</span></p>{board.query.compare && <p className="comparison-total">{board.totals.previousCommits === null ? "Previous period unavailable" : `${count.format(board.totals.previousCommits)} in previous period`}</p>}</div>
            <section className="activity-chart" aria-label="Daily commits"><div className="chart-heading"><h2>Commits by author date</h2>{board.query.compare && <div className="legend"><span><i className="current-key" />This period</span><span><i className="previous-key" />Previous</span></div>}</div><div className="chart-frame"><div className="chart-scale" aria-hidden="true"><span>{chartMax}</span><span>0</span></div><div className={`chart-columns days-${board.query.days}`}>{board.buckets.map(bucket => <button className={`day-column${state.selectedBucket === bucket.start ? " selected" : ""}`} key={bucket.start} data-bucket-start={bucket.start} aria-label={`${label(bucket.start)}: ${bucket.commits} commits${bucket.previousCommits !== null ? `, ${bucket.previousCommits} in previous period` : ""}`} title={`${moment(bucket.start, timezone)} to ${moment(bucket.end, timezone)}`} aria-pressed={state.selectedBucket === bucket.start} onClick={() => void controller.selectBucket(state.selectedBucket === bucket.start ? null : bucket.start)}><span className="column-count">{bucket.commits}</span><span className="bar-track">{board.query.compare && bucket.previousCommits !== null && <span className="bar previous-bar" style={{ height: `${bucket.previousCommits / chartMax * 100}%` }} />}<span className={`bar current-bar${bucket.commits === 0 ? " empty-bar" : ""}`} style={{ height: `${bucket.commits / chartMax * 100}%` }} /></span><span className="day-label">{label(bucket.start)}</span><span className="selection-tick" /></button>)}</div></div><p className="chart-instruction">Select a 24-hour interval to inspect its commits.</p></section>
          </> : peopleView ? <>
            <div className="pr-view-header"><div className="pr-state-control" role="group" aria-label="People source"><button aria-pressed={state.peopleSource === "git"} onClick={() => void controller.setPeopleSource("git")}>Git contributors</button><button aria-pressed={state.peopleSource === "github"} onClick={() => void controller.setPeopleSource("github")}>GitHub authors</button></div><p className="inventory-count">{state.totalPeople}{peopleComplete ? "" : "+"} people</p></div>
            <p className="view-instruction">{state.peopleSource === "git" ? "Contributors by commits in this period. Git email identities use each repository’s mailmap." : "PR authors by open and merged pull requests. Open counts are as of collection; merged counts use this period."} Git and GitHub identities stay separate.</p>
            {state.people.length === 0 ? <p className="empty-view" role="status">{state.peopleLoading ? "Loading people…" : peopleComplete ? "No people in this scope." : "No people are available. Check source coverage before treating this as zero."}</p> : <div className="people-inventory" aria-label="People records">{state.people.map(person => <button key={person.id} data-person-id={person.id} className={`person-row${selectedPerson?.id === person.id ? " selected" : ""}`} aria-label={`Select person ${person.name}: ${person.identity}`} aria-pressed={selectedPerson?.id === person.id} onClick={() => void controller.selectPerson(selectedPerson?.id === person.id ? null : person.id)}><span className="person-main"><strong>{person.name}</strong><span className="person-identity">{person.identity}</span><span className="person-bar" aria-hidden="true"><span style={{ width: `${(state.peopleSource === "git" ? person.commits : person.openPRs + person.mergedPRs) / peopleMax * 100}%` }} /></span></span><span className="person-numbers">{state.peopleSource === "git" ? <><strong>{count.format(person.commits)}{peopleComplete ? "" : "+"} commits</strong><span><span className="additions">+{count.format(person.additions)}</span> <span className="deletions">−{count.format(person.deletions)}</span>{person.unknownLineCommits > 0 ? " known lines" : " lines"}</span></> : <><strong>{count.format(person.openPRs)}{openPRsComplete ? "" : "+"} open</strong><span>{count.format(person.mergedPRs)}{mergedPRsComplete ? "" : "+"} merged</span></>}<span>{person.repositories.length} {person.repositories.length === 1 ? "repository" : "repositories"}</span></span></button>)}</div>}
            {state.peopleNextCursor && <div className="load-more"><span>{state.people.length} of {state.totalPeople}</span><button disabled={state.peopleLoading} onClick={() => void controller.loadMorePeople()}>{state.peopleLoading ? "Loading…" : "Load more people"}</button></div>}
          </> : <>
            <div className="pr-view-header"><div className="pr-state-control" role="group" aria-label="Pull request state"><button aria-pressed={state.pullRequestState === "open"} onClick={() => void controller.setPullRequestState("open")}>Open</button><button aria-pressed={state.pullRequestState === "merged"} onClick={() => void controller.setPullRequestState("merged")}>Merged</button></div><p className="inventory-count">{state.evidenceLoading && !state.pullRequests.length ? (state.pullRequestState === "open" ? board.totals.openPRs : board.totals.mergedPRs) : state.totalPullRequests}{prComplete ? "" : "+"} {state.pullRequestState === "open" ? "open" : "merged in period"}</p></div>
            <div className="pr-sort-controls"><label>Sort by<select aria-label="PR sort" value={state.pullRequestSort} onChange={event => void controller.setPullRequestSort(event.target.value as PullRequestSort)}><option value="updated">Updated</option><option value="created">Created</option><option value="repository">Repository</option><option value="author">Author</option><option value="title">Title</option><option value="number">Number</option></select></label><label>Order<select aria-label="Sort direction" value={state.sortDirection} onChange={event => void controller.setPullRequestSort(state.pullRequestSort, event.target.value as SortDirection)}><option value="desc">Descending</option><option value="asc">Ascending</option></select></label><button disabled={!state.pullRequests.length || state.evidenceLoading} onClick={() => void controller.selectLoadedPullRequests()}>Select loaded</button></div>
            {state.selectedPullRequests.length > 0 && <section className="pr-selection" aria-label="Selected pull requests"><div className="pr-selection-heading"><strong>{state.selectedPullRequests.length} selected</strong><button onClick={() => void controller.clearPullRequestSelection()}>Clear selection</button></div><div className="selected-pr-list">{state.selectedPullRequests.map(pr => <button key={pr.id} aria-label={`Remove ${pr.repository}#${pr.number} from selection`} onClick={() => void controller.togglePullRequest(pr.id)}><span>{pr.repository}#{pr.number}</span><span aria-hidden="true">×</span></button>)}</div><div className="pr-selection-actions"><button className="explain-button" disabled={state.busy || !state.contextReady} onClick={() => void controller.sharePullRequestSelection(true)}>Request code reviews</button><button disabled={state.busy || !state.contextReady} onClick={() => void controller.sharePullRequestSelection()}>Share selection</button></div><p className="context-status" role="status">{state.contextReady ? "Selection shared. You can also ask the agent to review these PRs." : state.contextFailed ? "Selection could not be shared. Try again above." : "Sharing selected pull requests…"}</p></section>}
            {state.pullRequests.length === 0 ? <p className="empty-view" role="status">{state.evidenceLoading ? "Loading pull requests…" : prComplete ? `No ${state.pullRequestState} pull requests in this scope.` : "No pull requests are available. Check source coverage before treating this as zero."}</p> : <div className="pr-inventory" aria-label="Pull request records">{state.pullRequests.map(pr => <div className={`pr-selectable-row${state.selectedPullRequests.some(selected => selected.id === pr.id) ? " checked" : ""}`} key={pr.id}><input type="checkbox" aria-label={`Select ${pr.repository}#${pr.number}`} checked={state.selectedPullRequests.some(selected => selected.id === pr.id)} onChange={() => void controller.togglePullRequest(pr.id)} /><button className={`pr-row${selectedPR?.id === pr.id ? " selected" : ""}`} data-pr-id={pr.id} aria-label={`Select pull request #${pr.number}: ${pr.title}`} aria-pressed={selectedPR?.id === pr.id} onClick={() => void controller.selectPullRequest(selectedPR?.id === pr.id ? null : pr.id)}><span className="pr-row-main"><strong>#{pr.number} {pr.title}</strong><span><span className="repository-name">{pr.repository}</span><span>{pr.author}</span></span></span><span className="pr-row-meta"><span className={`pr-state ${pr.state}`}>{pr.state === "merged" ? "Merged" : pr.draft ? "Draft" : "Open"}</span><time dateTime={pr.mergedAt ?? pr.updatedAt}>{label(pr.mergedAt ?? pr.updatedAt)}</time></span></button></div>)}</div>}
            {state.nextCursor && <div className="load-more"><span>{state.pullRequests.length} of {state.totalPullRequests}</span><button disabled={state.evidenceLoading} onClick={() => void controller.loadMore()}>{state.evidenceLoading ? "Loading…" : "Load more pull requests"}</button></div>}
          </>}
        </section>
        {selected && <aside className="detail-pane" aria-label="Activity evidence"><div className="detail-heading"><div><p className="eyebrow">Selected interval</p><h2>{label(selected.start)}</h2></div><button className="close-detail" aria-label="Close activity evidence" onClick={closeActivity}>×</button></div><p className="selection-interval">{moment(selected.start, timezone)} – {moment(selected.end, timezone)}</p><p className="detail-count">{selected.commits}{historyComplete ? "" : "+"} supporting commits</p>{state.records.length === 0 ? <p className="evidence-empty">{state.evidenceLoading ? "Loading supporting commits…" : historyComplete ? "No commits in this interval." : "No commits are available. Source coverage is incomplete."}</p> : <div className="evidence-list" tabIndex={0} aria-label="Commit records">{state.records.map(record => <CommitRow key={record.id} record={record} timezone={timezone} />)}</div>}{state.nextCursor && <div className="load-more"><span>{state.records.length} of {state.totalRecords}</span><button disabled={state.evidenceLoading} onClick={() => void controller.loadMore()}>{state.evidenceLoading ? "Loading…" : "Load more evidence"}</button></div>}<SelectionAction controller={controller} busy={state.busy} ready={state.contextReady} failed={state.contextFailed} /></aside>}
        {selectedPerson && <aside className="detail-pane" aria-label="People evidence"><div className="detail-heading"><div><p className="eyebrow">{state.peopleSource === "git" ? "Git contributor" : "GitHub author"}</p><h2>{selectedPerson.name}</h2></div><button className="close-detail" aria-label="Close people evidence" onClick={closePerson}>×</button></div><p className="repository-name person-detail-identity">{selectedPerson.identity}</p><p className="detail-count">{selectedPerson.repositories.join(" · ")}</p>{state.records.length === 0 && state.pullRequests.length === 0 ? <p className="evidence-empty">{state.evidenceLoading ? "Loading supporting evidence…" : "No supporting evidence is available in this scope."}</p> : <>{state.records.length > 0 && <div className="evidence-list" tabIndex={0} aria-label="Commit records">{state.records.map(record => <CommitRow key={record.id} record={record} timezone={timezone} />)}</div>}{state.pullRequests.length > 0 && <div className="evidence-list" tabIndex={0} aria-label="Person pull requests">{state.pullRequests.map(pr => <article className="evidence-row" key={pr.id}><div className="record-main"><strong><a href={githubLink(pr.url)} target="_blank" rel="noopener noreferrer">#{pr.number} {pr.title}</a></strong><div><span className="repository-name">{pr.repository}</span><span>{pr.state === "merged" ? "Merged" : pr.draft ? "Draft" : "Open"}</span></div></div></article>)}</div>}</>}{state.nextCursor && <div className="load-more"><span>{state.records.length + state.pullRequests.length} of {state.totalRecords + state.totalPullRequests}</span><button disabled={state.evidenceLoading} onClick={() => void controller.loadMore()}>{state.evidenceLoading ? "Loading…" : "Load more evidence"}</button></div>}<SelectionAction controller={controller} busy={state.busy} ready={state.contextReady} failed={state.contextFailed} /></aside>}
        {selectedPR && <aside className="detail-pane" aria-label="Pull request details"><div className="detail-heading"><h2>Pull request details</h2><button className="close-detail" aria-label="Close pull request details" onClick={closePR}>×</button></div><PullRequestDetails pr={selectedPR} timezone={timezone} /><SelectionAction controller={controller} busy={state.busy} ready={state.contextReady} failed={state.contextFailed} /></aside>}
      </div>}
    </>}
  </main>;
}

function SelectionAction({ controller, busy, ready, failed }: { controller: BoardController; busy: boolean; ready: boolean; failed: boolean }) {
  return <div className="selection-action"><button className="explain-button" disabled={busy || !ready} onClick={() => void controller.explainSelection()}>Explain selection</button><p className="context-status" role="status"><span className={ready ? "ready-dot" : "pending-dot"} />{ready ? "Ready for your next question" : failed ? "Selection could not be shared. Try again above." : "Sharing selected scope…"}</p></div>;
}
