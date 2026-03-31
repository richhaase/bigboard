# Repository Exclusion

Exclude repositories from BigBoard's aggregate view, either via CLI flag at startup or interactively through a TUI overlay during a session.

## Motivation

When scanning a directory with many repos, a single repo with outsized activity (e.g., millions of lines from a bulk import) can drown out all other contributors. Users need a way to hide these repos without restarting or changing their directory structure.

## CLI Flag

- New `--exclude` flag accepting exact repo directory basenames.
- Repeatable: `--exclude pod-integrations-kb --exclude another-repo`.
- Matched against `filepath.Base()` of each discovered repo path.
- Pre-excluded repos start hidden when the TUI launches.

## Repo Overlay (TUI)

- Press `r` to open a full-screen overlay listing all discovered repos.
- Each repo shown with a checkbox: `[x] repo-name` (included) or `[ ] repo-name` (excluded).
- Repos pre-excluded via `--exclude` start unchecked.
- Navigate with `j/k` or arrow keys, toggle with `Space`.
- Dismiss with `Enter` or `Esc`.
- Stats recalculate on dismiss based on current inclusion set.

## Header Indicator

- Show repo count in the header area, e.g. `12/15 repos` when 3 are excluded.
- When all repos are included, show just `15 repos` (no fraction).

## Session Behavior

- Exclusions persist across time range changes and sort changes within the session.
- No disk persistence. Everything resets on quit.
- Excluded repos still appear in the overlay (unchecked) so they can be re-included.

## Data Flow

1. `DiscoverRepos()` returns all repos as today (unchanged).
2. `Model` holds the full repo list plus a set of excluded repo paths.
3. `--exclude` flags seed the excluded set at startup.
4. On overlay dismiss, re-filter commit records and re-aggregate stats.
5. Filtering happens before `Aggregate()`: skip commit records from excluded repos.

## Interaction Summary

| Key | Context | Action |
|-----|---------|--------|
| `r` | Aggregate view | Open repo overlay |
| `j/k`, arrows | Repo overlay | Navigate repo list |
| `Space` | Repo overlay | Toggle repo inclusion |
| `Enter/Esc` | Repo overlay | Dismiss overlay, recalculate stats |
