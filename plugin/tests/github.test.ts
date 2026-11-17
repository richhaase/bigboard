import assert from "node:assert/strict";
import test from "node:test";
import { createGitHubCollector, type GitHubCollectionProgress, type GitHubDependencies } from "../src/sources/github";
import type { ConfiguredRepository } from "../src/shared";

const repository: ConfiguredRepository = { id: "example/alpha", path: "/synthetic/alpha", remote: "origin", defaultBranch: "main" };
const other: ConfiguredRepository = { ...repository, id: "example/beta", path: "/synthetic/beta" };
const interval = { since: "2026-11-01T00:00:00.000Z", until: "2026-11-15T00:00:00.000Z" };
const viewer = { id: 17, login: "fixture-user" };

function pull(number: number, overrides: Record<string, unknown> = {}) {
  return {
    number, title: `Synthetic change ${number}`, user: { login: "fixture-contributor" }, state: "open", draft: false,
    created_at: "2026-01-01T00:00:00Z", updated_at: "2026-11-14T12:00:00Z", merged_at: null,
    base: { ref: "main" }, head: { sha: "a".repeat(40) }, ...overrides,
  };
}

function response(value: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(value), { status, headers: { "content-type": "application/json", ...headers } });
}

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>(done => { resolve = done; });
  return { promise, resolve };
}

async function waitFor(condition: () => boolean): Promise<void> {
  const deadline = Date.now() + 2000;
  while (!condition()) {
    assert.ok(Date.now() < deadline, "Collector did not reach the expected state.");
    await new Promise(resolve => setTimeout(resolve, 1));
  }
}

function harness(route: (url: URL, init: RequestInit) => Response | Promise<Response>, limits: Omit<GitHubDependencies, "request" | "token"> = {}) {
  const calls: URL[] = [];
  const collect = createGitHubCollector({
    token: async () => "synthetic-credential",
    request: async (url, init) => {
      calls.push(url);
      assert.equal(url.origin, "https://api.github.com");
      assert.equal(init.method, url.pathname === "/graphql" ? "POST" : "GET");
      assert.equal(init.redirect, "error");
      assert.equal(new Headers(init.headers).get("Authorization"), "Bearer synthetic-credential");
      return url.pathname === "/user" ? response(viewer) : route(url, init);
    }, ...limits,
  });
  return { calls, collect };
}

function reviewPull(number: number, overrides: Record<string, unknown> = {}) {
  return pull(number, { node_id: `${repository.id}#${number}`, requested_reviewers: [], requested_teams: [], assignees: [], ...overrides });
}

function reviewNode(number: number, overrides: Record<string, unknown> = {}) {
  return { __typename: "PullRequest", id: `${repository.id}#${number}`, number, headRefOid: "a".repeat(40), repository: { nameWithOwner: repository.id }, reviewDecision: "APPROVED", reviewRequests: { totalCount: 0, pageInfo: { hasNextPage: false }, nodes: [] }, assignees: { totalCount: 0, pageInfo: { hasNextPage: false }, nodes: [] }, ...overrides };
}

function graphIds(init: RequestInit): string[] {
  const body = JSON.parse(String(init.body));
  assert.match(body.query, /^query BigBoardReviews/);
  assert.ok(!body.query.includes("mutation"));
  assert.ok(Array.isArray(body.variables.ids) && body.variables.ids.length <= 50);
  return body.variables.ids;
}

test("GitHub collection confirms the account and separates current open work from merge-date history", async () => {
  const { calls, collect } = harness((url) => url.searchParams.get("state") === "open"
    ? response([pull(1, { draft: true, created_at: "2001-01-01T00:00:00Z" })])
    : response([
      pull(2, { state: "closed", merged_at: interval.since }),
      pull(3, { state: "closed", merged_at: interval.until, updated_at: interval.until }),
      pull(4, { state: "closed", merged_at: "2026-10-31T23:59:59Z" }),
      pull(5, { state: "closed", merged_at: "2026-11-10T12:00:00Z", base: { ref: "release" } }),
      pull(6, { state: "closed" }),
    ]));
  const result = await collect([repository], interval);
  assert.deepEqual(result.account, { id: "17", login: "fixture-user" });
  assert.equal(calls[0].pathname, "/user");
  assert.deepEqual(result.pullRequests.map((row) => row.number), [1, 2]);
  assert.equal(result.pullRequests[0].state, "open");
  assert.equal(result.pullRequests[0].draft, true);
  assert.equal(result.pullRequests[0].createdAt, "2001-01-01T00:00:00.000Z");
  assert.equal(result.pullRequests[1].state, "merged");
  assert.equal(result.pullRequests[1].mergedAt, interval.since);
  assert.equal(result.pullRequests[1].url, "https://github.com/example/alpha/pull/2");
  assert.deepEqual(result.repositories, [{ id: repository.id, openComplete: true, mergedComplete: true, reviewComplete: false, accessDenied: false, error: null }]);
});

test("GitHub pagination follows local page numbers and stops the closed inventory at the update boundary", async () => {
  const { calls, collect } = harness((url) => {
    const page = url.searchParams.get("page");
    const open = url.searchParams.get("state") === "open";
    if (open && page === "1") return response([pull(1)], 200, { link: '<https://untrusted.test/ignored>; rel="next"' });
    if (open) return response([pull(2)]);
    if (page === "1") return response([pull(3, { state: "closed", merged_at: "2026-11-10T00:00:00Z" })], 200, { link: '<https://api.github.com/next>; rel="next"' });
    if (page === "2") return response([pull(4, { state: "closed", merged_at: "2026-10-20T00:00:00Z", updated_at: "2026-10-21T00:00:00Z" })], 200, { link: '<https://api.github.com/next>; rel="next"' });
    throw new Error("Unnecessary old-history request");
  });
  const result = await collect([repository], interval);
  assert.deepEqual(result.pullRequests.map((row) => row.number), [1, 2, 3]);
  assert.equal(calls.length, 5);
  assert.ok(calls.every((url) => url.origin === "https://api.github.com"));
  assert.equal(result.repositories[0].mergedComplete, true);
});

test("page and global request budgets return explicit incomplete coverage", async () => {
  const paged = harness(() => response([pull(1)], 200, { link: '<https://api.github.com/next>; rel="next"' }), { maximumPages: 1 });
  const limited = await paged.collect([repository], interval);
  assert.equal(limited.repositories[0].openComplete, false);
  assert.match(limited.repositories[0].error!, /page limit/);
  const budgeted = harness(() => response([pull(1)]), { maximumRequests: 2 });
  const exhausted = await budgeted.collect([repository, other], interval);
  assert.equal(budgeted.calls.length, 2);
  assert.equal(exhausted.repositories[0].openComplete, true);
  assert.equal(exhausted.repositories[0].mergedComplete, false);
  assert.equal(exhausted.repositories[1].openComplete, false);
  assert.match(exhausted.error!, /request budget/);
});

test("repository denial discards its partial inventory while retaining permitted repositories", async () => {
  for (const status of [403, 404]) {
    const { collect } = harness((url) => {
      if (url.pathname.includes("/beta/") && url.searchParams.get("state") === "closed") return response({ message: "private details" }, status);
      return response(url.searchParams.get("state") === "open" ? [pull(1)] : []);
    });
    const result = await collect([repository, other], interval);
    assert.deepEqual(result.pullRequests.map((row) => row.repository), [repository.id]);
    assert.equal(result.repositories[1].accessDenied, true);
    assert.equal(result.repositories[1].openComplete, false);
    assert.equal(result.repositories[1].mergedComplete, false);
    assert.ok(!JSON.stringify(result).includes("private details"));
  }
});

test("a 401 after successful collection invalidates the account and all PR evidence", async () => {
  const { collect } = harness((url) => url.pathname.includes("/beta/")
    ? response({ message: "sensitive response" }, 401)
    : response(url.searchParams.get("state") === "open" ? [pull(1)] : []));
  const result = await collect([repository, other], interval);
  assert.equal(result.account, null);
  assert.deepEqual(result.pullRequests, []);
  assert.ok(result.repositories.every((row) => !row.openComplete && !row.mergedComplete));
  assert.match(result.error!, /authentication/);
  assert.ok(!JSON.stringify(result).includes("sensitive response"));
});

test("rate limits halt further API calls and remain distinct from repository denial", async () => {
  for (const status of [403, 429]) {
    const { calls, collect } = harness(() => response({}, status, { "x-ratelimit-remaining": "0", "retry-after": "30" }));
    const result = await collect([repository, other], interval);
    assert.equal(calls.length, 3);
    assert.equal(result.account?.id, "17");
    assert.ok(result.repositories.every((row) => !row.accessDenied && !row.openComplete && !row.mergedComplete));
    assert.match(result.error!, /rate-limited/);
  }
});

test("temporary inventory failures retain bounded evidence without claiming complete coverage", async () => {
  const { collect } = harness((url) => {
    if (url.searchParams.get("state") === "closed") return response({}, 503);
    if (url.searchParams.get("page") === "1") return response([pull(1)], 200, { link: '<https://api.github.com/next>; rel="next"' });
    return response({}, 503);
  });
  const result = await collect([repository], interval);
  assert.equal(result.pullRequests.length, 1);
  assert.equal(result.repositories[0].openComplete, false);
  assert.equal(result.repositories[0].mergedComplete, false);
  assert.match(result.repositories[0].error!, /could not be reached/);
});

test("authentication errors and malformed rows do not leak diagnostics or claim full coverage", async () => {
  const collect = createGitHubCollector({
    token: async () => { throw new Error("synthetic-secret-marker"); },
    request: async () => { throw new Error("network must not run"); },
  });
  const absent = await collect([repository], interval);
  assert.equal(absent.account, null);
  assert.ok(!JSON.stringify(absent).includes("synthetic-secret-marker"));
  const { collect: malformed } = harness((url) => url.searchParams.get("state") === "open"
    ? response([pull(1), pull(2, { head: { sha: "invalid" } })]) : response([]));
  const partial = await malformed([repository], interval);
  assert.deepEqual(partial.pullRequests.map((row) => row.number), [1]);
  assert.equal(partial.repositories[0].openComplete, false);
  assert.equal(partial.repositories[0].mergedComplete, true);
});

test("request timeout and pre-cancellation return bounded incomplete results", async () => {
  const collect = createGitHubCollector({ token: async () => "synthetic-credential", request: async () => new Promise(() => {}), requestTimeoutMs: 10 });
  const keeper = setTimeout(() => {}, 100);
  try {
    const timedOut = await collect([repository], interval);
    assert.equal(timedOut.account, null);
    assert.match(timedOut.error!, /timed out/);
  } finally {
    clearTimeout(keeper);
  }
  const cancelled = new AbortController();
  cancelled.abort();
  let calls = 0;
  const skip = createGitHubCollector({ token: async () => { calls += 1; return "synthetic-credential"; } });
  const result = await skip([repository], { ...interval, signal: cancelled.signal });
  assert.equal(calls, 0);
  assert.match(result.error!, /cancelled/);
});

test("invalid scope is rejected before authenticating or issuing requests", async () => {
  let calls = 0;
  const collect = createGitHubCollector({ token: async () => { calls += 1; return "synthetic-credential"; } });
  const result = await collect([{ ...repository, id: "invalid/../../outside" }], interval);
  assert.equal(calls, 0);
  assert.equal(result.account, null);
  assert.match(result.error!, /canonical repository/);
  const traversal = await collect([{ ...repository, id: "example/.." }], interval);
  assert.equal(calls, 0);
  assert.equal(traversal.account, null);
});

test("a changing paginated inventory and oversized response remain explicitly incomplete", async () => {
  const duplicate = harness((url) => {
    if (url.searchParams.get("state") === "closed") return response([]);
    return response([pull(1)], 200, url.searchParams.get("page") === "1"
      ? { link: '<https://api.github.com/next>; rel="next"' } : {});
  });
  const moving = await duplicate.collect([repository], interval);
  assert.equal(moving.pullRequests.length, 1);
  assert.equal(moving.repositories[0].openComplete, false);
  const oversized = harness((url) => url.searchParams.get("state") === "open"
    ? response([pull(1, { title: "x".repeat(2 * 1024 * 1024) })]) : response([]));
  const bounded = await oversized.collect([repository], interval);
  assert.equal(bounded.pullRequests.length, 0);
  assert.equal(bounded.repositories[0].openComplete, false);
  assert.equal(bounded.repositories[0].mergedComplete, true);
});

test("one unavailable default branch does not prevent collecting other repositories", async () => {
  const { calls, collect } = harness((url) => response(url.searchParams.get("state") === "open" ? [pull(1)] : []));
  const result = await collect([{ ...repository, defaultBranch: "" }, other], interval);
  assert.equal(result.repositories[0].openComplete, false);
  assert.equal(result.repositories[0].mergedComplete, false);
  assert.match(result.repositories[0].error!, /default branch is unavailable/);
  assert.equal(result.repositories[1].openComplete, true);
  assert.equal(result.repositories[1].mergedComplete, true);
  assert.ok(calls.every((url) => !url.pathname.includes("/alpha/")));
  assert.deepEqual(result.pullRequests.map((row) => row.repository), [other.id]);
});

test("smaller PR pages retain the same 400-record inventory capacity without raising the byte limit", async () => {
  const { calls, collect } = harness((url) => {
    assert.equal(url.searchParams.get("per_page"), "50");
    if (url.searchParams.get("state") === "closed") return response([]);
    const page = Number(url.searchParams.get("page"));
    const rows = Array.from({ length: 50 }, (_, index) => pull((page - 1) * 50 + index + 1, {
      body: "synthetic description ".repeat(1_100),
    }));
    return response(rows, 200, page < 8 ? { link: '<https://api.github.com/next>; rel="next"' } : {});
  });
  const result = await collect([repository], interval);
  assert.equal(result.pullRequests.length, 400);
  assert.equal(result.repositories[0].openComplete, true);
  assert.equal(result.repositories[0].mergedComplete, true);
  assert.equal(calls.length, 10);
});

test("repository workers overlap within the three-request limit and report monotonic progress in stable result order", async () => {
  const repositories = Array.from({ length: 5 }, (_, index) => ({ ...repository, id: `example/project-${index}` }));
  const gates = repositories.map(() => deferred());
  const started = new Set<number>();
  const activeRepositories = new Set<number>();
  const progress: GitHubCollectionProgress[] = [];
  let active = 0;
  let maximumActive = 0;
  const { collect } = harness(async url => {
    const index = Number(url.pathname.match(/project-(\d+)/)![1]);
    assert.ok(!activeRepositories.has(index));
    activeRepositories.add(index);
    active += 1;
    maximumActive = Math.max(maximumActive, active);
    const open = url.searchParams.get("state") === "open";
    try {
      if (open) {
        started.add(index);
        await gates[index].promise;
      }
      return response(open ? [pull(index + 1)] : []);
    } finally {
      active -= 1;
      activeRepositories.delete(index);
    }
  });
  const pending = collect(repositories, { ...interval, onProgress: item => {
    progress.push(item);
    if (item.requests === 3) throw new Error("Synthetic observer failure");
  } });
  await waitFor(() => started.size === 3);
  assert.equal(maximumActive, 3);
  assert.deepEqual([...started], [0, 1, 2]);
  gates[2].resolve();
  await waitFor(() => started.has(3));
  gates[3].resolve();
  await waitFor(() => started.has(4));
  gates[4].resolve();
  gates[1].resolve();
  gates[0].resolve();
  const result = await pending;
  assert.equal(maximumActive, 3);
  assert.deepEqual(result.pullRequests.map(pr => pr.repository), repositories.map(repo => repo.id));
  assert.deepEqual(result.repositories.map(repo => repo.id), repositories.map(repo => repo.id));
  assert.ok(result.repositories.every(repo => repo.openComplete && repo.mergedComplete));
  assert.deepEqual(progress[0], { completedRepositories: 0, totalRepositories: 5, requests: 0 });
  assert.deepEqual(progress.at(-1), { completedRepositories: 5, totalRepositories: 5, requests: 11 });
  for (let index = 1; index < progress.length; index += 1) {
    assert.ok(progress[index].completedRepositories >= progress[index - 1].completedRepositories);
    assert.ok(progress[index].requests >= progress[index - 1].requests);
    assert.equal(progress[index].totalRepositories, 5);
  }
});

test("concurrent terminal failures stop queued repositories and respect the shared request budget", async () => {
  for (const kind of ["auth", "rate", "budget"] as const) {
    const repositories = Array.from({ length: 7 }, (_, index) => ({ ...repository, id: `example/project-${index}` }));
    const gates = repositories.map(() => deferred());
    const started = new Set<number>();
    let completed = 0;
    const { calls, collect } = harness(async url => {
      const index = Number(url.pathname.match(/project-(\d+)/)![1]);
      started.add(index);
      await gates[index].promise;
      if (index === 0 && kind === "auth") return response({}, 401);
      if (index === 0 && kind === "rate") return response({}, 429, { "retry-after": "30" });
      return response([pull(index + 1)]);
    }, kind === "budget" ? { maximumRequests: 4 } : {});
    const pending = collect(repositories, { ...interval, onProgress: item => { completed = item.completedRepositories; } });
    await waitFor(() => started.size === 3);
    gates[0].resolve();
    await waitFor(() => completed > 0);
    gates.forEach(gate => gate.resolve());
    const result = await pending;
    assert.equal(calls.length, 4);
    assert.deepEqual([...started].sort(), [0, 1, 2]);
    assert.ok(result.repositories.slice(3).every(repo => !repo.openComplete && !repo.mergedComplete && repo.error));
    assert.equal(result.repositories.some(repo => repo.accessDenied), false);
    assert.match(result.error!, kind === "auth" ? /authentication/ : kind === "rate" ? /rate-limited/ : /request budget/);
    if (kind === "auth") {
      assert.equal(result.account, null);
      assert.deepEqual(result.pullRequests, []);
      assert.ok(result.repositories.every(repo => !repo.openComplete && !repo.mergedComplete));
    }
  }
});

test("an overlapping 401 discards earlier successful repositories and ignores late successful responses", async () => {
  const repositories = [repository, other, { ...repository, id: "example/gamma" }];
  const denial = deferred();
  const late = deferred();
  let completed = 0;
  let lateReturned = false;
  const { calls, collect } = harness(async url => {
    if (url.pathname.includes("/beta/")) {
      await denial.promise;
      return response({}, 401);
    }
    if (url.pathname.includes("/gamma/")) {
      await late.promise;
      lateReturned = true;
    }
    return response(url.searchParams.get("state") === "open" ? [pull(1)] : []);
  });
  const pending = collect(repositories, { ...interval, onProgress: item => { completed = item.completedRepositories; } });
  await waitFor(() => completed === 1);
  denial.resolve();
  const result = await pending;
  assert.equal(lateReturned, false);
  assert.equal(result.account, null);
  assert.deepEqual(result.pullRequests, []);
  assert.ok(result.repositories.every(repo => !repo.openComplete && !repo.mergedComplete && /authentication/.test(repo.error!)));
  const callsAtCompletion = calls.length;
  late.resolve();
  await waitFor(() => lateReturned);
  assert.equal(calls.length, callsAtCompletion);
  assert.deepEqual(result.pullRequests, []);
  assert.equal(result.account, null);
});

test("cancellation releases active workers without starting queued repositories", async () => {
  const repositories = Array.from({ length: 6 }, (_, index) => ({ ...repository, id: `example/project-${index}` }));
  const gate = deferred();
  const controller = new AbortController();
  let active = 0;
  const { calls, collect } = harness(async () => {
    active += 1;
    await gate.promise;
    return response([pull(1)]);
  });
  const pending = collect(repositories, { ...interval, signal: controller.signal });
  await waitFor(() => active === 3);
  controller.abort();
  const result = await pending;
  assert.equal(calls.length, 4);
  assert.match(result.error!, /cancelled/);
  assert.ok(result.repositories.every(repo => !repo.openComplete && !repo.mergedComplete));
  gate.resolve();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(calls.length, 4);
  assert.deepEqual(result.pullRequests, []);
});

test("review metadata is batched with bounded concurrency and repository progress waits for enrichment", async () => {
  const gate = deferred();
  let graphRequests = 0;
  let active = 0;
  let maximumActive = 0;
  const progress: GitHubCollectionProgress[] = [];
  const { calls, collect } = harness(async (url, init) => {
    if (url.pathname === "/graphql") {
      graphRequests += 1;
      active += 1;
      maximumActive = Math.max(maximumActive, active);
      const ids = graphIds(init);
      await gate.promise;
      active -= 1;
      return response({ data: { nodes: ids.map(id => reviewNode(Number(id.split("#")[1]))) } });
    }
    if (url.searchParams.get("state") === "closed") return response([]);
    const page = Number(url.searchParams.get("page"));
    return response(Array.from({ length: page === 1 ? 50 : 11 }, (_, index) => reviewPull((page - 1) * 50 + index + 1)), 200, page === 1 ? { link: '<https://api.github.com/next>; rel="next"' } : {});
  });
  const pending = collect([repository], { ...interval, onProgress: value => { progress.push(value); } });
  await waitFor(() => graphRequests === 2);
  assert.equal(progress.at(-1)?.completedRepositories, 0);
  assert.equal(maximumActive, 2);
  gate.resolve();
  const result = await pending;
  assert.equal(calls.length, 6);
  assert.equal(graphRequests, 2);
  assert.equal(result.pullRequests.length, 61);
  assert.deepEqual(result.pullRequests.map(row => row.number), Array.from({ length: 61 }, (_, index) => index + 1));
  assert.ok(result.pullRequests.every(row => row.review?.decision === "approved" && row.review.reviewRequestsComplete && row.review.assigneesComplete));
  assert.equal(result.repositories[0].reviewComplete, true);
  assert.equal(progress.at(-1)?.completedRepositories, 1);
});

test("review decisions, requested users and teams, assignments, and incomplete connections stay distinct", async () => {
  const { collect } = harness((url, init) => {
    if (url.pathname === "/graphql") return response({ data: { nodes: graphIds(init).map(id => {
      const number = Number(id.split("#")[1]);
      if (number === 1) return reviewNode(number, { reviewDecision: "REVIEW_REQUIRED", reviewRequests: { totalCount: 2, pageInfo: { hasNextPage: false }, nodes: [{ requestedReviewer: { __typename: "User", login: "fixture-reviewer" } }, { requestedReviewer: { __typename: "Team", slug: "maintainers", organization: { login: "example" } } }] }, assignees: { totalCount: 1, pageInfo: { hasNextPage: false }, nodes: [{ login: "fixture-owner" }] } });
      if (number === 2) return reviewNode(number, { reviewDecision: "CHANGES_REQUESTED" });
      if (number === 3) return reviewNode(number, { reviewDecision: null });
      if (number === 4) return reviewNode(number, { reviewDecision: "UNSUPPORTED" });
      if (number === 5) return reviewNode(number, { reviewRequests: { totalCount: 1, pageInfo: { hasNextPage: false }, nodes: [{ requestedReviewer: { __typename: "Mannequin", login: "fixture-user" } }] } });
      if (number === 6) return reviewNode(number, { reviewRequests: { totalCount: 51, pageInfo: { hasNextPage: true }, nodes: Array.from({ length: 50 }, (_, index) => ({ requestedReviewer: { __typename: "User", login: `reviewer-${index}` } })) } });
      return reviewNode(number, { assignees: { totalCount: 2, pageInfo: { hasNextPage: true }, nodes: [{ login: "fixture-owner" }] } });
    }) } });
    return response(url.searchParams.get("state") === "open" ? Array.from({ length: 7 }, (_, index) => reviewPull(index + 1)) : []);
  });
  const result = await collect([repository], interval);
  const reviews = result.pullRequests.map(row => row.review!);
  assert.deepEqual(reviews.map(row => row.decision), ["review_required", "changes_requested", "none", "unknown", "approved", "approved", "approved"]);
  assert.deepEqual(reviews[0].requestedReviewers, ["fixture-reviewer"]);
  assert.deepEqual(reviews[0].requestedTeams, ["example/maintainers"]);
  assert.deepEqual(reviews[0].assignees, ["fixture-owner"]);
  assert.equal(reviews[0].reviewRequestsComplete, true);
  assert.deepEqual(reviews[4].requestedReviewers, []);
  assert.equal(reviews[4].reviewRequestsComplete, false);
  assert.equal(reviews[5].requestedReviewers.length, 50);
  assert.equal(reviews[5].reviewRequestsComplete, false);
  assert.equal(reviews[6].assigneesComplete, false);
  assert.equal(result.repositories[0].openComplete, true);
  assert.equal(result.repositories[0].reviewComplete, false);
  assert.match(result.repositories[0].error!, /Review metadata is incomplete/);
});

test("REST review request fallback remains explicit when enrichment is unavailable", async () => {
  const { collect } = harness(url => url.pathname === "/graphql" ? response({}, 503) : response(url.searchParams.get("state") === "open" ? [reviewPull(1, { requested_reviewers: [{ login: "fixture-reviewer" }], requested_teams: [{ slug: "maintainers" }], assignees: [{ login: "fixture-owner" }] })] : []));
  const result = await collect([repository], interval);
  assert.deepEqual(result.pullRequests[0].review, { decision: "unknown", requestedReviewers: ["fixture-reviewer"], requestedTeams: ["example/maintainers"], assignees: ["fixture-owner"], reviewRequestsComplete: true, assigneesComplete: true });
  assert.equal(result.repositories[0].reviewComplete, false);
  assert.equal(result.repositories[0].openComplete, true);
});

test("changed heads and mismatched GraphQL identities never attach review decisions", async () => {
  for (const changed of [{ headRefOid: "b".repeat(40) }, { id: "different-id" }, { number: 99 }, { repository: { nameWithOwner: other.id } }, { __typename: "Issue" }]) {
    const { collect } = harness(url => url.pathname === "/graphql" ? response({ data: { nodes: [reviewNode(1, changed)] } }) : response(url.searchParams.get("state") === "open" ? [reviewPull(1)] : []));
    const result = await collect([repository], interval);
    assert.equal(result.pullRequests.length, 1);
    assert.equal(result.pullRequests[0].review?.decision, "unknown");
    assert.equal(result.pullRequests[0].review?.reviewRequestsComplete, false);
    assert.equal(result.repositories[0].reviewComplete, false);
  }
});

test("GraphQL node denial removes the affected repository while field errors retain unknown review metadata", async () => {
  for (const kind of ["FORBIDDEN", "NOT_FOUND"]) {
    const { collect } = harness((url, init) => {
      if (url.pathname === "/graphql") return response({ data: { nodes: graphIds(init).map(id => id.startsWith(other.id) ? null : reviewNode(1)) }, errors: [{ type: kind, path: ["nodes", 1], message: "synthetic-private-diagnostic" }] });
      return response(url.searchParams.get("state") === "open" ? [reviewPull(1, { node_id: `${url.pathname.includes("/beta/") ? other.id : repository.id}#1` })] : []);
    });
    const result = await collect([repository, other], interval);
    assert.deepEqual(result.pullRequests.map(row => row.repository), [repository.id]);
    assert.equal(result.repositories[1].accessDenied, true);
    assert.equal(result.repositories[1].reviewComplete, false);
    assert.ok(!JSON.stringify(result).includes("synthetic-private-diagnostic"));
  }
  const { collect } = harness(url => url.pathname === "/graphql" ? response({ data: { nodes: [reviewNode(1, { reviewDecision: null })] }, errors: [{ type: "FORBIDDEN", path: ["nodes", 0, "reviewDecision"], message: "synthetic-private-diagnostic" }] }) : response(url.searchParams.get("state") === "open" ? [reviewPull(1)] : []));
  const partial = await collect([repository], interval);
  assert.equal(partial.pullRequests.length, 1);
  assert.equal(partial.pullRequests[0].review?.decision, "unknown");
  assert.equal(partial.repositories[0].accessDenied, false);
  assert.ok(!JSON.stringify(partial).includes("synthetic-private-diagnostic"));
});

test("an authentication failure during batched review enrichment purges inventory and ignores late responses", async () => {
  for (const mode of ["http", "graphql"]) {
    const denied = deferred();
    const late = deferred();
    let graphRequests = 0;
    let lateReturned = false;
    const { calls, collect } = harness(async (url, init) => {
      if (url.pathname === "/graphql") {
        graphRequests += 1;
        const ids = graphIds(init);
        const first = Number(ids[0].split("#")[1]);
        if (first === 51) {
          await denied.promise;
          return mode === "http" ? response({}, 401) : response({ errors: [{ type: "UNAUTHORIZED", message: "synthetic-private-diagnostic" }] });
        }
        if (first === 101) { await late.promise; lateReturned = true; }
        return response({ data: { nodes: ids.map(id => reviewNode(Number(id.split("#")[1]))) } });
      }
      if (url.searchParams.get("state") === "closed") return response([]);
      const page = Number(url.searchParams.get("page"));
      return response(Array.from({ length: page < 3 ? 50 : 1 }, (_, index) => reviewPull((page - 1) * 50 + index + 1)), 200, page < 3 ? { link: '<https://api.github.com/next>; rel="next"' } : {});
    });
    const pending = collect([repository], interval);
    await waitFor(() => graphRequests === 3);
    denied.resolve();
    const result = await pending;
    assert.equal(result.account, null);
    assert.deepEqual(result.pullRequests, []);
    assert.equal(result.repositories[0].reviewComplete, false);
    assert.equal(lateReturned, false);
    assert.match(result.error!, /authentication/);
    assert.ok(!JSON.stringify(result).includes("synthetic-private-diagnostic"));
    const count = calls.length;
    late.resolve();
    await waitFor(() => lateReturned);
    assert.equal(calls.length, count);
    assert.deepEqual(result.pullRequests, []);
  }
});

test("review enrichment shares request budgets and handles cancellation without claiming metadata completeness", async () => {
  const budgeted = harness(url => response(url.searchParams.get("state") === "open" ? [reviewPull(1)] : []), { maximumRequests: 3 });
  const exhausted = await budgeted.collect([repository], interval);
  assert.equal(budgeted.calls.length, 3);
  assert.equal(exhausted.pullRequests.length, 1);
  assert.equal(exhausted.pullRequests[0].review?.decision, "unknown");
  assert.equal(exhausted.repositories[0].openComplete, true);
  assert.equal(exhausted.repositories[0].reviewComplete, false);
  assert.match(exhausted.error!, /request budget/);
  const entered = deferred(), gate = deferred();
  const controller = new AbortController();
  const { collect } = harness(async (url, init) => {
    if (url.pathname === "/graphql") { entered.resolve(); await gate.promise; return response({ data: { nodes: graphIds(init).map(() => reviewNode(1)) } }); }
    return response(url.searchParams.get("state") === "open" ? [reviewPull(1)] : []);
  });
  const pending = collect([repository], { ...interval, signal: controller.signal });
  await entered.promise;
  controller.abort();
  const cancelled = await pending;
  assert.match(cancelled.error!, /cancelled/);
  assert.equal(cancelled.repositories[0].reviewComplete, false);
  assert.equal(cancelled.pullRequests[0].review?.decision, "unknown");
  gate.resolve();
  await new Promise(resolve => setTimeout(resolve, 0));
  assert.equal(cancelled.pullRequests[0].review?.decision, "unknown");
});
