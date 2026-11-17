import { version } from "../package.json";

export const APP_VERSION = version;
export type RepositoryFilter = string;
export type BoardViewMode = "activity" | "pullRequests" | "people";
export type PeopleSource = "git" | "github";
export type PullRequestSort = "updated" | "created" | "number" | "title" | "author" | "repository";
export type SortDirection = "asc" | "desc";

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
  initialView?: BoardViewMode;
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
  personId?: string;
  pullRequestSort?: PullRequestSort;
  sortDirection?: SortDirection;
}

export interface PersonSummary {
  id: string;
  source: PeopleSource;
  name: string;
  identity: string;
  commits: number;
  additions: number;
  deletions: number;
  unknownLineCommits: number;
  openPRs: number;
  mergedPRs: number;
  repositories: string[];
}

export interface PeopleResult {
  boardId: string;
  revision: number;
  snapshotId: string;
  query: BoardQuery;
  scope: BoardSnapshot["scope"];
  coverage: BoardSnapshot["coverage"];
  source: PeopleSource;
  people: PersonSummary[];
  totalPeople: number;
  nextCursor: string | null;
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
  selectedPersonId?: string;
  pullRequestSort?: PullRequestSort;
  sortDirection?: SortDirection;
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
  view?: BoardViewMode;
  pullRequestState?: "open" | "merged";
  pullRequestId?: string | null;
  personId?: string | null;
  peopleSource?: PeopleSource;
  selectedPullRequests?: PullRequestRecord[];
  pullRequestSort?: PullRequestSort;
  sortDirection?: SortDirection;
}

export const BOARD_RESOURCE_URI = "ui://bigboard/activity.html";
