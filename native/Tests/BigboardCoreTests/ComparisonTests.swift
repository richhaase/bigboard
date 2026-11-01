import Foundation
import Testing
@testable import Bigboard

struct ComparisonTests {
    var calendar: Calendar { var c = Calendar(identifier: .gregorian); c.timeZone = TimeZone(identifier: "America/Denver")!; return c }
    func date(_ text: String) -> Date { ISO8601DateFormatter().date(from: text)! }
    var now: Date { date("2026-11-01T19:00:00Z") }
    func commit(_ id: String, at date: Date, repo: String = "o/r", email: String = "a@x", files: [FileChange] = []) -> Commit {
        Commit(id: id, repo: repo, author: email, email: email, date: date, subject: id, added: files.reduce(0) { $0 + $1.added }, removed: files.reduce(0) { $0 + $1.removed }, files: files)
    }
    func snapshot(_ records: [Commit], repos: [Repository]? = nil) -> Snapshot {
        Snapshot(generatedAt: now, accountID: "one", repositories: repos ?? [Repository(id: "o/r", scannedAt: now)], commits: records)
    }
    func build(_ records: [Commit], scope: ActivityScope = ActivityScope(), repos: [Repository]? = nil, configuration: WorkspaceConfiguration = WorkspaceConfiguration(), people: Set<String> = [], hideBots: Bool = false) throws -> ComparisonProjection {
        try ComparisonProjection.build(snapshot: snapshot(records, repos: repos), configuration: configuration, comparison: .preset(days: 7, now: now, calendar: calendar), repositories: [], scope: scope, hideBots: hideBots, people: people)
    }
    @Test func calendarRangesIncludeEndDateAndUseHalfOpenBoundaries() {
        let c = HistoryComparison.preset(days: 7, now: now, calendar: calendar)
        #expect(c.a.start == c.b.end)
        #expect(c.a.contains(c.a.start, snapshot: now))
        #expect(!c.b.contains(c.a.start, snapshot: now))
        #expect(!c.a.contains(c.a.end, snapshot: now))
        let period = HistoryComparison.calendarPeriod(start: c.a.start, through: c.a.end.addingTimeInterval(-1), calendar: calendar)
        #expect(period == c.a)
    }
    @Test func equivalentCalendarPeriodsCrossDSTAndLeapDay() {
        let spring = HistoryComparison.preset(days: 7, now: date("2026-03-10T18:00:00Z"), calendar: calendar)
        #expect(spring.a.end.timeIntervalSince(spring.a.start) == 7 * 86400 - 3600)
        #expect(spring.b.end.timeIntervalSince(spring.b.start) == 7 * 86400)
        let leap = HistoryComparison.preset(days: 1, now: date("2024-03-01T18:00:00Z"), calendar: calendar)
        #expect(calendar.component(.day, from: leap.a.start) == 29)
        #expect(calendar.component(.day, from: leap.b.start) == 28)
    }
    @Test func partialTodayExcludesFutureEvidenceAndRatesUseElapsedDuration() {
        let start = calendar.startOfDay(for: now)
        let period = HistoryComparison.calendarPeriod(start: start, through: now, calendar: calendar)
        #expect(period.partial(at: now))
        #expect(period.contains(now, snapshot: now))
        #expect(!period.contains(now.addingTimeInterval(1), snapshot: now))
        let c = HistoryComparison(a: period, b: .init(start: start.addingTimeInterval(-86400), end: start), timeZoneID: calendar.timeZone.identifier)
        let totals = ComparisonValues(a: ActivityTotals(commits: 12, people: 2), b: ActivityTotals(commits: 12, people: 3))
        let values = totals.values(.commits, comparison: c, snapshot: now, rates: true)
        #expect(abs(values.0 - 12 * 86400 / period.duration(at: now)) < 0.00001 && values.1 == 12)
        let people = totals.values(.people, comparison: c, snapshot: now, rates: true)
        #expect(people.0 == 2 && people.1 == 3)
    }
    @Test func validationAndOverlap() {
        var c = HistoryComparison.preset(days: 7, now: now, calendar: calendar)
        #expect(c.validation(at: now) == nil && !c.overlaps)
        c.b = c.a
        #expect(c.overlaps && c.validation(at: now) == nil)
        c.a.end = c.a.start
        #expect(c.validation(at: now) != nil)
        c.a = .init(start: now.addingTimeInterval(1), end: now.addingTimeInterval(86400))
        #expect(c.validation(at: now) != nil)
        c.a = c.b; c.a.end = now.addingTimeInterval(3 * 86400)
        #expect(c.validation(at: now) != nil)
    }
    @Test func unionIncludesBaselineOnlyPathsAndQuietRepositories() throws {
        let c = HistoryComparison.preset(days: 7, now: now, calendar: calendar)
        let records = [commit("a", at: c.a.start, files: [.init(path: "new/a.swift", added: 8)]), commit("b", at: c.b.start, files: [.init(path: "old/b.swift", added: 20)])]
        let p = try build(records, scope: .init(repo: "o/r"))
        #expect(Set(p.rows.map(\.label)) == ["new", "old"])
        #expect(p.rows.first { $0.label == "old" }?.values.a.commits == 0)
        #expect(p.rows.first { $0.label == "old" }?.values.b.added == 20)
        let root = try build(records, repos: [Repository(id: "o/r", scannedAt: now), Repository(id: "o/quiet", scannedAt: now)])
        #expect(root.rows.count == 2)
        #expect(root.rows.first { $0.label == "o/quiet" }?.values.reliable == true)
    }
    @Test func scopedTotalsUseOnlyMatchingFilesAndPreserveExcludedRules() throws {
        let c = HistoryComparison.preset(days: 7, now: now, calendar: calendar)
        let records = [commit("a", at: c.a.start, files: [.init(path: "src/a", added: 3), .init(path: "src2/b", added: 80), .init(path: "src/generated", added: 100, excludedReason: "path")])]
        let p = try build(records, scope: .init(repo: "o/r", path: "src"))
        #expect(p.evidence.values.a.added == 3)
        #expect(p.rows.count == 1 && p.rows.first?.label == "a")
        let file = try build(records, scope: .init(repo: "o/r", path: "src/a", isFile: true))
        #expect(file.evidence.a.count == 1 && file.evidence.values.a.added == 3)
    }
    @Test func deduplicationKeepsRepositoryAssociations() throws {
        let c = HistoryComparison.preset(days: 7, now: now, calendar: calendar)
        let records = [commit("same", at: c.a.start), commit("same", at: c.a.start, repo: "o/s")]
        let p = try build(records, repos: [Repository(id: "o/r", scannedAt: now), Repository(id: "o/s", scannedAt: now)])
        #expect(p.evidence.values.a.commits == 1)
        #expect(p.rows.count == 2 && p.rows.allSatisfy { $0.values.a.commits == 1 })
        #expect(p.evidence.people.count == 1)
    }
    @Test func canonicalIdentityAndBotFiltersAreConsistentAcrossOverlappingPeriods() throws {
        let c = HistoryComparison.preset(days: 7, now: now, calendar: calendar)
        var bot = commit("b", at: c.b.start, email: "bot@x"); bot.bot = true
        let records = [commit("a", at: c.a.start), bot]
        let p = try build(records, hideBots: true)
        #expect(p.recordsB.isEmpty && p.evidence.people.count == 1)
        let selected = try build(records, people: ["bot@x"])
        #expect(selected.recordsA.isEmpty && selected.recordsB.count == 1)
        var comparison = c; comparison.b = c.a
        let overlap = try ComparisonProjection.build(snapshot: snapshot(records), configuration: .init(), comparison: comparison, repositories: [], scope: .init(), hideBots: false, people: [])
        #expect(overlap.evidence.values.a.commits == 1 && overlap.evidence.values.b.commits == 1)
    }
    @Test func unknownPathsAndStaleHistoryNeverBecomeVerifiedZero() throws {
        let c = HistoryComparison.preset(days: 7, now: now, calendar: calendar)
        var unknown = commit("x", at: c.a.start); unknown.pathsUnknown = true
        let p = try build([unknown], scope: .init(repo: "o/r", path: "src"))
        #expect(!p.evidence.values.reliable && !p.evidence.warnings.isEmpty)
        #expect(p.evidence.values.relative(.commits, a: 0, b: 0) == "Incomplete evidence")
        let missing = try build([], repos: [Repository(id: "o/r")])
        #expect(!missing.evidence.values.reliable)
        let stale = try build([], repos: [Repository(id: "o/r", scannedAt: now, stale: true)])
        #expect(!stale.evidence.values.reliable)
    }
    @Test func relativeChangesAndUnknownLines() {
        var value = ComparisonValues(a: .init(commits: 2, added: 10), b: .init(commits: 1, added: 4))
        #expect(value.relative(.lines, a: 10, b: 4) == "+150.0%")
        #expect(value.relative(.commits, a: 1, b: 0) == "Activity only in A")
        #expect(value.relative(.commits, a: 0, b: 1) == "Activity only in B")
        #expect(value.relative(.commits, a: 0, b: 0) == "No activity in either")
        value.a.unknown = true
        #expect(value.relative(.lines, a: 10, b: 4) == "Incomplete evidence")
        #expect(value.relative(.commits, a: 2, b: 1) == "+100.0%")
    }
    @Test func timelineUsesHistoricalBoundsNotCurrentYear() throws {
        let c = HistoryComparison.preset(days: 7, now: date("2022-03-01T18:00:00Z"), calendar: calendar)
        let points = try ComparisonTimeline.points([commit("old", at: c.a.start), commit("outside", at: now)], period: c.a, snapshot: now, calendar: calendar)
        #expect(points.count == 7 && points.reduce(0) { $0 + $1.commits } == 1)
        #expect(points.first?.date == c.a.start)
    }
    @Test @MainActor func navigationRetainsComparisonAndResetsOnAccountChange() {
        let navigation = BoardState()
        navigation.days = 30; navigation.scope = .init(repo: "o/r")
        let c = HistoryComparison.preset(days: 14, now: now, calendar: calendar)
        navigation.applyComparison(c)
        #expect(navigation.comparisonActive && navigation.lens == .directories)
        navigation.explore(.init(repo: "o/r", path: "src/a", isFile: true))
        #expect(navigation.lens == .commits && navigation.comparison == c)
        navigation.applyComparison(c)
        #expect(navigation.lens == .commits)
        navigation.back()
        #expect(navigation.comparison == c && navigation.scope.path == "src")
        navigation.setLens(.heat)
        #expect(!navigation.comparisonActive && navigation.comparison == c)
        navigation.setLens(.directories); navigation.switchBoard(.contributors)
        #expect(!navigation.comparisonActive && navigation.days == 30)
        navigation.switchBoard(.activity)
        #expect(navigation.comparisonActive)
        navigation.exitComparison()
        #expect(navigation.comparison == nil && navigation.days == 30)
        navigation.applyComparison(c); navigation.resetForAccount(.init())
        #expect(navigation.comparison == nil)
    }
    @Test func canceledProjectionDoesNotPublish() async {
        let task = Task.detached { () throws -> ComparisonProjection in
            while !Task.isCancelled { await Task.yield() }
            return try build([])
        }
        task.cancel()
        do { _ = try await task.value; Issue.record("Canceled projection returned a result") } catch is CancellationError {} catch { Issue.record("Unexpected error: \(error)") }
    }
}

struct ComparisonIntegrationTests {
    let fixture = ComparisonTests()
    @Test @MainActor func markdownIncludesPeriodsUnitsCoverageAndSelectedPeopleEvidence() throws {
        let c = HistoryComparison.preset(days: 7, now: fixture.now, calendar: fixture.calendar)
        var a = fixture.commit("a", at: c.a.start, files: [.init(path: "src/a", added: 5)])
        a.linesUnknown = true
        let snapshot = fixture.snapshot([a, fixture.commit("b", at: c.b.start, email: "b@x", files: [.init(path: "old/b", added: 2)])])
        let navigation = BoardState(); navigation.applyComparison(c); navigation.comparisonRates = true
        let text = BoardMarkdown.render(snapshot: snapshot, configuration: .init(), navigation: navigation)
        #expect(text.contains("Timezone: America/Denver"))
        #expect(text.contains("Per elapsed 24 hours"))
        #expect(text.contains("Incomplete evidence"))
        #expect(text.contains("A − B") && text.contains("2026"))
        navigation.setLens(.people); navigation.scopedPerson = "b@x"; navigation.comparisonPeriodA = false
        let people = BoardMarkdown.render(snapshot: snapshot, configuration: .init(), navigation: navigation)
        #expect(people.contains("Evidence period: B") && people.contains("b@x"))
        #expect(!people.contains("/commit/a)"))
    }
    @Test func sortingUsesMagnitudeAndSupportsBaselineOnlyAndRateViews() throws {
        let c = HistoryComparison.preset(days: 7, now: fixture.now, calendar: fixture.calendar)
        let records = [fixture.commit("a", at: c.a.start, files: [.init(path: "a/one", added: 4)]), fixture.commit("b", at: c.b.start, files: [.init(path: "z/two", added: 20)])]
        let projection = try fixture.build(records, scope: .init(repo: "o/r"))
        func rows(_ order: ComparisonOrder, query: String = "") -> [ComparisonArea] { projection.sortedRows(order: order, measure: .lines, comparison: c, snapshot: fixture.now, rates: false, query: query) }
        #expect(rows(.difference).map(\.label) == ["z", "a"])
        #expect(rows(.a).map(\.label) == ["a", "z"])
        #expect(rows(.b).map(\.label) == ["z", "a"])
        #expect(rows(.name).map(\.label) == ["a", "z"])
        #expect(rows(.difference, query: "Z").map(\.label) == ["z"])
    }
    @Test func workAreasAndRepositorySelectionApplyToBothPeriods() throws {
        let c = HistoryComparison.preset(days: 7, now: fixture.now, calendar: fixture.calendar)
        let records = [fixture.commit("a", at: c.a.start, files: [.init(path: "src/a", added: 3), .init(path: "tools/b", added: 4), .init(path: "other/c", added: 90)]), fixture.commit("b", at: c.b.start, files: [.init(path: "tools/b", added: 10)]), fixture.commit("x", at: c.a.start, repo: "o/other", files: [.init(path: "src/a", added: 100)])]
        var config = WorkspaceConfiguration(); config.workAreas = ["o/r": [.init(name: "Engine", paths: ["src", "tools"])]]
        let p = try ComparisonProjection.build(snapshot: fixture.snapshot(records, repos: [Repository(id: "o/r", scannedAt: fixture.now), Repository(id: "o/other", scannedAt: fixture.now)]), configuration: config, comparison: c, repositories: ["o/r"], scope: .init(repo: "o/r", areaName: "Engine"), hideBots: false, people: [])
        #expect(p.evidence.values.a.added == 7 && p.evidence.values.b.added == 10)
        #expect(Set(p.rows.map(\.label)) == ["src", "tools"])
        #expect(!p.recordsA.contains { $0.repo == "o/other" })
    }
    @Test func missingEmailCanonicalPeopleRemainExplorableAcrossRepositoryCopies() throws {
        let c = HistoryComparison.preset(days: 7, now: fixture.now, calendar: fixture.calendar)
        let records = [fixture.commit("same", at: c.a.start, email: ""), fixture.commit("same", at: c.a.start, repo: "o/s", email: "")]
        let snapshot = fixture.snapshot(records, repos: [Repository(id: "o/r", scannedAt: fixture.now), Repository(id: "o/s", scannedAt: fixture.now)])
        let p = try ComparisonProjection.build(snapshot: snapshot, configuration: .init(), comparison: c, repositories: [], scope: .init(), hideBots: false, people: ["o/r:"])
        #expect(p.recordsA.count == 2)
        #expect(p.evidence.people.count == 1)
        #expect(Analysis.people(p.evidence.a, canonicalRecords: p.canonical).first?.records.count == 2)
    }
    @Test func largeProjectionRetainsAllEvidenceAndBoundedTimeline() throws {
        let c = HistoryComparison.preset(days: 14, now: fixture.now, calendar: fixture.calendar)
        let records = (0..<8000).map { i in fixture.commit("\(i)", at: (i % 2 == 0 ? c.a.start : c.b.start).addingTimeInterval(Double(i)), files: [.init(path: "src/area\(i % 20)/a.swift", added: 2)]) }
        let start = ContinuousClock.now
        let p = try ComparisonProjection.build(snapshot: fixture.snapshot(records), configuration: .init(), comparison: c, repositories: [], scope: .init(repo: "o/r", path: "src"), hideBots: false, people: [])
        #expect(p.evidence.values.a.commits == 4000 && p.evidence.values.b.commits == 4000)
        #expect(p.rows.count == 20)
        #expect(p.evidence.values.a.added == 8000)
        let points = try ComparisonTimeline.points(p.evidence.a, period: c.a, snapshot: fixture.now, calendar: fixture.calendar)
        #expect(points.count == 14)
        print("Comparison 8,000-commit / 20-area projection: \(start.duration(to: .now))")
    }
}
