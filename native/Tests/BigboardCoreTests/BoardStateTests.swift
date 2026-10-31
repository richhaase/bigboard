import Foundation
import Testing
@testable import Bigboard

@MainActor struct BoardStateTests {
    let now = Date(timeIntervalSince1970: 2_000_000_000)
    func commit(_ id: String, repo: String = "acme/app", email: String = "alex@example.test", days: Int = 1, added: Int = 10) -> Commit { Commit(id: id, repo: repo, author: email, email: email, date: now.addingTimeInterval(-Double(days) * 86400), subject: "Change \(id)", added: added, files: [FileChange(path: "Sources/App.swift", added: added)]) }
    @Test func contextualPRNavigationRestoresIndependentState() {
        let state = BoardState()
        state.days = 30; state.repositories = ["acme/app"]
        state.activity.query = "source"; state.contributors.query = "alex"
        state.prs.query = "release"; state.prs.authors = ["alex"]
        state.scope = ActivityScope(repo: "acme/app", path: "Sources")
        state.lens = .people
        state.showPRs(repo: "acme/app", selected: "acme/app#7")
        #expect(state.board == .pullRequests)
        #expect(state.prs.query.isEmpty)
        #expect(state.contextualRepository == "acme/app")
        state.prs.checks = ["FAILURE"]
        state.returnToActivity()
        #expect(state.board == .activity)
        #expect(state.scope.path == "Sources")
        #expect(state.lens == .people)
        #expect(state.activity.query == "source")
        #expect(state.contributors.query == "alex")
        #expect(state.prs.query == "release")
        #expect(state.prs.authors == ["alex"])
        #expect(state.prs.checks.isEmpty)
        #expect(state.days == 30)
        #expect(state.repositories == ["acme/app"])
        state.showPRs(repo: "acme/app")
        state.switchBoard(.contributors)
        #expect(state.contextualRepository == nil)
        #expect(state.prs.query == "release")
    }
    @Test func heatKeepsHistoryWhileHonoringSharedRepositoriesAndPeople() {
        let snapshot = Snapshot(generatedAt: now, commits: [commit("recent"), commit("old", days: 100), commit("other", repo: "acme/other"), commit("otherperson", email: "sam@example.test")], bookmark: now.addingTimeInterval(-2 * 86400))
        let state = BoardState(); state.repositories = ["acme/app"]; state.activity.people = ["alex@example.test"]; state.sinceLastLook = true
        #expect(state.filteredCommits(snapshot).map(\.id) == ["recent"])
        #expect(state.filteredCommits(snapshot, heat: true).map(\.id) == ["recent", "old"])
    }
    @Test func fileDrillReturnsToParentAndAreaBoundaryRemains() {
        let state = BoardState()
        state.explore(ActivityScope(repo: "acme/app", path: "Sources/UI/View.swift", isFile: true, areaName: "Interface"))
        #expect(state.lens == .commits)
        state.back()
        #expect(state.scope.path == "Sources/UI")
        #expect(!state.scope.isFile)
        #expect(state.scope.areaName == "Interface")
        #expect(state.lens == .directories)
        state.back()
        #expect(state.scope.path == "Sources")
        #expect(state.scope.areaName == "Interface")
    }
    @Test func relatedExplorationRetainsEvidenceUntilChangingLens() {
        let state = BoardState()
        let matching = commit("shared", repo: "acme/other")
        let unrelated = commit("unrelated", repo: "acme/other")
        state.exploreRelated(RelatedRow(scope: ActivityScope(repo: "acme/other"), evidence: ["shared"], records: [matching]))
        #expect(state.lens == .commits)
        #expect(state.scope.repo == "acme/other")
        #expect(state.evidenceRecords([matching, unrelated], canonical: [matching, unrelated]).map(\.id) == ["shared"])
        state.setLens(.directories)
        #expect(state.relatedEvidence == nil)
        #expect(state.evidenceRecords([matching, unrelated], canonical: [matching, unrelated]).count == 2)
    }
    @Test func reverseSortIsStableForEqualMetrics() {
        let snapshot = Snapshot(generatedAt: now, repositories: [Repository(id: "acme/app")], commits: [commit("a", email: "a@example.test"), commit("b", email: "b@example.test"), commit("c", email: "c@example.test")])
        let state = BoardState(); let forward = state.peopleRows(snapshot).map(\.id)
        state.contributors.reversed = true
        #expect(state.peopleRows(snapshot).map(\.id) == Array(forward.reversed()))
        #expect(Set(forward).count == 3)
    }
    @Test func markdownExportsAllMatchingRowsBeyondVisualLimit() {
        let commits = (0..<150).map { commit(String($0)) }
        let snapshot = Snapshot(generatedAt: now, repositories: [Repository(id: "acme/app")], commits: commits)
        let state = BoardState(); state.lens = .commits
        let markdown = BoardMarkdown.render(snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state)
        #expect(markdown.components(separatedBy: "https://github.com/acme/app/commit/").count == 151)
        state.activity.query = "Change 149"
        let narrowed = BoardMarkdown.render(snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state)
        #expect(narrowed.components(separatedBy: "https://github.com/acme/app/commit/").count == 2)
        #expect(narrowed.contains("Change 149"))
    }
    @Test func accountAndAccessChangesClearUnavailableContext() {
        let state = BoardState(); state.scope = ActivityScope(repo: "acme/private"); state.repositories = ["acme/private"]; state.showPRs(repo: "acme/private")
        state.reconcile(Snapshot(repositories: [Repository(id: "acme/public")]))
        #expect(state.scope.repo.isEmpty)
        #expect(state.repositories.isEmpty)
        #expect(state.contextualRepository == nil)
        state.sinceLastLook = true; state.contributors.people = ["old@example.test"]
        var configuration = WorkspaceConfiguration(); configuration.sinceDays = 90; configuration.contributorSort = "commits"
        state.resetForAccount(configuration)
        #expect(state.days == 90)
        #expect(!state.sinceLastLook)
        #expect(state.contributors.people.isEmpty)
        #expect(state.contributors.sort == "commits")
    }
}
