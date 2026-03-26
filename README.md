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
```

## Controls

| Key | Action |
|-----|--------|
| `↑/↓` `j/k` | Navigate rows |
| `←/→` `h/l` | Cycle time range (30d / 90d / 1y / all) |
| `Enter` | Drill into selected repo |
| `Esc` | Back / Quit |
| `s` | Cycle sort column |
| `Tab` | Toggle focus (table / repo tags) |
| `q` | Quit |

## License

MIT
