import type { PullRequestRecord } from "../shared";

export function reviewStatus(pr: PullRequestRecord): { label: string; tone: string } {
  switch (pr.review?.decision) {
    case "approved": return { label: "Approved", tone: "approved" };
    case "changes_requested": return { label: "Changes requested", tone: "changes-requested" };
    case "review_required": return { label: "Review required", tone: "review-required" };
    case "none": return { label: "No decision", tone: "neutral" };
    default: return { label: "Unknown", tone: "unknown" };
  }
}

export function reviewNeed(pr: PullRequestRecord): { label: string; title: string; needed: boolean | null } {
  if (pr.state === "merged") return { label: "—", title: "This pull request is merged.", needed: false };
  if (pr.draft) return { label: "Draft", title: "This pull request has not been marked ready for review.", needed: false };
  const review = pr.review;
  if (review?.decision === "review_required" || review?.requestedReviewers.length || review?.requestedTeams.length) {
    return { label: "Yes", title: "GitHub requires a review or has an outstanding review request.", needed: true };
  }
  if (!review || review.decision === "unknown" || !review.reviewRequestsComplete) {
    return { label: "Unknown", title: "Review metadata is unavailable or incomplete. Refresh sources to check it.", needed: null };
  }
  if (review.decision === "changes_requested") return { label: "Author action", title: "Changes have been requested and no outstanding review request is recorded.", needed: false };
  if (review.decision === "approved") return { label: "No", title: "GitHub reports approval and no outstanding review request.", needed: false };
  return { label: "No request", title: "GitHub reports no review decision or outstanding review request.", needed: false };
}

export function viewerRelationships(pr: PullRequestRecord, login: string | null | undefined): string[] {
  if (!login) return [];
  const isViewer = (value: string) => value.toLowerCase() === login.toLowerCase();
  return [
    ...(isViewer(pr.author) ? ["You authored"] : []),
    ...(pr.review?.assignees.some(isViewer) ? ["Assigned to you"] : []),
    ...(pr.review?.requestedReviewers.some(isViewer) ? ["Your review requested"] : []),
  ];
}

export function viewerRelationshipsUnknown(pr: PullRequestRecord, login: string | null | undefined): boolean {
  return !login || !pr.review || !pr.review.assigneesComplete || !pr.review.reviewRequestsComplete;
}
