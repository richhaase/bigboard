---
name: configure
description: Inspect and update BigBoard configuration when the user asks to set up BigBoard, change its settings, add or remove collected repositories, reuse native BigBoard sources, or change a board's date range, repository filter, or comparison.
---

# BigBoard Configure

Invoke this workflow as `bigboard:configure`, or ask naturally to change
BigBoard configuration.

Use the bundled BigBoard tools to turn the user's requested configuration change
into saved settings. Inspect the relevant current state first. If the request is
general, explain what is configured and ask which change they want. When the
user already supplied the necessary choices, apply them without another approval
step. Keep unrelated settings intact.

## Collection sources

Sources are shared across all boards. Call `list_sources` for the saved
repositories, local clone paths, and configuration revision.
Skip a save when the requested selection already matches these settings.

- For explicit clones, call `configure_sources` with absolute existing local
  `paths` and the current `expected_revision`. This replaces the entire saved
  selection: an add/remove request must include all retained paths as well as
  the requested additions. A repository URL is not a clone path; ask for the
  existing local path rather than cloning it.
- When the user chooses their native BigBoard selection, use
  `configure_sources` with `import_native:true` and `expected_revision`, omitting
  `paths`. This copies the native selection read-only and replaces plugin sources.
- Source changes clear collected evidence. Describe the saved result and use
  `list_sources` to verify the resulting selection and revision. On a revision
  conflict, reread and reconcile the requested change with the latest selection.

Collect once when needed to validate the new sources. If an active board is
known, check its `read_board` result for an in-flight `refresh`; watch it instead
of starting another. Activating Pull requests also refreshes sources automatically.
Otherwise use `refresh_sources`, allowing up to 150 seconds, and report coverage
gaps separately from successfully saving configuration. Honor any explicit
request to save without collecting; an active PR view may need to be left before
changing sources because it can start collection automatically.

## Board viewing settings

Viewing changes affect one board, not the saved collection. Resolve its
`boardId` from the app's background context or the user's supplied ID. If the
intended board is unclear, ask them to select it; create another board only when
requested. Pass the ID as `board_id` to the tools.

Call `read_board`, then `update_board` with the current `expected_revision` and
only the requested fields:

- `days`: 7 or 14.
- `repository`: `all` or a configured repository ID returned by `list_sources`.
- `compare`: whether to compare the preceding equal-length period.

Verify the resulting query and revision. Reconcile conflicts against fresh
state. These tools do not switch the active tab or change collected sources.

## Capability limits

The tools do not configure global viewing defaults, refresh cadence, GitHub
account authentication, credentials, or contributor/team identity mappings.
Explain the relevant limit rather than claiming a setting changed or editing
repository code, host configuration, credentials, or clones as a workaround.
Native imports do not modify native settings. Configuration help alone does
not authorize changes beyond the user's requested settings.
