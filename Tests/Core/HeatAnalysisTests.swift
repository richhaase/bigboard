import Foundation
import Testing

@testable import Bigboard

struct HeatAnalysisTests {
    var calendar: Calendar {
        var value = Calendar(identifier: .gregorian)
        value.timeZone = TimeZone(identifier: "America/Denver")!
        value.firstWeekday = 2
        return value
    }
    var now: Date { calendar.date(from: DateComponents(year: 2026, month: 11, day: 4, hour: 12))! }
    func commit(_ id: String, at date: Date, lines: Int = 10, email: String = "alex@example.test")
        -> Commit
    {
        Commit(
            id: id, repo: "acme/app", author: "Alex", email: email, date: date, subject: id,
            added: lines, files: [FileChange(path: "Sources/App.swift", added: lines)])
    }
    @Test func selectedRangeExcludesOlderAndFutureActivityWithoutDuplicatingBoundaries() {
        let range = HeatRange(days: 1, now: now, calendar: calendar)
        let boundary = range.intervals[0].end
        let records = [
            commit("before", at: range.current.start.addingTimeInterval(-1)),
            commit("start", at: range.current.start), commit("boundary", at: boundary),
            commit("end", at: now), commit("future", at: now.addingTimeInterval(1)),
        ]
        let series = Analysis.heatSeries(records, metric: "commits", range: range)
        #expect(series.total == 2 && series.future == 1)
        #expect(series.series.reduce(0) { $0 + $1.value } == 2)
        #expect(series.series.first?.value == 1)
    }
    @Test(arguments: [(1, Calendar.Component.hour), (14, .day), (90, .weekOfYear), (365, .month)])
    func intervalsAdaptToTheSelectedRange(days: Int, unit: Calendar.Component) {
        let range = HeatRange(days: days, now: now, calendar: calendar)
        #expect(range.unit == unit)
        #expect(range.intervals.first?.start == range.current.start)
        #expect(range.intervals.last?.end == now)
        #expect(zip(range.intervals, range.intervals.dropFirst()).allSatisfy { $0.end == $1.start })
    }
    @Test func calendarBucketsRespectDaylightSavingAndFirstWeekday() {
        let range = HeatRange(days: 7, now: now, calendar: calendar)
        #expect(range.intervals.contains { $0.duration == 25 * 3600 })
        #expect(range.intervals.first!.duration < 86_400)
        #expect(range.intervals.last!.duration < 86_400)
        let weekly = HeatRange(days: 90, now: now, calendar: calendar)
        #expect(
            weekly.intervals.dropFirst().allSatisfy { calendar.component(.weekday, from: $0.start) == 2 })
        let monthly = HeatRange(days: 365, now: now, calendar: calendar)
        #expect(
            monthly.intervals.dropFirst().allSatisfy { calendar.component(.day, from: $0.start) == 1 })
    }
    @Test func cutoffAndAllTimeDefineTheDisplayedRange() {
        let cutoff = now.addingTimeInterval(-6 * 3600)
        let range = HeatRange(days: 90, since: cutoff, now: now, calendar: calendar)
        #expect(range.current.start == cutoff && range.unit == .hour)
        #expect(range.current.duration == 21_600.0)
        let midnight = calendar.startOfDay(for: now)
        let record = commit("first", at: midnight)
        let all = HeatRange(days: 0, now: now, records: [record], calendar: calendar)
        let series = Analysis.heatSeries([record], range: all)
        #expect(all.includesStart)
        #expect(series.total == 10)
        #expect(series.series.reduce(0) { $0 + $1.value } == 10)
        let futureCutoff = HeatRange(since: now.addingTimeInterval(60), now: now)
        #expect(futureCutoff.current.duration == 0 && futureCutoff.intervals.isEmpty)
    }
    @Test func wholeRangePeopleAreDistinctRatherThanSummedCells() {
        let range = HeatRange(days: 14, now: now, calendar: calendar)
        let first = commit("first", at: now.addingTimeInterval(-86_400))
        let second = commit("second", at: now.addingTimeInterval(-3 * 86_400))
        let prior = commit("prior", at: now.addingTimeInterval(-20 * 86_400), email: "sam@example.test")
        let series = Analysis.heatSeries([first, first, second, prior], metric: "people", range: range)
        #expect(series.total == 1)
        #expect(series.series.reduce(0) { $0 + $1.value } == 2)
        #expect(
            Analysis.heatSeries([first, first, second], metric: "commits", range: range).total == 2)
    }
    @Test func totalsAndCoverageDistinguishKnownActivityFromMissingEvidence() {
        let range = HeatRange(days: 14, now: now)
        let record = commit("new", at: now)
        let activity = Analysis.heatSeries([record], range: range)
        #expect(activity.total == 10 && !activity.incomplete)
        let quiet = Analysis.heatSeries(
            [commit("old", at: now.addingTimeInterval(-20 * 86_400))], range: range)
        #expect(quiet.total == 0 && !quiet.incomplete)
        let missing = Analysis.heatSeries([record], range: range, complete: false)
        #expect(missing.total == 10 && missing.incomplete)
        var unknown = record
        unknown.linesUnknown = true
        #expect(Analysis.heatSeries([unknown], range: range).incomplete)
        #expect(!Analysis.heatSeries([unknown], metric: "commits", range: range).incomplete)
    }
    @Test func rowsUseOnlySelectedActivityAndQualifyIncompleteRepositories() throws {
        let range = HeatRange(days: 14, now: now)
        let old = commit("old", at: now.addingTimeInterval(-20 * 86_400))
        let recent = commit("recent", at: now)
        let scanned = Repository(id: "acme/app", scannedAt: now)
        let rows = Analysis.heatRows(
            [old], repositories: [scanned], scope: ActivityScope(repo: scanned.id), range: range)
        #expect(rows.isEmpty)
        for repo in [
            Repository(id: scanned.id), Repository(id: scanned.id, scannedAt: now, stale: true),
            Repository(id: scanned.id, scannedAt: now, error: "Unavailable"),
        ] {
            let result = Analysis.heatRows([recent], repositories: [repo], range: range)
            #expect(result.first?.heat.total == 10)
            #expect(result.first?.heat.incomplete == true)
        }
        var unknown = commit("unknown", at: now)
        unknown.pathsUnknown = true
        let partial = Analysis.heatRows(
            [recent, unknown], repositories: [scanned], scope: ActivityScope(repo: scanned.id), range: range)
        #expect(partial.first?.heat.incomplete == true)
        let empty = Analysis.heatRows([], repositories: [scanned], range: range)
        #expect(empty.first?.heat.total == 0 && empty.first?.heat.incomplete == false)
    }
}
