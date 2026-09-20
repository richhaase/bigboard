# Big Board project context

Cyberpunk terminal dashboard for contributor activity across Git repositories. The application is Rust, using Ratatui with the Crossterm backend; Git operations invoke the Git CLI.

## Architecture

- `src/main.rs`: CLI entry and JSON export.
- `src/config.rs`: strict JSON config, the four existing flags, groups, paths, exclusions, preset validation.
- `src/model.rs`: repository, commit, and analysis option types.
- `src/git.rs`: repository discovery, default branch, streaming Git log parsing, path filters, AI identity matching, cancellation and timeout.
- `src/stats.rs`: identity union, aggregation, bot tagging, filters, sorting, derived metrics.
- `src/scan.rs`: bounded eight-worker repository scan sessions for export and TUI.
- `src/tui/`: application state, Ratatui rendering, terminal lifecycle and keyboard handling.
- `scripts/check_parity.py`: compares the Rust executable with the immutable Go reference.

## Migration scope

This revision preserves Go behavior. Do not silently correct analytics during port maintenance. The deferred findings and policy decisions are in `docs/analytics-correction-backlog.md`. The immutable Go reference is recorded in `docs/rust-port.md` and CI.

## Checks

```bash
cargo fmt --all -- --check
cargo clippy --all-targets --locked -- -D warnings
cargo test --locked
cargo build --release --locked
```

Run `python3 scripts/check_parity.py --reference /path/to/go-bigboard` after building the debug binary to check the end-to-end migration fixtures. The shipped application needs Rust to build and Git at runtime; Go is used only for reference validation.

## UI conventions

Use contributor in visible labels. Preserve cyberpunk colors, the block banner, gold/silver/bronze ranks, negative net values in red, and gradient impact bars. Keep the original keyboard controls and static presentation. JSON export covers all time; interactive time/repository filters are computed in memory. Git scans are concurrent and cancelable, with a 120-second per-repository deadline.
