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
    @Test func selectedRepositoryModeStartsWithOneRelevantRepository() {
        let repos = [Repository(id: "acme/service"), Repository(id: "acme/app"), Repository(id: "acme/library")]
        #expect(BoardState.startingRepositorySelection(from: repos, preferred: "acme/library") == ["acme/library"])
        #expect(BoardState.startingRepositorySelection(from: repos) == ["acme/app"])
        #expect(BoardState.startingRepositorySelection(from: repos, preferred: "unloaded/repo") == ["acme/app"])
        #expect(BoardState.startingRepositorySelection(from: []).isEmpty)
    }
    @Test func repositoryPopoverRemainsOpenAcrossSelections() {
        let state = BoardState()
        let repos = [Repository(id: "acme/app"), Repository(id: "acme/library"), Repository(id: "acme/service")]
        state.toggleRepositoryPopover()
        #expect(state.repositoryPopoverPresented)
        state.setRepositoryFilter(["acme/app", "acme/library"], available: repos)
        #expect(state.repositoryPopoverPresented)
        state.setRepositoryFilter(["acme/library"], available: repos)
        #expect(state.repositoryPopoverPresented)
        state.setRepositoryFilter([], available: repos)
        #expect(state.repositoryPopoverPresented)
        #expect(state.panel == nil)
        state.dismissRepositoryPopover()
        #expect(!state.repositoryPopoverPresented)
        #expect(state.repositories.isEmpty)
    }
    @Test func repositoryPopoverYieldsToOtherNavigation() {
        let state = BoardState()
        state.toggleRepositoryPopover()
        state.panel = .status
        #expect(!state.repositoryPopoverPresented)
        #expect(state.panel == .status)
        state.toggleRepositoryPopover()
        #expect(state.panel == nil)
        state.switchBoard(.contributors)
        #expect(!state.repositoryPopoverPresented)
        state.toggleRepositoryPopover()
        state.resetForAccount(WorkspaceConfiguration())
        #expect(!state.repositoryPopoverPresented)
    }
    @Test func toolbarPopoversSwitchWithoutStaleDismissalsClosingTheNewPopover() {
        let state = BoardState()
        state.toggleRepositoryPopover()
        state.toggleToolbarPopover(.date)
        #expect(state.toolbarPopover == .date)
        #expect(!state.repositoryPopoverPresented)
        state.repositoryPopoverPresented = false
        #expect(state.toolbarPopover == .date)
        state.toggleToolbarPopover(.groups)
        state.setToolbarPopover(.date, presented: false)
        #expect(state.toolbarPopover == .groups)
        state.toggleToolbarPopover(.groups)
        #expect(state.toolbarPopover == nil)
        state.panel = .status
        state.toggleToolbarPopover(.date)
        #expect(state.panel == nil)
        #expect(state.toolbarPopover == .date)
    }
    @Test func allToolbarPopoversIsolateShortcutsAndYieldToNavigation() {
        let state = BoardState()
        for popover in [ToolbarPopover.date, .repositories, .groups] {
            state.toggleToolbarPopover(popover)
            #expect(state.handlePopoverShortcut("r"))
            #expect(state.handlePopoverShortcut("a"))
            #expect(state.toolbarPopover == popover)
            #expect(state.handlePopoverShortcut("\u{1b}"))
            #expect(state.toolbarPopover == nil)
            #expect(!state.handlePopoverShortcut("a"))
            state.toggleToolbarPopover(popover)
            state.panel = .filters
            #expect(state.toolbarPopover == nil)
            state.toggleToolbarPopover(popover)
            state.switchBoard(.contributors)
            #expect(state.toolbarPopover == nil)
            state.toggleToolbarPopover(popover)
            state.resetForAccount(WorkspaceConfiguration())
            #expect(state.toolbarPopover == nil)
        }
    }
    @Test func dataStatusIndicatorQualifiesIncompleteAndCachedEvidence() {
        let complete = Repository(id: "acme/app", prInventoryComplete: true, prFilesComplete: true, prReviewersComplete: true)
        var snapshot = Snapshot(repositories: [complete])
        let loaded = DataStatusPresentation(snapshot: snapshot, busy: false, checkingSignIn: false, signedIn: true, error: "")
        #expect(!loaded.warning && !loaded.loading)
        snapshot.repositories[0].prFilesComplete = false
        #expect(DataStatusPresentation(snapshot: snapshot, busy: false, checkingSignIn: false, signedIn: true, error: "").warning)
        snapshot.cachedOnly = true
        let cached = DataStatusPresentation(snapshot: snapshot, busy: false, checkingSignIn: false, signedIn: true, error: "")
        #expect(cached.label == "Cached history")
        #expect(!cached.warning)
        #expect(DataStatusPresentation(snapshot: snapshot, busy: true, checkingSignIn: false, signedIn: true, error: "").loading)
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
