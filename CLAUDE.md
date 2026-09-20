# Big Board project context

Cyberpunk terminal dashboard for contributor activity across Git repositories. Rust with Ratatui/Crossterm; Git operations invoke the local Git CLI.

## Architecture

- `src/main.rs`, `src/config.rs`: three CLI flags, strict JSON preferences, timezone, groups, paths and exclusions. No JSON export.
- `src/model.rs`: repository, identity, commit, scope, scan data and options.
- `src/git.rs`: snapshot branch history, streaming collection, generated-file filtering, mailmap/coauthors, detected AI, completeness and merge accounting.
- `src/identity.rs`: explicit user-global contributor mappings, atomic persistence with locking.
- `src/stats.rs`: identity aggregation, unique commit counting, participation, consistent calendar buckets, bot tags, filters and exact-ratio sorting.
- `src/scan.rs`: bounded eight-worker cancelable scan sessions.
- `src/tui/`: stable contributor selection, merge flow, history toggle, completeness notices, rendering and terminal lifecycle.

## Analytics invariants

Follow `docs/analytics.md` and the user-approved conversational scope recorded in `docs/contracts/analytics-accuracy.md`. Names are labels, never automatic identity joins. Commit totals are globally unique across selected repositories, whose subtotals may overlap. Human coauthored credit is separate from authored commits/lines. Missing measurements must stay visibly unknown. Calendar buckets use the configured timezone and a shared cutoff. Metrics describe activity, not productivity.

Use only locally available Git history; do not fetch or modify the user's working tree/index/refs during analysis. Keep reconstruction objects temporary. Prefer targeted synthetic fixtures with known expected outcomes to old Go parity, which deliberately preserved defects.

## Checks

```bash
cargo fmt --all -- --check
cargo clippy --all-targets --locked -- -D warnings
cargo test --locked
cargo build --release --locked
```

CI checks Linux/macOS and release packaging on x86-64/ARM64. Preserve existing keyboard controls and cyberpunk presentation. `M` merges identities and `B` toggles history scope. The initial scope is landed work. Known-data qualifiers must remain visible at small terminal sizes.
