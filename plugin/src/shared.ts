import { version } from "../package.json";

export const APP_VERSION = version;

export const REPOSITORIES = ["all", "sample/api", "sample/web"] as const;
export type RepositoryFilter = (typeof REPOSITORIES)[number];

export interface BoardQuery {
  days: 7 | 14;
  repository: RepositoryFilter;
  compare: boolean;
}

export interface ActivityRecord {
  id: string;
  repository: Exclude<RepositoryFilter, "all">;
  timestamp: string;
  subject: string;
  contributor: string;
  additions: number;
  deletions: number;
}

export interface ActivityBucket {
  start: string;
  end: string;
  commits: number;
  additions: number;
  deletions: number;
  previousCommits: number | null;
  recordIds: string[];
}

export interface BoardSnapshot {
  boardId: string;
  revision: number;
  snapshotId: string;
  updatedAt: string;
  query: BoardQuery;
  scope: { since: string; until: string; timezone: string };
  coverage: { source: "fixture"; complete: boolean; note: string };
  buckets: ActivityBucket[];
  totals: { commits: number; additions: number; deletions: number; previousCommits: number | null };
}

export interface SelectionContext {
  boardId: string;
  revision: number;
  snapshotId: string;
  query: BoardQuery;
  scope: BoardSnapshot["scope"];
  bucketStart: string | null;
  recordIds: string[];
}

export const BOARD_RESOURCE_URI = "ui://bigboard/activity.html";
