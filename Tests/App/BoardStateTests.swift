import Foundation
import Testing
@testable import Bigboard

@MainActor struct BoardStateTests {
    let now = Date(timeIntervalSince1970: 2_000_000_000)
    func commit(_ id: String, repo: String = "acme/app", email: String = "alex@example.test", days: Int = 1, added: Int = 10) -> Commit { Commit(id: id, repo: repo, author: email, email: email, date: now.addingTimeInterval(-Double(days) * 86400), subject: "Change \(id)", added: added, files: [FileChange(path: "Sources/App.swift", added: added)]) }
    @Test func contributorsSortByRecencyUnlessConfigured() {
        let state = BoardState()
        #expect(state.contributors.sort == "recent")
        var configuration = WorkspaceConfiguration(); configuration.contributorSort = "change"
        state.resetForAccount(configuration)
        #expect(state.contributors.sort == "change")
    }
    @Test func contextualPRNavigationRestoresIndependentState() {
        let state = BoardState()
        state.dateRange = .days(30); state.repositories = ["acme/app"]
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
        #expect(state.dateRange == .days(30))
        #expect(state.repositories == ["acme/app"])
        state.showPRs(repo: "acme/app")
        state.switchBoard(.contributors)
        #expect(state.contextualRepository == nil)
        #expect(state.prs.query == "release")
    }
    @Test func openPullRequestsIgnoreHistoricalDatesButKeepRepositoryAndAttentionFilters() {
        let stale = PullRequest(repo: "acme/app", number: 1, title: "Waiting", author: "alex", url: "", updatedAt: now.addingTimeInterval(-90 * 86400), reviewers: ["viewer"])
        let unknown = PullRequest(repo: "acme/app", number: 2, title: "Unknown date", author: "sam", url: "", updatedAt: nil)
        let other = PullRequest(repo: "acme/other", number: 3, title: "Other repository", author: "alex", url: "", updatedAt: now)
        let snapshot = Snapshot(generatedAt: now, viewer: "viewer", pullRequests: [stale, unknown, other])
        let state = BoardState(); state.dateRange = .hours(1)
        state.repositories = ["acme/app"]
        state.showHeatActivity(ActivityScope(repo: "acme/app"), interval: DateInterval(start: now.addingTimeInterval(-60), end: now))
        state.showPRs(repo: "acme/app")
        #expect(state.pullRequests(snapshot).map(\.number) == [1, 2])
        state.prs.mine = ["requested"]
        #expect(state.pullRequests(snapshot).map(\.number) == [1])
        let markdown = BoardMarkdown.render(snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state)
        #expect(markdown.contains("Range: Current open work") && !markdown.contains("Last hour"))
        #expect(markdown.contains("Waiting"))
        state.returnToActivity()
        #expect(state.dateRange == .hours(1) && state.activityInterval != nil)
    }
    @Test func searchesStayWithTheirContentInsteadOfFilteringDirectoryBrowsing() {
        let state = BoardState()
        let snapshot = Snapshot(generatedAt: now, repositories: [Repository(id: "acme/app")], commits: [commit("one")])
        #expect(state.searchPrompt == nil)
        state.switchBoard(.activity)
        #expect(state.searchPrompt == nil)
        state.setLens(.commits)
        state.searchQuery = "missing commit"
        #expect(state.searchCommits(snapshot.commits).isEmpty)
        state.setLens(.directories)
        #expect(state.searchPrompt == nil)
        #expect(state.activityRows(snapshot, configuration: WorkspaceConfiguration()).count == 1)
        #expect(!BoardMarkdown.render(snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state).contains("Search:"))
        state.setLens(.people)
        state.searchQuery = "Alex"
        #expect(state.activityPeopleQuery == "Alex" && state.activity.query == "missing commit")
        state.setLens(.commits)
        #expect(state.searchQuery == "missing commit")
        state.switchBoard(.contributors)
        state.searchQuery = "Sam"
        state.switchBoard(.activity)
        #expect(state.searchQuery == "missing commit" && state.contributors.query == "Sam")
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
        let snapshot = Snapshot(generatedAt: now, commits: [commit("recent"), commit("old", days: 100), commit("other", repo: "acme/other"), commit("otherperson", email: "sam@example.test")])
        let state = BoardState(); state.repositories = ["acme/app"]; state.activity.people = ["alex@example.test"]; state.heat.people = ["alex@example.test"]; state.dateRange = .days(2)
        #expect(state.filteredCommits(snapshot).map(\.id) == ["recent"])
        #expect(state.heatRecords(snapshot).map(\.id) == ["recent", "old"])
    }
    @Test(arguments: [DateRange.hours(1), .hours(4), .hours(12), .days(1), .days(7), .days(43)])
    func rollingRangesAgreeAcrossHistoricalBoards(range: DateRange) throws {
        let start = try #require(range.start(relativeTo: now))
        let dates = [start.addingTimeInterval(-1), start, start.addingTimeInterval(1), now]
        let records = dates.enumerated().map { index, date in
            var value = commit(String(index)); value.date = date; return value
        }
        let prs = dates.enumerated().map { index, date in
            PullRequest(repo: "acme/app", number: index, title: "Change", author: "alex", url: "", updatedAt: date)
        }
        let snapshot = Snapshot(generatedAt: now, commits: records, pullRequests: prs)
        let state = BoardState(); state.dateRange = range
        for board in [Board.heat, .activity, .contributors] {
            state.switchBoard(board)
            #expect(state.filteredCommits(snapshot).map(\.id) == ["2", "3"])
            #expect(state.timelinePoints(state.filteredCommits(snapshot), snapshot: snapshot).reduce(0) { $0 + $1.commits } == 2)
        }
        #expect(state.pullRequests(snapshot).map(\.number) == [3, 2, 1, 0])
        let heat = state.heatRange(snapshot)
        #expect(heat.current.start == start && heat.current.end == now)
        #expect(Analysis.heatSeries(records, metric: "commits", range: heat).total == 2)
    }
    @Test func allTimeContributorTimelineRetainsSelectedRepositoriesAndPeopleHistory() throws {
        let old = commit("old", days: 800)
        let snapshot = Snapshot(generatedAt: now, commits: [
            old, commit("recent"),
            commit("other-repository", repo: "acme/other", days: 1400),
            commit("other-person", email: "sam@example.test", days: 1200)
        ])
        let state = BoardState()
        state.switchBoard(.contributors)
        state.dateRange = .days(0)
        state.repositories = ["acme/app"]
        state.contributors.people = ["alex@example.test"]
        let people = state.peopleRows(snapshot)
        #expect(people.count == 1)
        let person = try #require(people.first)
        let points = state.timelinePoints(person.records, snapshot: snapshot)
        let calendar = Calendar.current
        #expect(state.rangeLabel == "All time")
        #expect(Set(person.records.map(\.id)) == ["old", "recent"])
        #expect(points.first?.date == calendar.date(from: calendar.dateComponents([.year, .month], from: old.date)))
        #expect(points.reduce(0) { $0 + $1.commits } == person.totals.commits)
        #expect(points.reduce(0) { $0 + $1.commits } == 2)
    }
    @Test func refreshMovesTheWindowWithoutChangingItsDuration() {
        var recent = commit("recent"); recent.date = now.addingTimeInterval(-1800)
        var snapshot = Snapshot(generatedAt: now, commits: [recent])
        let state = BoardState(); state.dateRange = .hours(1)
        #expect(state.filteredCommits(snapshot).count == 1)
        snapshot.generatedAt = now.addingTimeInterval(3600)
        #expect(state.filteredCommits(snapshot).isEmpty)
        #expect(state.dateRange == .hours(1) && state.rangeLabel == "Last hour")
        state.showHeatActivity(ActivityScope(repo: "acme/app"), interval: DateInterval(start: now, duration: 60))
        state.dateRange = .days(1)
        #expect(state.activityInterval == nil && state.rangeLabel == "Last 24 hours")
        #expect(state.filteredCommits(snapshot).count == 1)
        state.dateRange = .days(0)
        #expect(state.filteredCommits(snapshot).count == 1 && state.rangeLabel == "All time")
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
        state.repositoryFilterPresented.toggle()
        let changed = try #require(RepositoryChecklist(selection: state.repositories, available: available).setting("acme/app", checked: false))
        #expect(state.setRepositoryFilter(changed, available: repos))
        #expect(state.repositoryFilterPresented)
        #expect(state.repositories == ["acme/library", "acme/service"])
        #expect(state.visibleRepositories(Snapshot(repositories: repos)).map(\.id) == ["acme/library", "acme/service"])
        state.repositoryFilterPresented = false
        #expect(state.repositories == ["acme/library", "acme/service"])
        state.repositoryFilterPresented.toggle()
        #expect(RepositoryChecklist(selection: state.repositories, available: available).checked == ["acme/library", "acme/service"])
        state.repositoryFilterPresented = false
        #expect(!state.repositoryFilterPresented)
        #expect(state.repositories == ["acme/library", "acme/service"])
        #expect(state.setRepositoryFilter([], available: repos))
        #expect(RepositoryChecklist(selection: state.repositories, available: available).checked == available)
    }
    @Test func repositoryPopoverYieldsToOtherNavigation() {
        let state = BoardState()
        state.repositoryFilterPresented.toggle()
        state.panel = .status
        #expect(!state.repositoryFilterPresented)
        #expect(state.panel == .status)
        state.repositoryFilterPresented.toggle()
        #expect(state.panel == nil)
        state.switchBoard(.contributors)
        #expect(!state.repositoryFilterPresented)
        state.repositoryFilterPresented.toggle()
        state.resetForAccount(WorkspaceConfiguration())
        #expect(!state.repositoryFilterPresented)
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
        snapshot.coverage.message = "Loading pull-request evidence…"
        let refreshing = DataStatusPresentation(snapshot: snapshot, busy: true, checkingSignIn: false, signedIn: true, error: "")
        #expect(refreshing.loading && refreshing.label == snapshot.coverage.message)
        snapshot.coverage.message = ""
        #expect(DataStatusPresentation(snapshot: snapshot, busy: true, checkingSignIn: false, signedIn: true, error: "").label == "Refreshing repository evidence")
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
        let state = BoardState(); state.board = .activity; state.lens = .commits
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
        let state = BoardState(); state.board = .activity; state.lens = .directories; state.scope = ActivityScope(repo: "acme/app")
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
        state.dateRange = .hours(1); state.contributors.people = ["old@example.test"]
        var configuration = WorkspaceConfiguration(); configuration.sinceDays = 90; configuration.contributorSort = "commits"
        state.resetForAccount(configuration)
        #expect(state.dateRange == .days(90))
        #expect(state.contributors.people.isEmpty)
        #expect(state.contributors.sort == "commits")
    }
}
