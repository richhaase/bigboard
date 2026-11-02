import Foundation
import Testing
@testable import Bigboard

struct ExportTests {
    func commit(_ id: String, repo: String = "a/repo", author: String = "A", email: String = "a@test", added: Int = 3, unknown: Bool = false) -> Commit {
        Commit(id: id, repo: repo, author: author, email: email, date: Date(timeIntervalSince1970: 1700000000), subject: id, added: added, removed: 1, linesUnknown: unknown)
    }
    @Test func perRepoUsesCanonicalMetricsAndAttributionForSharedCommit() throws {
        let rows = ActivityExport.authors([commit("same", repo: "z/repo", author: "Other", email: "other@test", added: 999), commit("same", added: 3), commit("second", repo: "z/repo", added: 7)])
        #expect(rows.count == 1)
        #expect(rows[0].commits == 2 && rows[0].added == 10)
        #expect(rows[0].perRepo["a/repo"]?.added == 3)
        #expect(rows[0].perRepo["z/repo"]?.added == 10)
        #expect(rows[0].perRepo["z/repo"]?.commits == 2)
    }
    @Test func canonicalNameUsesFrequencyThenUnicodeLength() {
        let rows = ActivityExport.authors([commit("1", author: "Al"), commit("2", author: "Alice"), commit("3", author: "Al"), commit("4", author: "Alice")])
        #expect(rows[0].name == "Alice")
        #expect(rows[0].activeDays == 1)
    }
    @Test func unknownCountsAreOmittedOnlyWhenZero() throws {
        let data = try ActivityExport.json([commit("one"), commit("two", email: "other@test", unknown: true)])
        let rows = try #require(JSONSerialization.jsonObject(with: data) as? [[String: Any]])
        #expect(rows[0]["unknown_line_commits"] == nil)
        #expect(rows[1]["unknown_line_commits"] as? Int == 1)
        #expect(rows[1]["added"] as? Int == 0)
        #expect(rows[0]["identity"] == nil)
        let perRepo = try #require(rows[1]["per_repo"] as? [String: [String: Any]])
        #expect(perRepo["a/repo"]?["unknown_line_commits"] as? Int == 1)
        #expect(data.last == 10)
    }
    @Test func configuredSortAndAIFractionsAreRespected() {
        var a = commit("one", added: 100); a.ai = true
        var b = commit("two", author: "B", email: "b@test", added: 2); b.ai = true
        let b2 = commit("three", author: "B", email: "b@test", added: 2)
        var config = WorkspaceConfiguration(); config.contributorSort = "commits"
        #expect(ActivityExport.authors([a,b,b2], configuration: config).map(\.name) == ["B", "A"])
        config.contributorSort = "ai"
        #expect(ActivityExport.authors([a,b,b2], configuration: config).map(\.name) == ["A", "B"])
    }
    @Test func emptyIDsStayIndependentAcrossContributors() {
        let rows = ActivityExport.authors([commit("", author: "A"), commit("", repo: "z/repo", author: "B", email: "b@test")])
        #expect(rows.count == 2)
        #expect(rows.allSatisfy { $0.perRepo.count == 1 })
    }
}
