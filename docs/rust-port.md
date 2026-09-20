# Rust revision

The user requested an isolated, behavior-preserving port before correcting the analytics. The Go reference is commit `a0acd7677837fff81eb10d31afc3a6b030362009`. The [analytics correction backlog](analytics-correction-backlog.md) records deferred work. The scope record in [the draft contract](contracts/rust-port.md) documents the conversational authorization; it is not a separately approved Steward contract.

## Structure

| Rust module | Responsibility | Go reference |
| --- | --- | --- |
| `src/main.rs`, `src/config.rs` | Four CLI flags, JSON config, path/group selection, headless export | `cmd/bigboard` |
| `src/git.rs` | Git CLI discovery, ref selection, mailmap-aware streaming log parsing, AI classification, timeout/cancellation | `git` |
| `src/model.rs`, `src/stats.rs` | Shared records, identity resolution, bot classification, filters, aggregation, sorting | `stats` and Git record types |
| `src/scan.rs` | Bounded eight-worker scan sessions shared by export and TUI | Concurrent scan paths in CLI and TUI |
| `src/tui` | Ratatui/Crossterm event loop, screens, styles, keyboard state, loading | Bubble Tea/Lipgloss `tui` |

The Git executable remains the source of repository semantics. The port does not substitute a Git library or fetch remote history. Records remain in memory and refresh rescans repositories; no persistence layer is introduced.

Ratatui provides styled cells, terminal layout, and a test backend; Crossterm handles terminal input, raw mode, and the alternate screen. See the [Ratatui backend documentation](https://ratatui.rs/concepts/backends/) for the supported pairing.

## Compatibility boundary

- Config keys, four command-line flags, group/path precedence, repository labels, and JSON contributor fields are preserved.
- Identity resolution, generated-file exclusions, bots, AI attribution, timestamps, time windows, and sorting preserve the Go revision, including recorded shortcomings.
- Existing screens, time ranges, keyboard controls, cyberpunk palette, banner, contribution bars, timeline, and heatmap are retained. Rendering is implemented with Ratatui; byte-for-byte ANSI output is not a compatibility target.
- The Go source remains available in Git history and is built in CI as an immutable reference. Rust is the shipped application; Go is needed only for the differential reference check.
- Release version/build metadata identifies the Rust revision and therefore differs from the Go executable.

## Validation

Run `cargo test --locked` for module, configuration, scan coordination, and TUI tests. Run `cargo fmt --all -- --check` and `cargo clippy --all-targets --locked -- -D warnings` for Rust checks.

Build the reference and compare complete parsed JSON and exit status with:

```bash
reference_dir=$(mktemp -d)
git archive a0acd7677837fff81eb10d31afc3a6b030362009 | tar -x -C "$reference_dir"
(cd "$reference_dir" && go build -o "$reference_dir/go-bigboard" ./cmd/bigboard)
cargo build --locked
python3 scripts/check_parity.py --reference "$reference_dir/go-bigboard"
```

The differential fixtures cover every sort mode, generated files, AI/bot overrides, fuzzy identities, time-independent export, config/group precedence, duplicate repository labels, exclusions, symlinks, mailmap, quoted paths, Git settings, tag collisions, shallow history, empty repositories, partial failures, and invalid input. Unit tests additionally cover deterministic identity resolution and TUI state transitions.

## Local verification record

Validated on macOS ARM64 with Rust 1.98.1:

- 69 Rust unit tests passed; formatting and Clippy with warnings denied passed.
- 36 end-to-end Go/Rust scenarios produced equal parsed JSON and exit status.
- The real Big Board repository also produced equal contributor JSON for its 62-commit history.
- Additional isolated differential checks covered 120 aggregation fixtures, 358 CLI argument sequences, 266 JSON config inputs, and 3,192 glob pattern/name pairs.
- A pseudo-terminal session exercised loading, leaderboard/detail views, resize, search, sorting, bot visibility, repository toggles, time changes, refresh, and quit. It verified idle output stays quiet and normal exit restores terminal modes and the alternate screen.
- Automatic theme detection selected the light palette from an OSC11 response and fell back to dark after the bounded query timed out. Both pseudo-terminal sessions exited successfully and restored configured terminal modes, cursor, and alternate screen.
- The optimized release build succeeded; the dependency audit reported no vulnerabilities and workflow validation passed.

Release packaging preserves Linux and macOS on x86-64 and ARM64. Linux archives use musl and are checked for a dynamic loader dependency. The packaging workflow runs on pull requests, but publication is restricted to tags; prerelease tags remain prereleases. The nonlocal release targets require their CI runs for validation.

The Ratatui leaderboard compacts its header at short terminal heights to keep rows reachable, and the repository overlay follows the selected row. Oversized contributor details preserve Go’s last-screenful clipping. No analytics policy is changed.
