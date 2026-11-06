import Foundation
import Testing

@testable import Bigboard

@MainActor struct HeatBoardTests {
    @Test func boardsKeepTheirStateAndExplicitExplorationCarriesHeatScope() {
        let state = BoardState()
        #expect(Board.allCases == [.heat, .activity, .contributors, .pullRequests])
        #expect(state.board == .heat && state.lens == .directories)
        state.heat.query = "Sources"
        state.heat.people = ["alex@example.test"]
        state.heat.hideBots = true
        state.heatMetric = "people"
        state.heatScope = ActivityScope(repo: "acme/app", path: "Sources")
        state.activity.query = "existing"
        state.activitySelection = "file"
        state.scope = ActivityScope(repo: "acme/other")
        state.lens = .related
        state.contributors.query = "Sam"
        state.switchBoard(.activity)
        #expect(state.activity.query == "existing" && state.lens == .related)
        state.switchBoard(.heat)
        #expect(state.heat.query == "Sources" && state.heatMetric == "people")
        #expect(state.scope.repo == "acme/other")
        let interval = DateInterval(start: Date(timeIntervalSince1970: 100), duration: 3600)
        state.showHeatActivity(state.heatScope, interval: interval)
        #expect(state.board == .activity && state.lens == .directories)
        #expect(state.scope == state.heatScope && state.activityInterval == interval)
        #expect(state.activity.people == state.heat.people && state.activity.hideBots)
        #expect(state.activity.query.isEmpty && state.activitySelection == nil)
        state.switchBoard(.heat)
        #expect(state.heat.query == "Sources" && state.heatScope.path == "Sources")
        #expect(state.contributors.query == "Sam")
        state.switchBoard(.activity)
        #expect(state.lens == .directories && state.scope == state.heatScope)
        state.dateRange = .days(90)
        #expect(state.activityInterval == nil)
    }
    @Test func heatBackPreservesWorkAreaAndActivityScope() {
        let state = BoardState()
        state.scope = ActivityScope(repo: "acme/other", path: "Tests")
        state.heatScope = ActivityScope(repo: "acme/app", path: "Sources/UI", areaName: "Interface")
        state.goUp()
        #expect(state.heatScope.path == "Sources" && state.heatScope.areaName == "Interface")
        state.goUp()
        #expect(state.heatScope.path.isEmpty && state.heatScope.areaName == "Interface")
        state.goUp()
        #expect(state.heatScope.repo == "acme/app" && state.heatScope.areaName.isEmpty)
        state.goUp()
        #expect(state.heatScope.repo.isEmpty)
        #expect(state.scope == ActivityScope(repo: "acme/other", path: "Tests"))
    }
    @Test func cellDrillDownFiltersExactIntervalAndPreservesTheSharedRange() {
        let now = Date(timeIntervalSince1970: 2_000_000_000)
        let interval = DateInterval(start: now.addingTimeInterval(-20 * 86_400), duration: 3600)
        func commit(_ id: String, date: Date) -> Commit {
            Commit(
                id: id, repo: "acme/app", author: "Alex", email: "alex@example.test", date: date,
                subject: id)
        }
        let snapshot = Snapshot(
            generatedAt: now,
            commits: [
                commit("start", date: interval.start),
                commit("inside", date: interval.start.addingTimeInterval(1)),
                commit("end", date: interval.end),
                commit("after", date: interval.end.addingTimeInterval(1)), commit("now", date: now),
            ])
        let state = BoardState()
        state.dateRange = .days(14)
        state.showHeatActivity(ActivityScope(repo: "acme/app"), interval: interval)
        #expect(state.dateRange == .days(14) && state.lens == .directories)
        #expect(state.filteredCommits(snapshot).map(\.id) == ["inside", "end"])
        #expect(
            state.timelinePoints(state.filteredCommits(snapshot), snapshot: snapshot).reduce(0) {
                $0 + $1.commits
            } == 2)
        #expect(state.filteredCommits(snapshot, for: .contributors).map(\.id) == ["now"])
        let output = BoardMarkdown.render(
            snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state)
        #expect(output.contains("Range: \(HeatRange.label(interval))"))
        #expect(!output.contains("commit/after"))
        state.switchBoard(.pullRequests)
    }
    @Test func allTimeCellIncludesItsFirstCommitWithoutDuplicatingIt() throws {
        let now = Date(timeIntervalSince1970: 2_000_000_000)
        let start = Calendar.current.startOfDay(for: now.addingTimeInterval(-2 * 86_400))
        let commit = Commit(
            id: "first", repo: "acme/app", author: "Alex", email: "alex@example.test", date: start,
            subject: "First")
        let snapshot = Snapshot(generatedAt: now, commits: [commit])
        let state = BoardState()
        state.dateRange = .days(0)
        let series = Analysis.heatSeries([commit], metric: "commits", range: state.heatRange(snapshot))
        #expect(series.series.reduce(0) { $0 + $1.value } == 1)
        let cell = try #require(series.series.first)
        state.showHeatActivity(
            ActivityScope(repo: "acme/app"), interval: cell.interval,
            includesStart: cell.includesStart)
        let records = state.filteredCommits(snapshot)
        #expect(records.map(\.id) == ["first"])
        #expect(state.timelinePoints(records, snapshot: snapshot).reduce(0) { $0 + $1.commits } == 1)
    }
    @Test func heatFiltersAndExportShowOnlySelectedActivity() {
        let now = Date(timeIntervalSince1970: 2_000_000_000)
        func commit(_ id: String, days: Int, email: String = "alex@example.test", bot: Bool = false)
            -> Commit
        {
            var value = Commit(
                id: id, repo: "acme/app", author: "Alex", email: email,
                date: now.addingTimeInterval(-Double(days) * 86_400), subject: id, added: 10)
            value.bot = bot
            return value
        }
        let snapshot = Snapshot(
            generatedAt: now, repositories: [Repository(id: "acme/app", scannedAt: now)],
            commits: [
                commit("current", days: 1), commit("previous", days: 20), commit("old", days: 40),
                commit("bot", days: 2, email: "bot@example.test", bot: true),
                commit("other", days: 20, email: "sam@example.test"),
            ])
        let state = BoardState()
        state.heat.hideBots = true
        state.heat.people = ["alex@example.test"]
        state.activity.people = ["sam@example.test"]
        let records = state.heatRecords(snapshot)
        #expect(records.map(\.id) == ["current", "previous", "old"])
        let series = Analysis.heatSeries(records, range: state.heatRange(snapshot))
        #expect(series.total == 10)
        let markdown = BoardMarkdown.render(
            snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state)
        #expect(markdown.contains("# Bigboard · Heat Maps"))
        #expect(!markdown.contains("Previous") && !markdown.contains("Difference"))
        #expect(markdown.contains("| acme/app | 10 | Loaded history |"))
        state.heat.query = "no match"
        #expect(
            BoardMarkdown.render(
                snapshot: snapshot, configuration: WorkspaceConfiguration(), navigation: state
            ).contains("| acme/app |"))
    }
    @Test func accountAndAccessChangesClearHeatContext() {
        let state = BoardState()
        state.heatScope = ActivityScope(repo: "private/repo")
        state.reconcile(Snapshot(repositories: [Repository(id: "public/repo")]))
        #expect(state.heatScope.repo.isEmpty)
        state.heatScope = ActivityScope(repo: "public/repo")
        state.repositories = ["another/repo"]
        state.reconcile()
        #expect(state.heatScope.repo.isEmpty)
        state.heat.people = ["alex@example.test"]
        state.heatMetric = "people"
        state.activityInterval = DateInterval(start: .distantPast, end: Date())
        state.resetForAccount(WorkspaceConfiguration())
        #expect(state.heat.people.isEmpty && state.heatMetric == "lines")
        #expect(state.activityInterval == nil)
    }
}
