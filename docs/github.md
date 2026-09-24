# GitHub source

Big Board can discover repositories through your existing GitHub CLI account and display them in the same contributor dashboard as local repositories. The source changes how history is obtained; contribution accounting, identity merges, time ranges, bot filtering, generated-file exclusions, and landed/all-branch views use the existing analytics engine.

## Getting started

Install Git and the [GitHub CLI](https://cli.github.com/), then authenticate once with `gh auth login` if needed. An existing browser login by itself is not a CLI login. Private repositories require permission to read their contents; organization SSO policies may require authorizing the CLI account.

Run `bigboard --github` to open the repository picker. This entry point does not scan local paths and cannot be combined with positional paths or `--group`. Alternatively, press `g` on a local board; you can then return to that local repository set from the picker. Starting Big Board in a directory with no discovered local repositories also opens the picker.

The picker lists repositories accessible to the authenticated account as an owner, collaborator, or organization member. It follows all API pages. It does not claim to enumerate every public repository on GitHub or repositories hidden from your account. Archived repositories and forks remain available and are labeled. Disabled repositories are excluded.

| Key | Action in the GitHub picker |
| --- | --- |
| `↑/↓`, `j/k` | Move between repositories |
| `PgUp/PgDn`, `Home/End` | Move through long lists |
| `/` | Search repository names; `Enter` finishes editing, `Esc` clears the search |
| `o` | Cycle owner/organization filters, including ALL |
| `space` | Toggle a repository |
| `a` | Select/deselect all repositories visible under the current filter |
| `Enter` | Save the checked selection and analyze it |
| `R` | Reload the repository list |
| `l` | Return to the local repository set, when one was discovered |
| `Esc` | Cancel without changing the current board; exit if there is no board |
| `q`, `Ctrl-C` | Quit |

Selections persist across owner/search filters, so check the selected count before analyzing. The last checked selection is saved in `github.json` beside the default Big Board configuration, scoped to the GitHub host and account. Custom `--config` files do not move this selection file. No API token is saved by Big Board.

For GitHub Enterprise, set `GH_HOST` to the hostname of your signed-in GitHub CLI account. Big Board uses that host for both discovery and authenticated Git fetches. Standard GitHub CLI environment credentials are also respected.

## History, refresh, and accuracy

Big Board downloads full branch history for **only the repositories you select**, into bare Git repositories without checked-out working files. First sync can take time and use substantial disk space for large repositories. Subsequent refreshes fetch updates and prune deleted remote branches. Default-branch metadata is refreshed too, including custom default branch names and branch renames. Tags and pull-request-only refs are not part of the branch scope.

The board identifies the GitHub source in its context line. `R` fetches and analyzes its selected repositories again; `r` controls which of the already loaded repositories contribute to the current totals. Reopen `g` to add other GitHub repositories. Local and GitHub boards are separate source modes, and returning to local mode restores its repository exclusions.

The committed default-branch `.mailmap` is used in the cache. A local checkout with an uncommitted `.mailmap`, unpublished branches, or stale remote refs can therefore legitimately differ from the GitHub view. GitHub summary-statistics endpoints are not used for analytics, because their merge and large-repository behavior differs from Big Board's accounting.

Downloads and scans run away from the terminal event loop. Quit cancels ongoing network work. Cache refresh and analysis hold a per-repository lock so concurrent Big Board instances cannot change the refs underneath a scan. Interrupted initial downloads are not published as completed caches. A failed refresh excludes that repository from new totals rather than silently using stale data; inspect its error with `r`. There is no offline fallback for GitHub mode in this revision.

The sync screen shows an animated heartbeat, elapsed time, the active repository, and its current stage. Analysis reports completed commit batches and merge checks; the repository counter advances only when a repository finishes. The heartbeat indicates that the interface is responsive, not a percentage estimate for the download.

Line-change analysis uses bounded parallel batches with the same exhaustive copy/rename rules. A first scan of a large history can still take time. After a successful fetch, unchanged repositories can reuse saved analysis. History refs, default branch, collection preferences, Git/Big Board versions, effective Git configuration, and external mailmap/attribute inputs are checked before reuse. Changed inputs or an unreadable snapshot trigger fresh analysis; unsupported fingerprint queries also fall back to scanning. Local repositories continue to be scanned directly.

## Local storage

History is stored under `$XDG_CACHE_HOME/bigboard/github` when set, otherwise `~/Library/Caches/bigboard/github` on macOS or `~/.cache/bigboard/github` elsewhere. Cache directories are scoped by host and stable GitHub repository ID, so identically named repositories do not collide. They contain repository content, including private content when selected. Big Board restricts storage-directory permissions and leaves credential handling to `gh`.

Removing a repository from the selected set stops future downloads but retains its cache. With Big Board closed, you can delete its GitHub cache directory to reclaim space; the next selection downloads fresh history. Identity merge preferences and saved repository selections live in the configuration directory and are unaffected by deleting history caches.

Each managed repository may also contain `bigboard-analysis.json`, a derived analysis snapshot. It can be deleted while Big Board is closed to force fresh analysis without downloading history again.

## Validation

Tests compare local and cached committed history, exercise pagination, account-scoped selections, branch deletion/default-branch changes, cancellation and cache locks, and verify that the picker remains navigable on small terminals. Live validation uses the current CLI account and an isolated cache; access still depends on the user's network and repository permissions.
