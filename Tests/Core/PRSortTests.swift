import Foundation
import Testing
@testable import Bigboard

struct PRSortTests {
    let now = Date(timeIntervalSince1970: 2_000_000_000)
    func pr(_ number: Int, review: String = "UNKNOWN", checks: String = "UNKNOWN", size: (Int, Int)? = nil, createdDays: Double? = nil) -> PullRequest {
        var row = PullRequest(repo: "acme/app", number: number, title: "PR \(number)", author: "alex", url: "", updatedAt: now.addingTimeInterval(-Double(number) * 60))
        row.review = review
        row.checks = checks
        row.additions = size?.0
        row.deletions = size?.1
        row.createdAt = createdDays.map { now.addingTimeInterval(-$0 * 86400) }
        return row
    }
    func order(_ prs: [PullRequest], _ sort: String, reverse: Bool = false) -> [Int] {
        Analysis.filterPRs(prs, viewer: "", filters: PRFilters(sort: sort, reverse: reverse), now: now).map(\.number)
    }
    @Test func sortsBySizeWithUnknownLast() {
        let prs = [pr(1, size: (5, 5)), pr(2), pr(3, size: (100, 1))]
        #expect(order(prs, "size") == [3, 1, 2])
        #expect(order(prs, "size", reverse: true) == [2, 1, 3])
    }
    @Test func sortsByCreatedNewestFirst() {
        let prs = [pr(1, createdDays: 10), pr(2, createdDays: 1), pr(3)]
        #expect(order(prs, "created") == [2, 1, 3])
    }
    @Test func sortsReviewAndChecksWorstFirstThenRecent() {
        let prs = [
            pr(1, review: "APPROVED", checks: "SUCCESS"), pr(2, review: "CHANGES_REQUESTED", checks: "PENDING"),
            pr(3, review: "REVIEW_REQUIRED", checks: "FAILURE"), pr(4, review: "APPROVED", checks: "FAILURE"),
        ]
        #expect(order(prs, "review") == [2, 3, 1, 4])
        #expect(order(prs, "checks") == [3, 4, 2, 1])
    }
}
