---
steward_contract: "3"
id: "rust-port"
title: "Port Big Board to Rust"
revision: 1
state: draft
created_at: "2026-09-20T18:23:17.837Z"
approved_at: null
approved_by: null
frozen_body_sha256: null
supersedes: null
---

# Port Big Board to Rust

## Outcome

Provide a working Rust revision of Big Board matching the existing Go application.

## Context

The user explicitly authorized construction in an isolated worktree, local validation, and a pull request on completion. This draft records that conversational scope; it is not a separately approved Steward contract.

Implementation was merged on 2026-09-20 in [PR #17](https://github.com/richhaase/bigboard/pull/17) at [`821f85d`](https://github.com/richhaase/bigboard/commit/821f85d53b86649df7f87c41aea73d2009afad40). See the [delivery and validation record](../rust-port.md). The draft metadata describes this scope record's approval status, not unfinished implementation.

## Scope

Replace the Go application with Rust. Preserve current contribution analytics, CLI/config/export behavior, interactive controls, and cyberpunk presentation. Record known analytics defects for later corrections rather than changing their semantics during the port.

## Acceptance

- AC1: The Rust application scans repositories and produces contributor JSON compatible with the Go application under equivalent inputs and settings.
- AC2: The terminal application supports the existing leaderboard, contributor details, time ranges, sorting, search, bot visibility, repository selection, refresh, and cancellation.
- AC3: The application can be built, tested, and packaged using the Rust toolchain, and installation documentation describes the Rust revision.
- AC4: The known analytics issues remain recorded as follow-up work, with no intentional analytics policy changes in this revision.
- AC5: The completed port is validated locally and submitted as a pull request.
