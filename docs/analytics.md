# Contribution analytics

Big Board measures recorded contribution activity. It does not estimate productivity, code quality, or total AI usage.

The Git collection details below describe **local mode**. [GitHub API mode](github.md#lightweight-activity-and-local-analysis) uses commit dates, all-file summaries, and unknown merge line counts; its measurement differences are explicit in the dashboard.

## Identity and collaboration

Canonical emails (after repository `.mailmap` processing) establish identities. Equal or similar names do not merge people. Identities with missing emails are scoped to the repository and exact name.

To combine `Mike` and `mbiggly`:

1. Highlight either contributor on the board, or open their detail view, and press `M`.
2. Type a name, alias, email, or repository name to narrow the candidates. Use `↑/↓` to select the other identity and press `Enter`.
3. Check the source and target emails and repositories. Edit the prefilled combined display name; `Ctrl+U` clears it.
4. Press `Enter` to save. Big Board recomputes the view using the saved mapping. `Esc` cancels at either stage without changing mappings.

The picker includes identities from all loaded repositories, dates, and branches, even when hidden by the current view's filters or repository exclusions. Future-dated records remain excluded. Both identities must be present in the loaded history; saved mappings can then apply to those identities in other repository selections and future sessions. A successful merge clears the board's name search; the other view filters still apply to the combined contributor. A failed save is reported and does not apply an in-memory-only merge.

Mappings live in `~/.config/bigboard/identities.json`, or `$XDG_CONFIG_HOME/bigboard/identities.json`. They apply to every repository and config profile. Saves reload the latest mapping under a file lock and replace it atomically. The chosen display name is a label; filtering and navigation use the resolved identity key.

Human `Co-authored-by` metadata gives separate coauthored participation credit. A person who is both the primary author and a coauthor of the same commit receives authored credit only. Mapping two aliases together also merges their participation without double credit. The primary author retains line-change attribution because the metadata does not allocate lines among participants.

## History and unique counts

The initial scope is **Landed**: commits reachable from the available default-branch snapshot. Available remote-tracking default history is preferred over a potentially divergent local checkout. **All branches** (`B`) includes local and remote-tracking branch history already on disk. Tags alone do not bring commits into either scope. No scan fetches remote history, so results reflect the local snapshot, not a claim that the clone is up to date. If no default branch can be identified, Big Board shows a short availability notice and contributes no Landed history from that repository; `B` still exposes its available branch history.

Commits are deduplicated by object ID across selected repositories. A full copy can supply known line counts missing at a shallow boundary. If copies have conflicting mailmap attribution, one attribution is chosen deterministically. The TUI does not display the per-commit conflict log. Consistent mailmaps or an explicit identity merge can reconcile them. Repository membership is retained, so repository subtotals may overlap and must not be summed to obtain board totals. Cherry-picks and rebases that create different IDs remain distinct.

Git author timestamps remain the activity date; the landed filter does not turn them into integration dates. A single scan cutoff excludes future-dated commits, including in ALL. Recent windows retain their rolling duration semantics. Daily/monthly buckets and heatmap cells all use the configured reporting timezone, defaulting to UTC. `R` rescans repositories and advances the cutoff; switching filters uses the existing scan snapshot.

## Merges, files, and incomplete data

Clean integration-only two-parent merges are omitted when Git can reconstruct their merge baseline. For two-parent merges with a clean reconstructed merge baseline, extra changes relative to that baseline receive authored credit. Conflicted or unsupported merges retain resolution activity with unknown line counts: removing conflict markers from a synthetic merge tree is not a trustworthy measure of newly authored lines. Temporary reconstruction objects are isolated from the user's repository. Git 2.38 or newer supplies this reconstruction mode; older supported versions retain merge activity with unknown line counts.

Branch references are resolved unambiguously, filenames use NUL-delimited parsing, and diff settings that affect counting are pinned. Generated/vendor exclusions apply to the parsed paths. Valid separate-Git-directory repositories are discoverable; linked worktrees remain skipped to avoid presenting duplicate checkouts.

Missing boundary diffs in shallow history and unallocatable merge diffs remain unknown, marked with `?` alongside any known subtotal. The TUI omits per-commit diagnostic logs and shows alerts for repository scan failures or unavailable default-branch history. Big Board does not automatically deepen or fetch repositories. Automatic object fetching in partial clones is disabled. If required objects are missing, the scan is visibly excluded rather than silently downloading them. Partial clones on Git older than 2.45.1 are skipped conservatively because this control was not consistently available; ordinary repositories require Git 2.31 or newer.

### Reading qualifications

- `?` means authored line counts are unknown. A known subtotal may appear alongside an unknown qualifier; it is not a complete measurement.
- `—` means line attribution is unallocated for coauthor-only participation, not measured as zero.
- `○` in the activity matrix marks coauthor-only participation on that day; it is separate from the line-change intensity scale.
- `N/A` for Removed/added ratio means additions are zero or some authored line counts are unknown.
- `⚠` marks repository scan failures or unavailable default-branch history. From the board, press `r` to inspect repository paths and full failure details, with `PgUp/PgDn` paging the selected repository's details. Press `Esc` first if viewing contributor details.

## Metrics and preferences

- **Authored:** unique commits for which the contributor is the primary author.
- **Coauthored:** unique commits on which a human contributor is named as a coauthor and is not the resolved primary author.
- **Lines changed:** known additions plus known removals, with incomplete counts qualified.
- **Removed/added ratio:** removals divided by additions; N/A with zero additions or unknown line counts.
- **Detected AI:** authored commits with a recognized agent author/coauthor or user override. Built-in rules use specific identities, not entire AI-company domains. Display rounding does not affect sorting precision.

Board totals reflect the selected repositories, history scope, time range, and bot inclusion. Bots are included initially; `b` changes both rows and totals. Name search (`/`) filters visible contributor rows without recalculating board totals. Repository breakdowns remain overlapping associations, not additional board commits.

Set `"timezone": "America/Denver"` (or another IANA zone) in the existing config file to override UTC, then restart Big Board. `R` refreshes Git data without reloading configuration. Interactive filters are session preferences; confirmed identity merges are saved globally. The obsolete `fuzzy: true` setting is rejected with instructions to use explicit merges; `false` is accepted for migration. `--export` has been removed, leaving `--version`, `--config`, `--group`, and `--github`.

## Revision and validation

The 0.8 analytics revision was merged in [PR #18](https://github.com/richhaase/bigboard/pull/18) on 2026-09-20 as commit [`4c726d1`](https://github.com/richhaase/bigboard/commit/4c726d1ed02cd5b6d4acac5ef0f0aed34b65e0af), following the behavior-preserving [Rust port](rust-port.md). It implements the agreed decisions recorded in the [scope document](contracts/analytics-accuracy.md) and resolves the [original audit findings](analytics-correction-backlog.md).

Validation of the final implementation commit [`7537e70`](https://github.com/richhaase/bigboard/commit/7537e7061b14a2fd3c4870ede357bdf1cec7f674):

- 108 unit tests and three real-Git integration tests passed locally on macOS ARM64 with Rust 1.98.1 and Git 2.55.0. Formatting, Clippy with warnings denied, the optimized build, dependency audit, and workflow lint passed.
- Synthetic repositories covered shared commits, conflicting mailmaps, coauthors, branch/tag ambiguity, unusual paths, shallow and partial clones, merge accounting, and consistent reporting dates. Regressions verified that partial clones do not fetch missing objects and that inherited file handles do not retain a completed identity save's lock.
- Pseudo-terminal checks at 120×60 and 80×24 exercised merge search, confirmation, saving, cancellation, persistence after restart and across repository selections, branch scope, detail views, resize, refresh, warning displays, and terminal restoration. The release binary also scanned Big Board's own repository successfully.
- [Linux/macOS Rust checks](https://github.com/richhaase/bigboard/actions/runs/35534840389), [all four Linux/macOS x86-64/ARM64 package jobs](https://github.com/richhaase/bigboard/actions/runs/35534840379), and the [CI dependency audit](https://github.com/richhaase/bigboard/actions/runs/35534840373) passed. Tag-only publication was skipped for the pull request; this merge record does not imply a tagged release was published.

For current check commands, see [Development](../README.md#development). The counts above describe the validated revision, not a fixed requirement for future test suites.
