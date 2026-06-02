# Big Board

A cyberpunk-themed TUI for assessing contributor volumes across git repositories. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

## Install

### From Source

```bash
go install github.com/richhaase/bigboard/cmd/bigboard@latest
```

## Usage

```bash
# Analyze current directory
bigboard

# Analyze specific repos
bigboard ~/src/repo1 ~/src/repo2

# Scan a directory for repos (one level deep)
bigboard ~/src/

# Set initial sort column
bigboard --sort commits ~/src/

# Exclude repos (exact name or glob, repeatable)
bigboard --exclude vendor-lib --exclude 'experiment-*' ~/src/

# Scan deeper than one level (e.g. ~/src/org/repo)
bigboard --depth 2 ~/src/

# Export the current view without the TUI
bigboard --export json --since 90d ~/src/ > board.json
bigboard --export md --since 1y ~/src/   # also csv

# Use a named group from your config, and auto-refresh
bigboard --group backend
bigboard --watch 30s ~/src/

# Force a color theme (default: auto-detect from terminal)
bigboard --theme dark ~/src/

# Enable fuzzy author-name merging (off by default)
bigboard --fuzzy ~/src/

# Count generated/vendored files too (off by default)
bigboard --all-files ~/src/
```

The time range is chosen interactively (`←/→`); it defaults to 14 days and is not a flag.

## Accuracy notes

- **Author identity** is resolved by git's native `.mailmap` (honored automatically for `%aN`/`%aE`), then grouped by email and exact name. Substring name merging (which can collapse distinct people like *Daniel* / *Daniela*) is **off by default** — enable it with `--fuzzy`. Add a `.mailmap` to a repo to canonicalize names/emails precisely.
- **Generated & vendored files** (lockfiles, `vendor/`, `node_modules/`, `dist/`, `*.min.*`, `*.snap`, `go.sum`, …) are excluded from line counts by default so they don't inflate scores. Pass `--all-files` to count everything.
- **Scope** is each repo's default branch, `--no-merges`. Squash-merge workflows credit the merger; rename/copy churn is normalized via `-M -C`.

## Controls

| Key | Action |
|-----|--------|
| `↑/↓` `j/k` | Navigate rows |
| `←/→` `h/l` | Cycle time range (1d / 7d / 14d / 30d / 90d / 1y / all) |
| `Enter` | Drill into selected contributor |
| `Esc` | Back / Quit |
| `s` | Cycle sort column (commits / added / removed / net / ai / total) |
| `S` | Reverse sort direction |
| `/` | Filter contributors by name (incremental) |
| `r` | Open repo inclusion/exclusion overlay |
| `space` | Toggle a repo in/out (within the repo overlay) |
| `R` | Refresh (re-scan all repos) |
| `q` | Quit (clears an active filter first) |

In the contributor detail view, `↑/↓` step to the previous/next contributor.

## Config file

Optional, at `~/.config/bigboard/config.json` (override with `--config`). Flags win over config.

```json
{
  "paths": ["~/src"],
  "exclude": ["vendor-*", "*-archive"],
  "sort": "net",
  "since": "90d",
  "theme": "dark",
  "depth": 2,
  "groups": {
    "backend": ["~/src/api", "~/src/workers"],
    "frontend": ["~/src/web"]
  }
}
```

Select a group with `--group backend`. Author identities can also be canonicalized with a standard git `.mailmap` in each repo.

## Features

- ASCII art banner with vertical color gradient
- Streaming repository-scan loader that surfaces unreadable repos as they load
- Gradient impact bars with trailing glow; gold/silver/bronze rank styling
- AI-authorship as a first-class metric: leaderboard `AI%` column, per-month AI share, per-repo AI %
- Per-contributor drill-down: repo breakdown, monthly timeline, neon contribution heatmap, and derived metrics (active days, first/last commit, churn)
- Scrollable, height-aware leaderboard with incremental `/` search — every contributor reachable
- Headless `--export json|csv|md`, config file with named `--group`s, glob `--exclude`, recursive `--depth`, and `--watch`
- Accurate-by-default: native `.mailmap`, generated/vendored files excluded, deterministic ordering, rename/copy-aware churn
- Git worktree detection (automatically skipped during repo discovery)

## License

MIT
