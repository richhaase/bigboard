# Contribution analytics

Big Board measures recorded contribution activity. It does not estimate productivity, code quality, or total AI usage.

## Identity and collaboration

Canonical emails (after repository `.mailmap` processing) establish identities. Equal or similar names do not merge people. Identities with missing emails are scoped to the repository and exact name.

Press `M` on a contributor in the board or detail view. Select the other contributor from the searchable identity list, choose the combined display name, and confirm. The picker shows emails and repositories to distinguish same-name people. Cancel leaves identities untouched. A failed save is reported and does not apply an in-memory-only merge.

Mappings live in `~/.config/bigboard/identities.json`, or `$XDG_CONFIG_HOME/bigboard/identities.json`. They apply to every repository and config profile. Saves reload the latest mapping under a file lock and replace it atomically. The chosen display name is a label; filtering and navigation use the resolved identity key.

Human `Co-authored-by` metadata gives separate coauthored participation credit. A person who is both the primary author and a coauthor of the same commit receives authored credit only. Mapping two aliases together also merges their participation without double credit. The primary author retains line-change attribution because the metadata does not allocate lines among participants.

## History and unique counts

The initial scope is **Landed**: commits reachable from the available default-branch snapshot. Available remote-tracking default history is preferred over a potentially divergent local checkout. **All branches** (`B`) includes local and remote-tracking branch history already on disk. Tags alone do not bring commits into either scope. No scan fetches remote history, so results reflect the local snapshot, not a claim that the clone is up to date.

Commits are deduplicated by object ID across selected repositories. A full copy can supply known line counts missing at a shallow boundary. If copies have conflicting mailmap attribution, the board displays a warning naming the affected repositories and the deterministic attribution used. Consistent mailmaps or an explicit identity merge can reconcile them. Repository membership is retained, so repository subtotals may overlap and must not be summed to obtain board totals. Cherry-picks and rebases that create different IDs remain distinct.

Git author timestamps remain the activity date; the landed filter does not turn them into integration dates. A single scan cutoff excludes future-dated commits, including in ALL. Recent windows retain their rolling duration semantics. Daily/monthly buckets and heatmap cells all use the configured reporting timezone, defaulting to UTC.

## Merges, files, and incomplete data

Clean integration-only two-parent merges are omitted when Git can reconstruct their merge baseline. For two-parent merges with a clean reconstructed merge baseline, extra changes relative to that baseline receive authored credit. Conflicted or unsupported merges retain resolution activity with unknown line counts: removing conflict markers from a synthetic merge tree is not a trustworthy measure of newly authored lines. Temporary reconstruction objects are isolated from the user's repository. Git 2.38 or newer supplies this reconstruction mode; older supported versions retain merge activity with unknown line counts.

Branch references are resolved unambiguously, filenames use NUL-delimited parsing, and diff settings that affect counting are pinned. Generated/vendor exclusions apply to the parsed paths. Valid separate-Git-directory repositories are discoverable; linked worktrees remain skipped to avoid presenting duplicate checkouts.

Shallow history and scan failures are visibly qualified in the TUI. Missing boundary diffs and unallocatable merge diffs do not masquerade as measured zeros. Known line subtotals remain available with an explicit unknown/partial indicator. These warnings indicate limitations of the available data; Big Board does not automatically deepen or fetch repositories. Automatic object fetching in partial clones is disabled. If required objects are missing, the scan is visibly excluded rather than silently downloading them. Partial clones on Git older than 2.45.1 are skipped conservatively because this control was not consistently available; ordinary repositories require Git 2.31 or newer.

## Metrics and preferences

- **Authored:** unique commits for which the contributor is the primary author.
- **Coauthored:** unique commits on which a human contributor is named as a coauthor and is not the resolved primary author.
- **Lines changed:** known additions plus known removals, with incomplete counts qualified.
- **Removed/added ratio:** removals divided by additions; N/A with zero additions or unknown line counts.
- **Detected AI:** authored commits with a recognized agent author/coauthor or user override. Built-in rules use specific identities, not entire AI-company domains. Display rounding does not affect sorting precision.

Set `"timezone": "America/Denver"` (or another IANA zone) in the existing config file to override UTC. The obsolete `fuzzy: true` setting is rejected with instructions to use explicit merges; `false` is accepted for migration. `--export` has been removed, leaving `--version`, `--config`, and `--group`.
