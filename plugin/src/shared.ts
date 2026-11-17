import { version } from "../package.json";

export const APP_VERSION = version;
export type RepositoryFilter = string;

export interface BoardQuery {
  days: 7 | 14;
  repository: RepositoryFilter;
  compare: boolean;
}

export interface ConfiguredRepository {
  id: string;
  path: string;
  remote: string;
  defaultBranch: string;
}

export interface SourceConfiguration {
  revision: number;
  repositories: ConfiguredRepository[];
}

export interface FileChange {
  path: string;
  additions: number | null;
  deletions: number | null;
}

export interface ActivityRecord {
  id: string;
  repository: string;
  sha: string;
  timestamp: string;
  subject: string;
  contributor: string;
  email: string;
  additions: number | null;
  deletions: number | null;
  files: FileChange[];
  filesTruncated?: boolean;
  url: string;
}

export interface PullRequestRecord {
  id: string;
  repository: string;
  number: number;
  title: string;
  url: string;
  author: string;
  state: "open" | "merged";
  draft: boolean;
  createdAt: string;
  updatedAt: string;
  mergedAt: string | null;
  baseBranch: string;
  headSha: string;
}

export interface GitCollectionResult {
  repository: ConfiguredRepository;
  revision: string | null;
  ref: string;
  shallow: boolean;
  partial: boolean;
  complete: boolean;
  linesComplete: boolean;
  records: ActivityRecord[];
  error: string | null;
}

export interface GitHubCollectionResult {
  account: { id: string; login: string } | null;
  repositories: Array<{ id: string; openComplete: boolean; mergedComplete: boolean; accessDenied: boolean; error: string | null }>;
  pullRequests: PullRequestRecord[];
  error: string | null;
}

export interface CollectedWorkspace {
  id: string;
  configRevision: number;
  generatedAt: string;
  since: string;
  until: string;
  git: GitCollectionResult[];
  github: GitHubCollectionResult;
}

export interface RepositoryCoverage {
  repository: string;
  revision: string | null;
  ref: string;
  shallow: boolean;
  partial: boolean;
  historyComplete: boolean;
  linesComplete: boolean;
  openPRsComplete: boolean;
  mergedPRsComplete: boolean;
  errors: string[];
}

export interface ActivityBucket {
  start: string;
  end: string;
  commits: number;
  additions: number;
  deletions: number;
  unknownLineCommits: number;
  previousCommits: number | null;
  mergedPRs: number;
  recordIds: string[];
  recordIdsTruncated?: boolean;
}

export interface BoardSnapshot {
  boardId: string;
  revision: number;
  snapshotId: string;
  updatedAt: string;
  query: BoardQuery;
  scope: { since: string; until: string; timezone: string };
  coverage: { source: "git+github"; complete: boolean; note: string; repositories: RepositoryCoverage[] };
  configuration: { revision: number; repositories: string[]; needsRefresh: boolean };
  account: { id: string; login: string } | null;
  collectedAt: string | null;
  buckets: ActivityBucket[];
  totals: { commits: number; additions: number; deletions: number; unknownLineCommits: number; previousCommits: number | null; mergedPRs: number; openPRs: number };
}

export interface EvidenceFilter {
  kind?: "all" | "activity" | "pull_requests";
  pullRequestState?: "open" | "merged";
  pullRequestId?: string;
}

export interface EvidenceResult {
  boardId: string;
  revision: number;
  snapshotId: string;
  query: BoardQuery;
  scope: BoardSnapshot["scope"];
  coverage: BoardSnapshot["coverage"];
  bucketStart: string | null;
  records: ActivityRecord[];
  pullRequests: PullRequestRecord[];
  nextCursor: string | null;
  totalRecords: number;
  totalPullRequests: number;
  kind?: "all" | "activity" | "pull_requests";
  pullRequestState?: "open" | "merged";
  selectedPullRequestId?: string;
}

export interface SelectionContext {
  boardId: string;
  revision: number;
  snapshotId: string;
  query: BoardQuery;
  scope: BoardSnapshot["scope"];
  bucketStart: string | null;
  recordIds: string[];
  recordIdsTruncated?: boolean;
  view?: "activity" | "pullRequests";
  pullRequestState?: "open" | "merged";
  pullRequestId?: string | null;
}

export const BOARD_RESOURCE_URI = "ui://bigboard/activity.html";
