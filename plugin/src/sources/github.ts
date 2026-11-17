import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { ConfiguredRepository, GitHubCollectionResult, PullRequestRecord, PullRequestReview } from "../shared";

const execFileAsync = promisify(execFile);
const REPOSITORY = /^[a-z0-9](?:[a-z0-9-]{0,38})\/[a-z0-9_.-]{1,100}$/;
const LOGIN = /^[a-z0-9](?:[a-z0-9-]{0,100})(?:\[bot\])?$/i;
const MAXIMUM_BYTES = 2 * 1024 * 1024;
const PAGE_SIZE = 50;
const REVIEW_QUERY = "query BigBoardReviews($ids:[ID!]!){nodes(ids:$ids){__typename ... on PullRequest{id number headRefOid repository{nameWithOwner} reviewDecision reviewRequests(first:50){totalCount pageInfo{hasNextPage}nodes{requestedReviewer{__typename ... on User{login} ... on Team{slug organization{login}}}}}assignees(first:50){totalCount pageInfo{hasNextPage}nodes{login}}}}}";

export interface GitHubCollectionOptions {
  since: string;
  until: string;
  signal?: AbortSignal;
  onProgress?: (progress: GitHubCollectionProgress) => void;
}

export interface GitHubCollectionProgress {
  completedRepositories: number;
  totalRepositories: number;
  requests: number;
}

export interface GitHubDependencies {
  token?: (signal: AbortSignal) => Promise<string>;
  request?: (url: URL, init: RequestInit) => Promise<Response>;
  maximumRequests?: number;
  maximumPages?: number;
  requestTimeoutMs?: number;
  timeoutMs?: number;
  repositoryConcurrency?: number;
}

type FailureKind = "auth" | "denied" | "rate" | "budget" | "timeout" | "cancelled" | "unavailable" | "response";

class GitHubFailure extends Error {
  constructor(readonly kind: FailureKind, message: string) {
    super(message);
  }
}

function failure(kind: FailureKind): GitHubFailure {
  const messages: Record<FailureKind, string> = {
    auth: "GitHub authentication is unavailable. Check the active GitHub CLI account with gh auth status.",
    denied: "GitHub denied access to this repository. Check repository and organization access for the active account.",
    rate: "GitHub rate-limited this collection. Retry after the account's rate limit resets.",
    budget: "The GitHub request budget was reached; PR coverage is incomplete.",
    timeout: "GitHub collection timed out; PR coverage is incomplete.",
    cancelled: "GitHub collection was cancelled; PR coverage is incomplete.",
    unavailable: "GitHub could not be reached; PR coverage is incomplete.",
    response: "GitHub returned unreadable PR evidence; coverage is incomplete.",
  };
  return new GitHubFailure(kind, messages[kind]);
}

async function tokenFromCLI(signal: AbortSignal): Promise<string> {
  try {
    const result = await execFileAsync("gh", ["auth", "token", "--hostname", "github.com"], {
      encoding: "utf8", timeout: 10_000, maxBuffer: 16_384, windowsHide: true, signal,
    });
    return result.stdout.trim();
  } catch {
    if (signal.aborted) throw failure("cancelled");
    throw failure("auth");
  }
}

function date(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : null;
}

function object(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function logins(value: unknown): { values: string[]; complete: boolean } {
  if (!Array.isArray(value)) return { values: [], complete: false };
  const values = value.slice(0, 50).flatMap(item => {
    const row = object(item);
    const login = row?.login;
    return (row?.type === undefined || row.type === "User") && typeof login === "string" && LOGIN.test(login) ? [login] : [];
  });
  return { values: [...new Set(values)], complete: value.length <= 50 && values.length === value.length && new Set(values.map(login => login.toLowerCase())).size === values.length };
}

function restReview(row: Record<string, unknown>, repository: ConfiguredRepository): PullRequestReview {
  const reviewers = logins(row.requested_reviewers);
  const assignees = logins(row.assignees);
  const teams = Array.isArray(row.requested_teams) ? row.requested_teams.slice(0, 50).flatMap(value => {
    const team = object(value);
    const organization = object(team?.organization)?.login ?? repository.id.split("/")[0];
    return typeof team?.slug === "string" && /^[a-z0-9][a-z0-9-]{0,99}$/i.test(team.slug) && typeof organization === "string" && LOGIN.test(organization) ? [`${organization}/${team.slug}`] : [];
  }) : [];
  return { decision: "unknown", requestedReviewers: reviewers.values, requestedTeams: [...new Set(teams)], assignees: assignees.values, reviewRequestsComplete: reviewers.complete && Array.isArray(row.requested_teams) && row.requested_teams.length <= 50 && teams.length === row.requested_teams.length && new Set(teams.map(team => team.toLowerCase())).size === teams.length, assigneesComplete: assignees.complete };
}

function graphReview(row: Record<string, unknown>): PullRequestReview {
  const connection = object(row.reviewRequests);
  const nodes = Array.isArray(connection?.nodes) ? connection.nodes : [];
  const reviewers: string[] = [];
  const teams: string[] = [];
  for (const value of nodes.slice(0, 50)) {
    const reviewer = object(object(value)?.requestedReviewer);
    if (reviewer?.__typename === "User" && typeof reviewer.login === "string" && LOGIN.test(reviewer.login)) reviewers.push(reviewer.login);
    if (reviewer?.__typename === "Team" && typeof reviewer.slug === "string" && /^[a-z0-9][a-z0-9-]{0,99}$/i.test(reviewer.slug)) {
      const organization = object(reviewer.organization)?.login;
      if (typeof organization === "string" && LOGIN.test(organization)) teams.push(`${organization}/${reviewer.slug}`);
    }
  }
  const assigneeConnection = object(row.assignees);
  const assignees = logins(assigneeConnection?.nodes);
  const count = reviewers.length + teams.length;
  const decisions: Record<string, PullRequestReview["decision"]> = { APPROVED: "approved", CHANGES_REQUESTED: "changes_requested", REVIEW_REQUIRED: "review_required" };
  return {
    decision: row.reviewDecision === null ? "none" : typeof row.reviewDecision === "string" && Object.hasOwn(decisions, row.reviewDecision) ? decisions[row.reviewDecision] : "unknown",
    requestedReviewers: [...new Set(reviewers)], requestedTeams: [...new Set(teams)], assignees: assignees.values,
    reviewRequestsComplete: Array.isArray(connection?.nodes) && nodes.length <= 50 && object(connection?.pageInfo)?.hasNextPage === false && connection?.totalCount === count && count === nodes.length && new Set([...reviewers, ...teams].map(value => value.toLowerCase())).size === count,
    assigneesComplete: assignees.complete && object(assigneeConnection?.pageInfo)?.hasNextPage === false && assigneeConnection?.totalCount === assignees.values.length,
  };
}

function record(value: unknown, repository: ConfiguredRepository, state: "open" | "merged"): PullRequestRecord | null {
  const row = object(value);
  const user = object(row?.user);
  const base = object(row?.base);
  const head = object(row?.head);
  const baseRepository = object(base?.repo);
  const createdAt = date(row?.created_at);
  const updatedAt = date(row?.updated_at);
  const mergedAt = date(row?.merged_at);
  if (!row || !user || !base || !head || !Number.isSafeInteger(row.number) || (row.number as number) <= 0 ||
      typeof row.title !== "string" || row.title.length > 10_000 || typeof user.login !== "string" || !LOGIN.test(user.login) ||
      typeof base.ref !== "string" || typeof head.sha !== "string" || !/^[a-f0-9]{40,64}$/i.test(head.sha) ||
      !createdAt || !updatedAt || updatedAt < createdAt ||
      (baseRepository?.full_name !== undefined && (typeof baseRepository.full_name !== "string" || baseRepository.full_name.toLowerCase() !== repository.id)) ||
      (state === "open" ? row.state !== "open" || row.merged_at !== null : !mergedAt || mergedAt < createdAt || mergedAt > updatedAt)) {
    return null;
  }
  return {
    id: `${repository.id}#${row.number}`,
    repository: repository.id,
    number: row.number as number,
    title: row.title,
    url: `https://github.com/${repository.id}/pull/${row.number}`,
    author: user.login,
    state,
    draft: row.draft === true,
    createdAt,
    updatedAt,
    mergedAt: state === "merged" ? mergedAt : null,
    baseBranch: base.ref,
    headSha: head.sha,
    review: restReview(row, repository),
  };
}

async function abortable<T>(operation: Promise<T>, signal: AbortSignal): Promise<T> {
  signal.throwIfAborted();
  return new Promise((resolve, reject) => {
    const abort = () => reject(signal.reason);
    signal.addEventListener("abort", abort, { once: true });
    operation.then(resolve, reject).finally(() => signal.removeEventListener("abort", abort));
  });
}

async function json(response: Response, signal: AbortSignal): Promise<unknown> {
  const declaredLength = Number(response.headers.get("content-length"));
  if (declaredLength > MAXIMUM_BYTES) {
    await response.body?.cancel();
    throw failure("response");
  }
  if (!response.body) throw failure("response");
  const reader = response.body.getReader();
  let text = "";
  let bytes = 0;
  const decoder = new TextDecoder();
  try {
    while (true) {
      const part = await abortable(reader.read(), signal);
      if (part.done) break;
      bytes += part.value.byteLength;
      if (bytes > MAXIMUM_BYTES) throw failure("response");
      text += decoder.decode(part.value, { stream: true });
    }
    text += decoder.decode();
    return JSON.parse(text);
  } catch (error) {
    void reader.cancel().catch(() => {});
    throw error instanceof SyntaxError ? failure("response") : error;
  } finally {
    reader.releaseLock();
  }
}

export function createGitHubCollector(dependencies: GitHubDependencies = {}) {
  const getToken = dependencies.token ?? tokenFromCLI;
  const transport = dependencies.request ?? ((url: URL, init: RequestInit) => fetch(url, init));
  const maximumRequests = dependencies.maximumRequests ?? 128;
  const maximumPages = dependencies.maximumPages ?? 8;
  const requestTimeoutMs = dependencies.requestTimeoutMs ?? 15_000;
  const timeoutMs = dependencies.timeoutMs ?? 120_000;
  const requestedConcurrency = dependencies.repositoryConcurrency ?? 3;
  const repositoryConcurrency = Number.isFinite(requestedConcurrency) ? Math.max(1, Math.min(3, Math.floor(requestedConcurrency))) : 3;

  return async function collect(repositories: ConfiguredRepository[], options: GitHubCollectionOptions): Promise<GitHubCollectionResult> {
    const result: GitHubCollectionResult = {
      account: null,
      repositories: repositories.map(({ id }) => ({ id, openComplete: false, mergedComplete: false, accessDenied: false, error: null })),
      pullRequests: [],
      error: null,
    };
    const finish = () => {
      for (const coverage of result.repositories) {
        coverage.reviewComplete = coverage.openComplete && coverage.mergedComplete && result.pullRequests.filter(record => record.repository === coverage.id).every(record => record.review && record.review.decision !== "unknown" && record.review.reviewRequestsComplete && record.review.assigneesComplete);
      }
      return result;
    };
    let requests = 0;
    let completedRepositories = 0;
    const report = () => {
      try { options.onProgress?.({ completedRepositories, totalRepositories: repositories.length, requests }); }
      catch {}
    };
    report();
    const since = date(options.since);
    const until = date(options.until);
    if (!since || !until || since >= until || repositories.some((repository) => !REPOSITORY.test(repository.id) ||
        [".", ".."].includes(repository.id.split("/")[1]))) {
      const message = "GitHub collection requires canonical repository IDs and a valid date interval.";
      result.error = message;
      result.repositories.forEach((repository) => { repository.error = message; });
      return finish();
    }
    if (repositories.length === 0) return finish();
    const deadline = AbortSignal.timeout(Math.max(1, timeoutMs));
    const collectionSignal = AbortSignal.any([deadline, ...(options.signal ? [options.signal] : [])]);
    let terminal: GitHubFailure | null = null;
    const authenticationAbort = new AbortController();
    let credential: string;
    const abortFailure = () => failure(options.signal?.aborted ? "cancelled" : "timeout");
    const halt = (issue: GitHubFailure): GitHubFailure => {
      if (!terminal || issue.kind === "auth") terminal = issue;
      if (issue.kind === "auth") authenticationAbort.abort();
      return terminal;
    };
    const request = async (endpoint: string, query?: Record<string, string>, graph?: { query: string; variables: { ids: string[] } }): Promise<{ value: unknown; next: boolean }> => {
      if (terminal) throw terminal;
      if (collectionSignal.aborted) throw halt(abortFailure());
      if (requests >= maximumRequests) throw halt(failure("budget"));
      const url = new URL(`https://api.github.com/${endpoint}`);
      for (const [key, value] of Object.entries(query ?? {})) url.searchParams.set(key, value);
      if (url.origin !== "https://api.github.com" || url.username || url.password || url.hash) throw failure("response");
      const signal = AbortSignal.any([collectionSignal, authenticationAbort.signal, AbortSignal.timeout(Math.max(1, requestTimeoutMs))]);
      requests += 1;
      report();
      try {
        signal.throwIfAborted();
        const response = await abortable(transport(url, {
          method: graph ? "POST" : "GET",
          headers: {
            Authorization: `Bearer ${credential}`,
            Accept: "application/vnd.github+json",
            "X-GitHub-Api-Version": "2026-03-10",
            "User-Agent": "BigBoard-MCP",
            ...(graph ? { "Content-Type": "application/json" } : {}),
          },
          ...(graph ? { body: JSON.stringify(graph) } : {}),
          redirect: "error",
          signal,
        }), signal);
        if (response.status === 401) throw halt(failure("auth"));
        if (response.status === 429 || (response.status === 403 &&
            (response.headers.get("x-ratelimit-remaining") === "0" || response.headers.has("retry-after")))) {
          throw halt(failure("rate"));
        }
        if (response.status === 403 || response.status === 404) throw failure("denied");
        if (!response.ok) throw failure(response.status >= 500 ? "unavailable" : "response");
        return { value: await json(response, signal), next: /<[^>]+>;\s*rel="next"/.test(response.headers.get("link") ?? "") };
      } catch (error) {
        if (error instanceof GitHubFailure) throw error;
        if ((terminal as GitHubFailure | null)?.kind === "auth") throw terminal;
        if (signal.aborted) throw halt(collectionSignal.aborted ? abortFailure() : failure("timeout"));
        throw failure("unavailable");
      }
    };

    try {
      if (collectionSignal.aborted) throw abortFailure();
      credential = await abortable(getToken(collectionSignal), collectionSignal);
      if (!credential || credential.length > 8192 || !/^[\x21-\x7e]+$/.test(credential)) throw failure("auth");
      const viewer = object((await request("user")).value);
      if (!viewer || !Number.isSafeInteger(viewer.id) || (viewer.id as number) < 1 || typeof viewer.login !== "string" || !LOGIN.test(viewer.login)) {
        throw failure("auth");
      }
      result.account = { id: String(viewer.id), login: viewer.login };
    } catch (error) {
      const issue = error instanceof GitHubFailure ? error : collectionSignal.aborted ? abortFailure() : failure("auth");
      result.error = issue.message;
      result.repositories.forEach((repository) => { repository.error = issue.message; });
      return finish();
    }

    const nodeIds = new Map<string, string>();
    const inventory = async (repository: ConfiguredRepository, state: "open" | "merged") => {
      const records: PullRequestRecord[] = [];
      const seen = new Set<string>();
      let malformed = false;
      for (let page = 1; page <= maximumPages; page += 1) {
        try {
          const response = await request(`repos/${repository.id}/pulls`, {
            state: state === "open" ? "open" : "closed", sort: "updated", direction: "desc", per_page: String(PAGE_SIZE), page: String(page),
          });
          if (!Array.isArray(response.value) || response.value.length > PAGE_SIZE) throw failure("response");
          const rows = response.value;
          for (const value of rows) {
            const row = object(value);
            if (state === "merged" && row?.merged_at === null) continue;
            const normalized = record(value, repository, state);
            if (!normalized) {
              malformed = true;
              continue;
            }
            if (seen.has(normalized.id)) {
              malformed = true;
              continue;
            }
            seen.add(normalized.id);
            if (state === "merged" && (normalized.baseBranch !== repository.defaultBranch || normalized.mergedAt! < since || normalized.mergedAt! >= until)) continue;
            records.push(normalized);
            if (typeof row?.node_id === "string" && row.node_id.length > 0 && row.node_id.length <= 512 && !row.node_id.includes("\0")) nodeIds.set(normalized.id, row.node_id);
          }
          const lastUpdated = rows.length > 0 ? date(object(rows.at(-1))?.updated_at) : null;
          const validUpdatedDates = rows.every((row) => date(object(row)?.updated_at) !== null);
          const reachedBoundary = state === "merged" && validUpdatedDates && lastUpdated !== null && lastUpdated < since;
          if (!response.next || reachedBoundary) {
            return { records, complete: !malformed, error: malformed ? failure("response").message : null };
          }
        } catch (error) {
          const issue = error instanceof GitHubFailure ? error : failure("unavailable");
          if (issue.kind === "auth" || issue.kind === "denied") throw issue;
          return { records, complete: false, error: issue.message };
        }
      }
      return { records, complete: false, error: "The GitHub page limit was reached; PR coverage is incomplete." };
    };

    const recordsByRepository: PullRequestRecord[][] = repositories.map(() => []);
    const completed = repositories.map(() => false);
    const completeRepository = (index: number) => {
      if (completed[index]) return;
      completed[index] = true;
      completedRepositories += 1;
      report();
    };
    let nextRepository = 0;
    const worker = async () => {
      while (nextRepository < repositories.length && !terminal && !collectionSignal.aborted) {
        const index = nextRepository++;
        const repository = repositories[index];
        const coverage = result.repositories[index];
        if (typeof repository.defaultBranch !== "string" || !repository.defaultBranch.trim()) {
          coverage.error = "The repository's default branch is unavailable; PR collection was skipped.";
          completeRepository(index);
          continue;
        }
        try {
          const open = await inventory(repository, "open");
          const merged = await inventory(repository, "merged");
          coverage.openComplete = open.complete;
          coverage.mergedComplete = merged.complete;
          coverage.error = [...new Set([open.error, merged.error].filter((value): value is string => value !== null))].join(" ") || null;
          recordsByRepository[index] = [...open.records, ...merged.records];
        } catch (error) {
          const issue = error instanceof GitHubFailure ? error : failure("unavailable");
          if (issue.kind === "auth") halt(issue);
          coverage.accessDenied = issue.kind === "denied";
          coverage.error = issue.message;
        } finally {
          if (!recordsByRepository[index].some(record => nodeIds.has(record.id))) completeRepository(index);
        }
      }
    };
    await Promise.all(Array.from({ length: Math.min(repositoryConcurrency, repositories.length) }, worker));
    const metadataError = (repository: string) => {
      const coverage = result.repositories.find(value => value.id === repository)!;
      const message = "Review metadata is incomplete for some pull requests.";
      coverage.error = [...new Set([coverage.error, message].filter((value): value is string => Boolean(value)))].join(" ");
    };
    const denyRepository = (repository: string) => {
      const index = repositories.findIndex(value => value.id === repository);
      recordsByRepository[index] = [];
      const coverage = result.repositories[index];
      coverage.accessDenied = true;
      coverage.openComplete = false;
      coverage.mergedComplete = false;
      coverage.error = failure("denied").message;
    };
    const candidates = recordsByRepository.flat().filter(record => nodeIds.has(record.id));
    const batches = Array.from({ length: Math.ceil(candidates.length / PAGE_SIZE) }, (_, index) => candidates.slice(index * PAGE_SIZE, (index + 1) * PAGE_SIZE));
    const remaining = new Map<string, number>();
    for (const record of candidates) remaining.set(record.repository, (remaining.get(record.repository) ?? 0) + 1);
    const completeBatch = (batch: PullRequestRecord[]) => {
      for (const record of batch) {
        const count = remaining.get(record.repository)! - 1;
        remaining.set(record.repository, count);
        if (count === 0) completeRepository(repositories.findIndex(repository => repository.id === record.repository));
      }
    };
    let nextBatch = 0;
    const enrich = async () => {
      while (nextBatch < batches.length && !terminal && !collectionSignal.aborted) {
        const scheduledBatch = batches[nextBatch++];
        const batch = scheduledBatch.filter(record => !result.repositories.find(value => value.id === record.repository)!.accessDenied);
        try {
          if (batch.length === 0) continue;
          const response = object((await request("graphql", undefined, { query: REVIEW_QUERY, variables: { ids: batch.map(record => nodeIds.get(record.id)!) } })).value);
          const errors = response?.errors;
          if (errors !== undefined && !Array.isArray(errors)) throw failure("response");
          const affected = new Set<number>();
          for (const value of errors as unknown[] | undefined ?? []) {
            const error = object(value);
            const type = typeof error?.type === "string" ? error.type.toUpperCase() : String(object(error?.extensions)?.code ?? "").toUpperCase();
            if (type === "UNAUTHORIZED" || type === "UNAUTHENTICATED") throw halt(failure("auth"));
            if (type === "RATE_LIMITED") throw halt(failure("rate"));
            const path = Array.isArray(error?.path) ? error.path : [];
            const indices = path[0] === "nodes" && Number.isSafeInteger(path[1]) && Number(path[1]) >= 0 && Number(path[1]) < batch.length ? [Number(path[1])] : batch.map((_, index) => index);
            for (const index of indices) {
              affected.add(index);
              if ((type === "FORBIDDEN" || type === "NOT_FOUND") && path.length <= 2) denyRepository(batch[index].repository);
              else metadataError(batch[index].repository);
            }
          }
          const nodes = object(response?.data)?.nodes;
          if (!Array.isArray(nodes) || nodes.length !== batch.length) throw failure("response");
          for (let index = 0; index < batch.length; index += 1) {
            const record = batch[index];
            if (affected.has(index) || result.repositories.find(value => value.id === record.repository)!.accessDenied) continue;
            const row = object(nodes[index]);
            if (!row) { denyRepository(record.repository); continue; }
            const repository = object(row.repository)?.nameWithOwner;
            if (row.__typename !== "PullRequest" || row.id !== nodeIds.get(record.id) || row.number !== record.number || row.headRefOid !== record.headSha || typeof repository !== "string" || repository.toLowerCase() !== record.repository) {
              record.review = { decision: "unknown", requestedReviewers: [], requestedTeams: [], assignees: [], reviewRequestsComplete: false, assigneesComplete: false };
              metadataError(record.repository);
              continue;
            }
            record.review = graphReview(row);
            if (record.review.decision === "unknown" || !record.review.reviewRequestsComplete || !record.review.assigneesComplete) metadataError(record.repository);
          }
        } catch (error) {
          const issue = error instanceof GitHubFailure ? error : failure("unavailable");
          if (issue.kind === "auth") halt(issue);
          for (const record of batch) {
            if (issue.kind === "denied") denyRepository(record.repository);
            else metadataError(record.repository);
          }
        } finally { completeBatch(scheduledBatch); }
      }
    };
    await Promise.all(Array.from({ length: Math.min(repositoryConcurrency, batches.length) }, enrich));
    for (; nextBatch < batches.length; nextBatch += 1) {
      for (const record of batches[nextBatch]) metadataError(record.repository);
      completeBatch(batches[nextBatch]);
    }
    if (collectionSignal.aborted && !terminal) halt(abortFailure());
    if (terminal) {
      const issue = terminal as GitHubFailure;
      result.error = issue.message;
      for (; nextRepository < repositories.length; nextRepository += 1) {
        result.repositories[nextRepository].error = issue.message;
        completeRepository(nextRepository);
      }
      if (issue.kind === "auth") {
        result.account = null;
        result.repositories.forEach((item) => {
          item.openComplete = false;
          item.mergedComplete = false;
          item.error = issue.message;
        });
        return finish();
      }
    }
    result.pullRequests = [...new Map(recordsByRepository.flat().map((row) => [row.id, row])).values()];
    return finish();
  };
}

export const collectGitHub = createGitHubCollector();
