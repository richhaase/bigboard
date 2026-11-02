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
    @Test func prMineShortcutsDoNotChangeFiltersOnOtherBoards() {
        let state = BoardState()
        state.activity.query = "source"; state.activity.people = ["alex@example.test"]
        state.contributors.query = "alex"; state.contributors.hideBots = true
        state.prs.query = "release"; state.prs.mine = ["authored", "requested"]
        let activity = state.activity, contributors = state.contributors, prs = state.prs
        for board in [Board.activity, .contributors] {
            state.switchBoard(board)
            for value in ["attention", "authored", "requested"] {
                state.togglePRMine(value)
                #expect(state.board == board)
                #expect(state.activity == activity)
                #expect(state.contributors == contributors)
                #expect(state.prs == prs)
            }
        }
        state.switchBoard(.pullRequests)
        #expect(state.prs == prs)
    }
    @Test func prMineShortcutsToggleOnlyTheirFilterOnPullRequests() {
        let state = BoardState()
        state.activity.query = "source"; state.contributors.query = "alex"
        state.prs.query = "release"; state.prs.checks = ["FAILURE"]
        let activity = state.activity, contributors = state.contributors, prs = state.prs
        state.switchBoard(.pullRequests)
        for value in ["attention", "authored", "requested"] {
            state.prs.mine = ["authored", "requested"]
            state.togglePRMine(value)
            var expected = prs; expected.mine = [value]
            #expect(state.prs == expected)
            state.togglePRMine(value)
            #expect(state.prs == prs)
            #expect(state.activity == activity)
            #expect(state.contributors == contributors)
        }
    }
    @Test func heatKeepsHistoryWhileHonoringSharedRepositoriesAndPeople() {
        let snapshot = Snapshot(generatedAt: now, commits: [commit("recent"), commit("old", days: 100), commit("other", repo: "acme/other"), commit("otherperson", email: "sam@example.test")], bookmark: now.addingTimeInterval(-2 * 86400))
        let state = BoardState(); state.repositories = ["acme/app"]; state.activity.people = ["alex@example.test"]; state.sinceLastLook = true
        #expect(state.filteredCommits(snapshot).map(\.id) == ["recent"])
        #expect(state.filteredCommits(snapshot, heat: true).map(\.id) == ["recent", "old"])
    }
    @Test func boardTimelinesFollowBookmarkInsteadOfPreviousDaySelection() {
        let snapshot = Snapshot(generatedAt: now, commits: [commit("recent"), commit("older", days: 40), commit("oldest", days: 700), commit("before", days: 850)], bookmark: now.addingTimeInterval(-800 * 86400))
        let state = BoardState(); state.days = 14; state.sinceLastLook = true
        for board in [Board.activity, .contributors] {
            state.board = board
            let records = state.filteredCommits(snapshot)
            #expect(records.map(\.id) == ["recent", "older", "oldest"])
            let points = state.timelinePoints(records, snapshot: snapshot)
            #expect(points.reduce(0) { $0 + $1.commits } == records.count)
            #expect(points.count > 24)
        }
        state.sinceLastLook = false
        #expect(state.timelinePoints(snapshot.commits, snapshot: snapshot).reduce(0) { $0 + $1.commits } == 1)
    }
    @Test func bookmarkTimelineFallsBackWithoutBookmarkAndHeatIgnoresActiveRange() {
        var snapshot = Snapshot(generatedAt: now, commits: [commit("recent"), commit("old", days: 100)])
        let state = BoardState(); state.days = 14; state.sinceLastLook = true
        #expect(state.timelinePoints(snapshot.commits, snapshot: snapshot).reduce(0) { $0 + $1.commits } == 1)
        snapshot.bookmark = now.addingTimeInterval(-2 * 86400)
        #expect(state.timelinePoints(snapshot.commits, snapshot: snapshot).reduce(0) { $0 + $1.commits } == 1)
        #expect(state.timelinePoints(snapshot.commits, snapshot: snapshot, heat: true).reduce(0) { $0 + $1.commits } == 2)
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
    @Test func repositoryChecklistStartsWithAllAndProtectsOnlyTheLastCheckedRepository() throws {
        let available = Set((1...13).map { "acme/repo\($0)" })
        var checklist = RepositoryChecklist(selection: [], available: available)
        #expect(checklist.checked == available)
        checklist.selection = try #require(checklist.setting("acme/repo1", checked: false))
        #expect(checklist.checked.count == 12)
        #expect(!checklist.checked.contains("acme/repo1"))
        for id in available.subtracting(["acme/repo1", "acme/repo13"]) {
            checklist.selection = try #require(checklist.setting(id, checked: false))
        }
        #expect(checklist.checked == ["acme/repo13"])
        #expect(checklist.setting("acme/repo13", checked: false) == nil)
        checklist.selection = try #require(checklist.setting("acme/repo1", checked: true))
        #expect(checklist.checked == ["acme/repo1", "acme/repo13"])
        #expect(checklist.setting("acme/repo13", checked: false) == ["acme/repo1"])
        #expect(checklist.setting("acme/repo1", checked: false) == ["acme/repo13"])
    }
    @Test func repositoryChecklistSelectAllUsesAllLoadedAndRejectsUnavailableRows() throws {
        let available: Set<String> = ["acme/app", "acme/library"]
        var checklist = RepositoryChecklist(selection: ["acme/app"], available: available)
        checklist.selection = try #require(checklist.setting("acme/library", checked: true))
        #expect(checklist.selection.isEmpty)
        #expect(checklist.checked == available)
        checklist.available.insert("acme/new")
        #expect(checklist.checked.contains("acme/new"))
        #expect(checklist.setting("unloaded/repo", checked: true) == nil)
        #expect(RepositoryChecklist(selection: [], available: []).checked.isEmpty)
    }
    @Test func repositoryPopoverAppliesChecklistChangesImmediatelyAndKeepsThemWhenClosed() throws {
        let state = BoardState()
        let repos = [Repository(id: "acme/app"), Repository(id: "acme/library"), Repository(id: "acme/service")]
        let available = Set(repos.map(\.id))
        state.toggleToolbarPopover(.repositories)
        let changed = try #require(RepositoryChecklist(selection: state.repositories, available: available).setting("acme/app", checked: false))
        #expect(state.setRepositoryFilter(changed, available: repos))
        #expect(state.toolbarPopover == .repositories)
        #expect(state.repositories == ["acme/library", "acme/service"])
        #expect(state.visibleRepositories(Snapshot(repositories: repos)).map(\.id) == ["acme/library", "acme/service"])
        state.dismissToolbarPopover()
        #expect(state.repositories == ["acme/library", "acme/service"])
        state.toggleToolbarPopover(.repositories)
        #expect(RepositoryChecklist(selection: state.repositories, available: available).checked == ["acme/library", "acme/service"])
        state.dismissToolbarPopover()
        #expect(state.toolbarPopover != .repositories)
        #expect(state.repositories == ["acme/library", "acme/service"])
        #expect(state.setRepositoryFilter([], available: repos))
        #expect(RepositoryChecklist(selection: state.repositories, available: available).checked == available)
    }
    @Test func repositoryPopoverYieldsToOtherNavigation() {
        let state = BoardState()
        state.toggleToolbarPopover(.repositories)
        state.panel = .status
        #expect(state.toolbarPopover != .repositories)
        #expect(state.panel == .status)
        state.toggleToolbarPopover(.repositories)
        #expect(state.panel == nil)
        state.switchBoard(.contributors)
        #expect(state.toolbarPopover != .repositories)
        state.toggleToolbarPopover(.repositories)
        state.resetForAccount(WorkspaceConfiguration())
        #expect(state.toolbarPopover != .repositories)
    }
    @Test func toolbarPopoversSwitchWithoutStaleDismissalsClosingTheNewPopover() {
        let state = BoardState()
        state.toggleToolbarPopover(.repositories)
        state.toggleToolbarPopover(.date)
        #expect(state.toolbarPopover == .date)
        #expect(state.toolbarPopover != .repositories)
        state.setToolbarPopover(.repositories, presented: false)
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
    @Test func allToolbarPopoversDismissAndYieldToNavigation() {
        let state = BoardState()
        for popover in [ToolbarPopover.date, .repositories, .groups] {
            state.toggleToolbarPopover(popover)
            #expect(state.toolbarPopover == popover)
            state.dismissToolbarPopover()
            #expect(state.toolbarPopover == nil)
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
    @Test func markdownDistinguishesHistoricalFileAndDirectoryRowsAndScopes() {
        var newer = commit("new"), older = commit("old")
        newer.files = [FileChange(path: "component", removed: 1), FileChange(path: "component/new.swift", added: 100)]
        older.files = [FileChange(path: "component", added: 1)]
        let snapshot = Snapshot(generatedAt: now, repositories: [Repository(id: "acme/app")], commits: [newer, older])
        let state = BoardState(); state.lens = .directories; state.scope = ActivityScope(repo: "acme/app")
        let markdown = BoardMarkdown.render(snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state)
        #expect(markdown.contains("| component | 2 | 1 | 1 | 1 |"))
        #expect(markdown.contains("| component/ | 1 | 1 | 100 | 0 |"))
        state.scope.path = "component"; state.lens = .commits
        #expect(BoardMarkdown.render(snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state).contains("Scope: acme/app / component/\n"))
        state.scope.isFile = true
        #expect(BoardMarkdown.render(snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state).contains("Scope: acme/app / component\n"))
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
