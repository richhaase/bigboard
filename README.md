# Big Board

A cyberpunk-themed TUI for assessing contributor volumes across git repositories. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

## Install

Big Board requires Git **2.31 or newer** on `PATH`. Git **2.38 or newer** supports reconstructed merge baselines; on older versions, merge activity is retained with unknown line counts. Partial clones require **2.45.1 or newer** so automatic object fetching can be disabled; older partial clones are skipped with a visible warning. This revision is written in Rust with Ratatui and Crossterm. Building from source also requires a current stable Rust toolchain.

### From Source

```bash
git clone https://github.com/richhaase/bigboard.git
cd bigboard
cargo install --path . --locked
```

## Usage

Big Board is TUI-first: preferences live in the config file, and the CLI has three flags: `--version`, `--config`, and `--group`.

```bash
# Analyze current directory
bigboard

# Analyze specific repos or scan directories for repos
bigboard ~/src/repo1 ~/src/repo2
bigboard ~/src/

# Use a named group from your config
bigboard --group backend

# Use an alternate config file
bigboard --config ./bigboard.json

# Print version and exit
bigboard --version
```

The time range is chosen interactively (`←/→`) and defaults to 14 days. Set
`since` in the config to choose the initial range. Dates use UTC unless you set
`timezone` to an IANA timezone such as `America/Denver`.

## Analytics

- **Explicit identity:** matching canonical emails and Git `.mailmap` entries establish identity. Names alone never combine people. Highlight a contributor and press `M` to select another identity, choose a combined name, and confirm a merge. Saved merges apply across repositories and sessions.
- **History:** the default view shows landed work on the available default branch. Press `B` to include unmerged activity from local and remote-tracking branches already present on disk. Scanning does not fetch remote changes.
- **Unique commits:** identical commit IDs count once across the board, retaining their repository associations. Repository subtotals can overlap. Cherry-picks and rebases with different IDs remain distinct commits.
- **Collaboration:** authored and human-coauthored commit counts are separate. Line changes remain attributed to the primary author; coauthor metadata does not specify each person's line contribution.
- **Detected AI:** known agent identities and recognized coauthor metadata indicate AI attribution. Human employees are not flagged solely by an AI company's domain. User-configured exact emails and domain overrides remain available. Missing attribution does not prove AI was absent.
- **Activity metrics:** Lines changed means additions plus removals. Removed/added ratio is N/A with zero additions or unknown line counts. These metrics describe activity, not productivity or business value.
- **Completeness:** shallow history, scan failures, and unallocatable merge-resolution line counts are visibly qualified. Unknown counts are not treated as measured zero. Clean integration-only merges do not add duplicate activity; additional merge edits receive credit where measurable.
- **Bots and generated files:** bots remain counted and tagged, with `b` toggling visibility. Generated/vendor files are excluded from line counts by default; `all_files` includes them.

See [analytics behavior](docs/analytics.md) for the counting rules and limitations. The [original audit](docs/analytics-correction-backlog.md) and [Rust port record](docs/rust-port.md) remain historical references.

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
| `b` | Toggle bot contributors in/out |
| `B` | Toggle landed / all-branch activity |
| `M` | Merge the selected contributor with another identity |
| `r` | Open repo inclusion/exclusion overlay |
| `space` | Toggle a repo in/out (within the repo overlay) |
| `R` | Refresh (re-scan all repos) |
| `q` | Quit (clears an active filter first) |

In the contributor detail view, `↑/↓` step to the previous/next contributor.

## Config file

Optional, at `~/.config/bigboard/config.json` (override with `--config`).

```json
{
  "paths": ["~/src"],
  "exclude": ["vendor-*", "org-a/api"],
  "sort": "net",
  "since": "90d",
  "theme": "dark",
  "timezone": "UTC",
  "all_files": false,
  "depth": 2,
  "groups": {
    "backend": ["~/src/api", "~/src/workers"],
    "frontend": ["~/src/web"]
  },
  "ai_identities": ["my-agent@example.com", "@agents.example.com"],
  "bot_identities": ["my-agent@example.com"]
}
```

- `timezone` defaults to `UTC` and accepts IANA names such as `America/Denver`.
- Saved merges live in `~/.config/bigboard/identities.json` (or `$XDG_CONFIG_HOME/bigboard/identities.json`), independent of repo groups and `--config`.
- Legacy `fuzzy: false` is accepted. `fuzzy: true` now gives an actionable error; use `M` for explicit merges.
- `since` takes a preset label: `1d`, `7d`, `14d`, `30d`, `90d`, `1y`, or `all`.
- `exclude` entries match a repo basename, a unique display name like `org-a/api` (for duplicate basenames), or a glob of either.
- `ai_identities` marks commits by those authors (or co-authors) as AI-assisted; entries are exact emails or `@domain` suffixes.
- `bot_identities` tags contributors as bots; entries are exact emails, `@domain` suffixes, or exact author names.
- Select a group with `--group backend`. Author identities can also be canonicalized with a standard git `.mailmap` in each repo.

## Features

- ASCII art banner with vertical color gradient
- Streaming repository-scan loader that surfaces unreadable repos as they load
- Gradient activity bars with trailing glow; gold/silver/bronze rank styling
- Detected AI attribution in the leaderboard, monthly activity, and repository breakdown
- Bot contributors counted and tagged (`BOT`), with a one-key toggle to hide them
- Per-contributor drill-down: repo breakdown, gap-aware monthly timeline, neon contribution heatmap, and derived metrics (active days, first/last commit, removed/added ratio)
- Scrollable, height-aware leaderboard with incremental `/` search — every contributor reachable
- Config file with named `--group`s, glob excludes, and recursive scan depth
- Native `.mailmap`, generated/vendored file filtering, deterministic ordering, and rename/copy detection
- Git worktree detection (automatically skipped during repo discovery); symlinked repo directories are followed and deduplicated

## Development

```bash
cargo build --locked
cargo test --locked
cargo fmt --all -- --check
cargo clippy --all-targets --locked -- -D warnings
```

`make check` runs formatting, Clippy, and tests. `make build` creates the release binary at `target/release/bigboard`.

Tests use synthetic Git repositories with known expected counts, including ambiguous refs, unusual filenames, duplicate clones, coauthors, shallow history, and merges. CI runs the Rust suite on Linux and macOS, and packages both systems on x86-64 and ARM64.

Version 0.8 removes `--export`. The earlier Go comparison suite intentionally preserved defects and is no longer the correctness target.

## License

MIT
