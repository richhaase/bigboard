# Big Board

A cyberpunk-themed TUI for assessing contributor volumes across git repositories. Inspired by Hiro Protagonist's Big Board from Neal Stephenson's *Snow Crash*.

## Install

### Homebrew

```bash
brew install --cask richhaase/tap/bigboard
```

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

# Exclude specific repos
bigboard --exclude vendor-lib ~/src/
```

## Controls

| Key | Action |
|-----|--------|
| `↑/↓` `j/k` | Navigate rows |
| `←/→` `h/l` | Cycle time range (1d / 7d / 14d / 30d / 90d / 1y / all) |
| `Enter` | Drill into selected contributor |
| `Esc` | Back / Quit |
| `s` | Cycle sort column |
| `r` | Open repo inclusion/exclusion overlay |
| `q` | Quit |

## Features

- ASCII art banner with vertical color gradient
- Gradient impact bars with trailing glow effect
- Gold/silver/bronze rank styling for top 3 contributors
- Per-contributor drill-down with repo breakdown and activity timeline
- Interactive repo inclusion/exclusion overlay
- Time range filtering (1d through all-time)
- Git worktree detection (automatically skipped during repo discovery)

## License

MIT
