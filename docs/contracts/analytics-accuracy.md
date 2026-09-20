---
steward_contract: "3"
id: "analytics-accuracy"
title: "Make contribution analytics accurate and explicit"
revision: 1
state: draft
created_at: "2026-09-20T19:36:13.022Z"
approved_at: null
approved_by: null
frozen_body_sha256: null
supersedes: null
---

# Make contribution analytics accurate and explicit

## Outcome

Report trustworthy team contribution activity with explicit identity attribution, consistent counting, and visible data limitations.

## Context

The user explicitly authorized implementation in a fresh worktree, local validation, and a pull request after discussing these decisions. This draft records that conversational authorization; it is not a separately approved Steward artifact.

Implementation was merged on 2026-09-20 in [PR #18](https://github.com/richhaase/bigboard/pull/18) at [`4c726d1`](https://github.com/richhaase/bigboard/commit/4c726d1ed02cd5b6d4acac5ef0f0aed34b65e0af). See the [delivery and validation record](../analytics.md#revision-and-validation). The draft metadata describes this scope record's approval status, not unfinished implementation.

## Scope

Replace the port's retained analytics defects with the agreed behavior. Preserve the terminal dashboard and its existing navigation. Metrics describe activity, not productivity or business value. Analysis uses Git history already available locally.

## Acceptance

- AC1: Names alone do not merge contributors. Matching canonical emails and repository mailmaps establish identity; the user can press M on a contributor, select another identity, choose a display name, and confirm a mapping that persists across sessions and applies across repositories. Filtering and drill-down retain contributor identity independently of display names.
- AC2: The dashboard defaults to landed default-branch history and offers a toggle including unmerged branch activity. Identical commits count once across the board while retaining repository associations. Additional merge-resolution work receives credit without recounting branch changes.
- AC3: Human coauthors receive separate coauthored participation counts. Authored commits and line changes remain credited to the primary author, and board-wide commit totals remain unique.
- AC4: AI attribution uses specific agent identities, recognized coauthor metadata, and user overrides, with no built-in company-domain classification. The metric is described as detected attribution and sorting retains full ratio precision.
- AC5: Reporting defaults to UTC and supports a saved user-configured timezone consistently across daily/monthly aggregation and heatmaps. One query cutoff excludes future-dated commits.
- AC6: Git branch/tag ambiguity, quoted and renamed paths, ambient settings, and valid separate-Git-directory discovery do not corrupt collection. Shallow or failed scans visibly qualify available totals; unknown line counts remain identified as unknown.
- AC7: The interface labels additions plus removals as Lines changed and removals divided by additions as Removed/added ratio, showing N/A when additions are zero. The unused --export feature is removed.
- AC8: The completed revision is validated locally and submitted as a pull request.
