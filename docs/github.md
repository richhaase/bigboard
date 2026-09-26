# GitHub source

Big Board can discover repositories through your existing GitHub CLI account and display them in the same contributor dashboard as local repositories. GitHub mode requests lightweight activity summaries; local mode provides deeper Git analysis. Their measurement differences are described below.

## Getting started

Install the [GitHub CLI](https://cli.github.com/), then authenticate once with `gh auth login` if needed. An existing browser login by itself is not a CLI login. Private repositories require permission to read their contents; organization SSO policies may require authorizing the CLI account.

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

For GitHub Enterprise, set `GH_HOST` to the hostname of your signed-in GitHub CLI account. Big Board uses that host for discovery and activity API requests. Standard GitHub CLI environment credentials are also respected.

## Lightweight activity and local analysis

Big Board has two sources:

- **GitHub API:** commit summaries from the selected repositories' default branches, limited to the chosen date range. No clone, fetch, Git objects, patches, or working files are downloaded.
- **Local Git:** full analysis of repositories you have cloned yourself. Big Board never clones repositories on your behalf.

GitHub mode starts at the configured range (14 days by default). The arrow keys load another range; `ALL` explicitly requests all available API history from 1970 onward. Large windows require more pages and can hit GitHub's API limits. A load stops after 500 newly requested history pages (up to 50,000 summaries); that repository is marked failed rather than presenting partial totals. Choose a shorter range or clone it yourself for local analysis.

The board labels this source **GITHUB API** and displays **Commit date · all files · merges: ? lines**. It retains contributor filtering, sorting, identity merging, bot inclusion, repository breakdowns, timelines, and heatmaps. The meaning of the underlying measurements differs from local analysis:

| Measurement | GitHub API | Local Git |
| --- | --- | --- |
| Branch scope | Current default branch, including its ancestors | Landed or all locally available branches (`B`) |
| Activity date | Committer date, matching the API history window | Author date |
| Files | GitHub's additions/deletions across all files | Generated/vendor exclusions unless `all_files` is enabled |
| Merge commits | Authored participation counted; lines unknown (`?`) to avoid counting ancestor changes twice | Reconstructed resolution work where available; clean integration-only merges omitted |
| Identities | GitHub's commit author and coauthor metadata; saved explicit mappings | Git identities with local `.mailmap` processing; saved explicit mappings |
| Diff rules | GitHub's summary counts | Big Board's pinned Git diff rules |

Neither date is a claim about when work first landed. API counts should not be treated as interchangeable with local counts. Unknown non-merge diff statistics are also shown with `?`, and retried on the next load. Coauthored participation remains separate from authored lines; detected AI reflects recognized author/coauthor identities, not inferred AI usage. No source-code content or commit-message text is requested.

Identity merge candidates in GitHub mode come from the loaded window. Saved email mappings remain global and also apply in local mode. Reporting buckets still respect the configured timezone. The `B` control is available only in local mode; GitHub mode always uses the default branch.

`R` checks access and the current default-branch head, then reloads the selected window. `r` filters the already loaded repository set. Reopen `g` to change GitHub selections or return to local repositories; local exclusions and history scope are restored. The modes remain separate.

## Progress, refresh, and caching

The loading screen shows a spinner, elapsed time, active repositories, and completed API pages/commit summaries. Collection runs outside the terminal event loop; quitting cancels requests. Each load pins a single branch head throughout pagination. Failed requests, incomplete responses, and rate-limit errors exclude the affected repository from new totals; `r` shows its error. Previously cached totals are not silently substituted for failed loads. There is no offline fallback.

Only summaries are cached. Coverage is rounded to UTC day boundaries for reuse, then filtered to the exact selected window and shared cutoff before display. An unchanged head can reuse covered dates and fetch just missing date intervals. A changed head reloads the requested window, which also handles rebases, force-pushes, and default-branch changes safely. Switching GitHub accounts invalidates reuse. Unknown non-merge diff counts are retried even when the head is unchanged.

The snapshot is replaced atomically after every page succeeds. Concurrent instances may replace one another's cache coverage, but each load uses its own complete snapshot. Cache write failures do not hide successfully loaded data. Larger requested windows can take longer and occupy more summary storage; no Git object history accumulates.

## Local storage and migration

Summaries are stored under `$XDG_CACHE_HOME/bigboard/github-api` when set, otherwise `~/Library/Caches/bigboard/github-api` on macOS or `~/.cache/bigboard/github-api` elsewhere. Files are scoped by host and stable repository ID, with the authenticated account recorded for cache validation. They contain commit IDs, dates, author/coauthor names and emails, parent counts, and line counts. Private repository summaries remain private data; storage directories and files have restricted permissions. Credentials stay with `gh`.

Removing a repository from the selection stops requests but retains its summary cache. With Big Board closed, deleting `bigboard/github-api` clears these summaries. The next load requests them again. Saved selections and identity mappings live in the configuration directory and are unaffected.

**Upgrading from 0.10:** the old `bigboard/github` directory can contain full bare repositories and analysis snapshots. Version 0.11 neither reads nor updates them and does not delete them automatically. With Big Board closed, you can delete that old cache directory to reclaim its space. There is no managed-clone fallback or full-history download option.

## Validation

Automated fixtures cover paginated discovery and summaries, exact date filtering, pinned heads, coverage reuse and extension, account changes, failed requests, coauthor pagination, unknown merge counts, cancellation, and responsive terminal layouts. A separately invoked read-only smoke test checks the public Big Board repository using the current `gh` account:

```bash
cargo test github::tests::live_public_summary_scan -- --ignored --nocapture
```

The API implementation follows the [GitHub Commit schema](https://docs.github.com/en/graphql/reference/commits) and uses [GitHub CLI API requests](https://cli.github.com/manual/gh_api). Actual latency and access depend on repository size, selected range, GitHub availability, and account permissions.
