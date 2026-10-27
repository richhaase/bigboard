# Big Board

A terminal dashboard for understanding activity across your GitHub repositories:
where change happened, who contributed, and which pull requests need attention.
Inspired by Hiro Protagonist's Big Board in Neal Stephenson's *Snow Crash*.

![Activity board with repository change bars and scoped contributors](docs/images/activity.png)

*The real terminal renderer with synthetic example data. See the
[visual guide](docs/visual-guide.md) for all three boards and their controls.*

## Install and start

You need Git 2.31 or newer and an installed, signed-in
[GitHub CLI](https://cli.github.com/) (`gh`) with access to your repositories.

```bash
brew install --cask richhaase/tap/bigboard
bigboard
```

On first launch, choose your personal GitHub account, organizations, or both.
Select repositories, review the choices, and save. You do not need to write a
configuration file. Press `,` on any board to revisit Settings.

Big Board maintains its own repository cache. It reads default-branch history
and open PRs; your developer checkouts and working branches are untouched.
Existing cached history appears before the startup refresh finishes.

To install from source:

```bash
go install github.com/richhaase/bigboard/cmd/bigboard@latest
```

## Three views of the same work

| Board | Key | Start here to… |
| --- | --- | --- |
| Activity | `a` | Compare change volume across repositories, then drill into directories and files. The lower pane shows who contributed to the highlighted scope. |
| Contributors | `c` | Compare contributors, then inspect where one person worked and when. |
| PRs | `p` | Browse open PRs, review/check status, authors, and work requesting your attention. |

The boards share date and repository viewing scope, while keeping their own
filters, sorting, and navigation position. Counts describe activity, not
productivity. Bars always show added and removed lines, even when sorted by
another measure.

| Key | Action |
| --- | --- |
| `↑` / `↓`, `PgUp` / `PgDn` | Select or scroll |
| `Enter` / `→` | Open or drill down |
| `Esc` / `←` | Return without clearing applied filters |
| `f` / `t` | Filters / Date range |
| `/` | Find in the current list |
| `s` / `S` | Change sort / reverse order |
| `r` | Refresh repository history and PRs |
| `e` | Data status and diagnostics |
| `?` / `F1` | Help for the current screen |
| `q` | Quit |

Settings, filters, repository controls, and PR details open as modals. While typing,
shortcut letters enter text; use `F1` for help and `Ctrl+C` to quit.

## Learn more

- [Visual guide](docs/visual-guide.md): Activity drill-down, Contributors, PRs, and Filters.
- [User reference](docs/reference.md): controls, settings, identity names/merges, bookmarks, cache behavior, configuration, and export.
- [Development](CLAUDE.md): architecture, implementation contracts, and checks.

The CLI also supports `--config`, `--group`, `--export`, and `--version`.
For example, `bigboard --export > board.json` exports all-time contributor stats
from an existing cache without network requests. See the
[CLI reference](docs/reference.md#command-line) for requirements.

## Build locally

```bash
make build
./bin/bigboard
```

Run `make test` for the test suite; `make fmt-check lint vet` covers formatting
and static checks.

## License

[MIT](LICENSE)
