import Foundation

struct HeatRange: Sendable {
    var current: DateInterval
    var includesStart: Bool
    var unit: Calendar.Component
    var calendar: Calendar

    init(
        days: Int = 14, since: Date? = nil, now: Date = Date(), records: [Commit] = [],
        calendar: Calendar = .current
    ) {
        self.calendar = calendar
        let start: Date
        if let since {
            start = min(since, now)
        } else if days > 0 {
            start = now.addingTimeInterval(-Double(days) * 86_400)
        } else {
            start = calendar.startOfDay(for: records.filter { $0.date <= now }.map(\.date).min() ?? now)
        }
        current = DateInterval(start: start, end: now)
        includesStart = since == nil && days == 0
        unit =
            current.duration <= 86_400
            ? .hour
            : current.duration <= 31 * 86_400
                ? .day
                : current.duration <= 180 * 86_400 ? .weekOfYear : .month
    }
    var unitLabel: String {
        switch unit {
        case .hour: "Hourly"
        case .day: "Daily"
        case .weekOfYear: "Weekly"
        default: "Monthly"
        }
    }
    var intervals: [DateInterval] {
        var result: [DateInterval] = []
        var start = current.start
        while start < current.end {
            guard let boundary = calendar.dateInterval(of: unit, for: start)?.end, boundary > start else {
                break
            }
            let end = min(boundary, current.end)
            result.append(DateInterval(start: start, end: end))
            start = end
        }
        return result
    }
    func contains(_ date: Date, in interval: DateInterval) -> Bool {
        (date > interval.start
            || (includesStart && interval.start == current.start && date == current.start))
            && date <= interval.end
    }
    static func label(_ interval: DateInterval) -> String {
        "\(interval.start.formatted(date: .abbreviated, time: .shortened)) – \(interval.end.formatted(date: .abbreviated, time: .shortened))"
    }
}
struct HeatPoint: Identifiable, Sendable {
    var interval: DateInterval
    var value: Int
    var includesStart = false
    var id: Date { interval.start }
}
struct HeatSeries: Sendable {
    var series: [HeatPoint]
    var total: Int
    var incomplete: Bool
    var future: Int
}
struct HeatRow: Identifiable, Sendable {
    var id: String
    var label: String
    var kind: String
    var scope: ActivityScope
    var heat: HeatSeries
}

extension Analysis {
    static func heatSeries(
        _ commits: [Commit], metric: String = "lines", range: HeatRange, complete: Bool = true
    ) -> HeatSeries {
        let records = unique(commits)
        func value(_ selected: [Commit]) -> Int {
            let totals = totals(selected)
            return metric == "people"
                ? totals.people : metric == "commits" ? totals.commits : totals.change
        }
        func selected(_ interval: DateInterval) -> [Commit] {
            records.filter { range.contains($0.date, in: interval) }
        }
        let current = selected(range.current)
        let incomplete =
            !complete
            || (metric == "lines" && current.contains(where: \.linesUnknown))
        let series = range.intervals.map { interval in
            return HeatPoint(
                interval: interval, value: value(selected(interval)),
                includesStart: range.includesStart && interval.start == range.current.start)
        }
        return HeatSeries(
            series: series, total: value(current),
            incomplete: incomplete, future: records.filter { $0.date > range.current.end }.count)
    }
    static func heatHistoryComplete(_ repositories: [Repository], records: [Commit]) -> Bool {
        !repositories.isEmpty
            && repositories.allSatisfy { $0.scannedAt != nil && !$0.stale && $0.error.isEmpty }
            && !records.contains(where: \.pathsUnknown)
    }
    static func heatRows(
        _ commits: [Commit], repositories: [Repository], scope: ActivityScope = ActivityScope(),
        workAreas: [String: [WorkArea]] = [:], metric: String = "lines", range: HeatRange
    ) -> [HeatRow] {
        let records = commits.filter { commit in
            range.contains(commit.date, in: range.current)
        }
        return activityRows(records, repositories: repositories, scope: scope, workAreas: workAreas)
            .map { row in
                let repos = repositories.filter { $0.id == row.scope.repo }
                let evidence = records.filter { $0.repo == row.scope.repo }
                return HeatRow(
                    id: row.id, label: row.label, kind: row.kind, scope: row.scope,
                    heat: heatSeries(
                        row.records, metric: metric, range: range,
                        complete: heatHistoryComplete(repos, records: evidence)))
            }
            .sorted {
                $0.heat.total != $1.heat.total
                    ? $0.heat.total > $1.heat.total : literalLess($0.label, $1.label)
            }
    }
}
