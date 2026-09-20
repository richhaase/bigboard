# Big Board analytics audit

Go reference revision: `a0acd7677837fff81eb10d31afc3a6b030362009`. Audited 2026-09-20.

These are deferred corrections. The user chose a behavior-preserving Rust port first; analytics changes will follow separately. All Go source references below refer to that immutable revision.
The initial audit was read-only and used isolated synthetic fixtures.

## Baseline

The implementation contains approximately 3,438 Go lines across Git collection, statistics, CLI/config/export, and the Bubble Tea/Lipgloss interface. It already supports mailmap identity mapping, generated-file exclusions, bot tags, AI attribution, time/repository filtering, contributor detail, and JSON export.

The Go baseline passed all four packages’ tests and `go vet ./...`. Passing the baseline did not cover the edge cases below.

## Confirmed correctness issues

1. **Ambiguous branch names select the wrong history.** `git/git.go:211–229,263` returns and scans short names. A branch `main` containing two commits and a tag `main` at the first commit yields only one collected commit. Resolve fully qualified branch refs and scan a resolved commit object. Git documents tag-before-branch ambiguity: https://git-scm.com/docs/gitrevisions .

2. **Quoted paths bypass generated-file filtering.** `git/git.go:85,263,440–450` parses line-oriented numstat without decoding quoted paths. One source line plus three lines in `vendor/bad<TAB>file.go` produces four counted lines rather than one. Use NUL-delimited path parsing, including explicit rename/copy path pairs. `core.quotePath=false` does not disable every form of quoting. Git documents `-z`: https://git-scm.com/docs/git-diff .

3. **AI detection falsely classifies human employees.** `git/git.go:424,466–473,540` treats all addresses at six AI vendor domains as AI. A synthetic human author at `@openai.com` with no AI trailer is classified AI-assisted. Use specific known agent identities by default and explicit user overrides for domains. Describe this metric as detected/declared AI attribution; absence of metadata cannot prove absence of AI use.

4. **Ambient Git settings change counts.** `git/git.go:263` controls path quoting but does not pin relevant analytics behavior. A one-commit repository with three added lines reports zero added lines when `log.showRoot=false`. Pin root-diff behavior and review other diff-affecting configuration. Git documents `log.showRoot`: https://git-scm.com/docs/git-log .

5. **Shallow history is presented without qualification.** The CLI reports one commit and four added lines for a depth-one clone of a two-commit repository; the boundary commit actually adds only one line. The same full repository reports two commits and four added lines. Detect shallow repositories and explicitly mark incomplete history and unknown boundary diffs. Do not fetch without an agreed policy. Git documents truncated history: https://git-scm.com/docs/git-clone .

6. **Some valid repository layouts are skipped.** `git/git.go:328,599` treats every `.git` file containing `gitdir:` as a linked worktree. A valid `git init --separate-git-dir` repository is undiscoverable, although direct collection succeeds. Distinguish shared Git metadata/worktrees from other gitfiles.

7. **AI ordering loses precision before sorting.** `stats/stats.go:67–71,325` sorts on a floored integer percentage; two different actual ratios can tie and be reordered by line volume. Compare exact ratios and round only for display.

8. **Recent windows admit future commits.** `stats/stats.go:85–94` checks only a lower cutoff. Future-dated records count in recent-window totals although the heatmap omits future dates. Define a single query timestamp and upper bound.

9. **Changing the time range can lose the selected contributor.** `stats/stats.go:224–243` chooses the canonical display name from the filtered records; `tui/app.go:417,655–664` identifies the selected contributor by that name. An ALL-to-7d change from a majority of `Alice Smith` commits to one recent `asmith` commit at the same email renders NO SIGNAL despite available activity. Selection needs a contributor identity independent of its current display label.

10. **Timezone mismatch hides past activity in the heatmap.** `tui/operativeview.go:106,124–138` groups by author-local date but draws and bounds cells using the display clock's timezone. At display time `2026-09-20T12:00Z`, a commit at `2026-09-21T00:30+14:00` is already 90 minutes old but yields no active cell. Use a consistent calendar timezone across grouping and rendering.

## Decisions required before changing analytics

- **Meaning of rank:** current impact equals additions plus removals. It measures change volume, not delivered value or productivity. Current churn is deletions/additions, including a zero result for deletion-only activity; it does not track code being rewritten later.
- **Identity policy:** `stats/stats.go:175–203` deliberately merges equal normalized names even with different emails. This joins work/personal aliases but also merges unrelated same-name people. Normalization removes spaces, dots, hyphens, and underscores. Existing tests explicitly require this behavior. Decide whether mailmap/explicit mappings should be authoritative and name-only merging optional.
- **History scope:** collection deliberately prefers local default-branch history, then remote tracking fallbacks, and excludes merge commits. A local default branch behind its remote tracking ref produces older results. Merge-only resolutions and unmerged branches are omitted. Decide what activity is intended to count.
- **Multiple clones/forks:** deduplication uses filesystem location, not shared commit history. Scanning full and shallow copies of the fixture together reports three commits/eight added lines. Decide whether shared commits count per repository or once across the chosen scope.
- **Dates:** timestamps are author dates, not integration/committer dates. Calendar aggregation uses each record's own offset while the heatmap calendar is local. Define date basis and timezone before changing this behavior.
- **Human coauthors:** trailers are used for AI detection but human coauthors do not receive contributor credit. Decide whether coauthor attribution belongs in scope and how totals should reconcile.
- **Completeness/export:** a partially failed scan produces a successful JSON array with warnings only on stderr. The export contains all-time contributor aggregates, not commit-level events or daily buckets; first/last dates cannot reconstruct totals for a narrower date window. Decide whether machine-readable completeness and query metadata are required.

## Evidence and migration boundary

The Rust unit tests and `scripts/check_parity.py` record existing behavior for normal histories and selected audited edge cases. These fixtures establish compatibility, not analytical correctness. The initial audit also reproduced these findings against the public Go APIs and CLI in isolated temporary repositories.

The confirmed purpose is contribution activity across a team, with interest in personal/agent activity and productivity insights as well. Identity, history scope, date policy, and the interpretation of productivity will be decided during the subsequent analytics work. No analytics-policy change is intended in this Rust revision.
