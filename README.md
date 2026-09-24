# Big Board

A cyberpunk-themed TUI for exploring team contribution activity across Git repositories. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

## Install

Big Board requires Git **2.31 or newer** on `PATH`. Git **2.38 or newer** supports reconstructed merge baselines; on older versions, merge activity is retained with unknown line counts. Partial clones require **2.45.1 or newer** so automatic object fetching can be disabled; older partial clones are excluded as scan failures. This revision is written in Rust with Ratatui and Crossterm. Building from source also requires a current stable Rust toolchain.

### From Source

```bash
git clone https://github.com/richhaase/bigboard.git
cd bigboard
cargo install --path . --locked
```

## Usage

Big Board is TUI-first: preferences live in the config file, and the CLI supports `--version`, `--config`, `--group`, and `--github`.

```bash
# Analyze current directory
bigboard

# Analyze specific repos or scan directories for repos
bigboard ~/src/repo1 ~/src/repo2
bigboard ~/src/

# Use a named group from your config
bigboard --group backend

# Choose repositories from GitHub using your existing gh login
bigboard --github

# Use an alternate config file
bigboard --config ./bigboard.json

# Print version and exit
bigboard --version
```

The time range is chosen interactively (`←/→`) and defaults to 14 days. Set
`since` in the config to choose the initial range. Dates use UTC unless you set
`timezone` to an IANA timezone such as `America/Denver`.

Press `g` from a local board to choose GitHub repositories. If no local repositories are found, Big Board opens that picker automatically. It uses your existing GitHub CLI (`gh`) account, lets you search and filter by owner or organization, and remembers your checked repositories. Only checked repositories are downloaded into Big Board's managed history cache; `R` on a GitHub board refreshes their history before recalculating the same analytics. See [GitHub source](docs/github.md) for authentication, controls, and cache details.

## Analytics

- **Explicit identity:** matching canonical emails and Git `.mailmap` entries establish identity. Names alone never combine people. Highlight a contributor and press `M` to select another identity, choose a combined name, and confirm a merge. Saved merges apply across repositories and sessions. See the [step-by-step merge flow](docs/analytics.md#identity-and-collaboration).
- **History:** the default view shows landed work on the available default branch. Press `B` to include unmerged activity from local and remote-tracking branches already present on disk. Scanning does not fetch remote changes.
- **Unique commits:** identical commit IDs count once across the board, retaining their repository associations. Repository subtotals can overlap. Cherry-picks and rebases with different IDs remain distinct commits.
- **Collaboration:** authored and human-coauthored commit counts are separate. Line changes remain attributed to the primary author; coauthor metadata does not specify each person's line contribution.
- **Detected AI:** known agent identities and recognized coauthor metadata indicate AI attribution. Human employees are not flagged solely by an AI company's domain. User-configured exact emails and domain overrides remain available. Missing attribution does not prove AI was absent.
- **Activity metrics:** Lines changed means additions plus removals. Removed/added ratio is N/A with zero additions or unknown line counts. These metrics describe activity, not productivity or business value.
- **Completeness:** shallow history, scan failures, and unallocatable merge-resolution line counts are visibly qualified. Unknown counts are not treated as measured zero. Clean integration-only merges are omitted when Git can reconstruct their baseline; extra merge edits receive credit where measurable.
- **Bots and generated files:** bots are included and tagged by default; `b` excludes them from contributor rows and displayed totals. Generated/vendor files are excluded from line counts by default; `all_files` includes them.

See [analytics behavior](docs/analytics.md) for the counting rules and limitations. The [0.8 merge and validation record](docs/analytics.md#revision-and-validation) documents the completed revision. The [resolved original audit](docs/analytics-correction-backlog.md) and [Rust port record](docs/rust-port.md) remain historical references.

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
| `b` | Include/exclude bots from contributor rows and totals |
| `B` | Toggle landed / all-branch activity |
| `M` | Merge the selected contributor with another identity |
| `r` | Open repository selection and details from the board |
| `g` | Choose GitHub repositories (or return to local repos from that picker) |
| `space` | Toggle a loaded repo in/out (within repository selection) |
| `PgUp/PgDn` | Scroll contributor detail content or selected repository details |
| `Home/End` | Jump to the top/bottom of contributor detail content |
| `R` | Refresh (re-scan local repos, or fetch then scan selected GitHub repos) |
| `q` | Quit (clears an active filter first) |

In the contributor detail view, `PgUp/PgDn` scroll the content and `Home/End` jump to its top/bottom while the header and footer stay fixed. `↑/↓` still step to the previous/next contributor, and `←/→` change the time range. Search (`/`) narrows the visible rows without changing board totals. History, time, bot, sorting, and repository selections apply to the current session; use the config file for supported startup preferences.

Incomplete line counts retain the `?` marker. Alerts identify repository scan failures or unavailable default-branch history; per-commit and attribution warning logs are not displayed. Press `r` to inspect repository paths and full scan errors; from contributor details, press `Esc` first. Use `↑/↓` or `j/k` to select a repository and `PgUp/PgDn` to page its details while the list and controls stay visible. Failed repositories remain inspectable but cannot be toggled into the totals. Both `Enter` and `Esc` apply repository selections and return to the board.

The dashboard keeps its neon cyberpunk panels and adapts to terminal size. If the board cannot fit its context, controls, and a contributor row, it shows a resize prompt.

## Config file

The optional config file is `$XDG_CONFIG_HOME/bigboard/config.json` when `XDG_CONFIG_HOME` is set, otherwise `~/.config/bigboard/config.json`. `--config` selects a different file. Restart Big Board after editing preferences, including the timezone; `R` rescans repositories using the preferences already loaded.

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

## Upgrading to 0.8

- Remove `fuzzy: true` (or change it to `false`) and use `M` for explicit identity merges. Equal names with different emails now stay separate.
- `--export` is removed. The interactive dashboard is the supported interface.
- Expect totals to change: shared commit IDs count once, landed work is the default, human coauthors have separate participation counts, and incomplete measurements are qualified. Calendar buckets now use UTC unless configured otherwise.
- Saved identity mappings apply globally, including when using `--config` or named repository groups. See [analytics behavior](docs/analytics.md) for the full rules.

## Features

- ASCII art banner with vertical color gradient and neon framed activity panels
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
For local builds, the version shown in the dashboard and by `--version` comes from `Cargo.toml`; update `Cargo.lock` with it, and use a matching `vX.Y.Z` tag when publishing a release. Release builds reject tags that disagree with the package version.

Tests use synthetic Git repositories with known expected counts, including ambiguous refs, unusual filenames, duplicate clones, coauthors, shallow history, and merges. CI runs the Rust suite on Linux and macOS, and packages both systems on x86-64 and ARM64.

The earlier Go comparison suite intentionally preserved defects and is no longer the correctness target. See the [historical port record](docs/rust-port.md) to reproduce that comparison at its original revision.

## License

MIT
