import SwiftUI
import Charts

struct ActivityTimeline: View {
    var points: [TimelinePoint]
    var body: some View {
        Chart(points, id: \.date) { point in
            BarMark(x: .value("Date", point.date), y: .value("Commits", point.commits))
                .foregroundStyle(Color.accentColor.gradient)
        }
        .chartYAxis { AxisMarks(position: .leading) }
        .frame(height: 120)
        .accessibilityLabel("Commit activity timeline")
    }
}
struct WeeklyPRTrends: View {
    var points: [ActivityPRTrendWeek]

    var body: some View {
        if let first = points.first, let last = points.last {
            let halfWeek: TimeInterval = 3.5 * 86_400
            let domain = first.size.weekStart.addingTimeInterval(-halfWeek)...last.size.weekStart.addingTimeInterval(halfWeek)
            VStack(alignment: .leading, spacing: 16) {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Merged PRs").font(.headline)
                    mergeChart.trendAxes(domain: domain)
                        .frame(height: 126)
                        .accessibilityLabel("Weekly merged pull requests")
                    if points.contains(where: { $0.hasMergeCount && $0.coverage != .complete }) {
                        Text("Lighter bars show known merges in weeks with incomplete coverage.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text("Lines changed per PR").font(.headline)
                    if points.contains(where: { $0.size.sizedCount > 0 }) {
                        sizeChart.trendAxes(domain: domain)
                            .frame(height: 148)
                            .accessibilityLabel("Weekly median and 90th-percentile lines changed per pull request")
                    } else {
                        Text("No PR sizes available in this range.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                VStack(alignment: .leading, spacing: 6) {
                    Text("Total lines in merged PRs").font(.headline)
                    if points.contains(where: \.hasLineTotals) {
                        lineTotalsChart.trendAxes(domain: domain)
                            .frame(height: 148)
                            .accessibilityLabel("Weekly lines added, deleted, and net change")
                    } else {
                        Text("No line totals available in this range.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                DisclosureGroup("Weekly values") {
                    LazyVStack(alignment: .leading, spacing: 10) {
                        ForEach(points.reversed()) { week in
                            Divider()
                            weekDetails(week)
                        }
                    }
                    .padding(.top, 6)
                }
            }
        }
    }

    private var mergeChart: some View {
        Chart(points) { week in
            if week.hasMergeCount {
                if week.size.mergedCount == 0 {
                    PointMark(x: .value("Week", week.size.weekStart), y: .value("Merged PRs", 0))
                        .foregroundStyle(Color.accentColor)
                        .symbolSize(22)
                } else {
                    BarMark(
                        x: .value("Week", week.size.weekStart),
                        y: .value("Merged PRs", week.size.mergedCount), width: .ratio(0.65)
                    )
                    .foregroundStyle(Color.accentColor.opacity(week.coverage == .complete ? 1 : 0.5))
                }
            }
        }
        .chartYScale(domain: 0...max(1, points.map(\.size.mergedCount).max() ?? 0))
    }

    private var sizeChart: some View {
        Chart(points) { week in
            if let p90 = week.size.p90ChangedLines {
                PointMark(x: .value("Week", week.size.weekStart), y: .value("Changed lines", p90))
                    .foregroundStyle(by: .value("Percentile", "90th percentile"))
                    .symbol(by: .value("Percentile", "90th percentile"))
                    .symbolSize(85)
            }
            if let median = week.size.medianChangedLines {
                PointMark(x: .value("Week", week.size.weekStart), y: .value("Changed lines", median))
                    .foregroundStyle(by: .value("Percentile", "Median"))
                    .symbol(by: .value("Percentile", "Median"))
                    .symbolSize(28)
            }
        }
        .chartForegroundStyleScale(["Median": Color.blue, "90th percentile": Color.purple])
        .chartSymbolScale(["Median": .circle, "90th percentile": .diamond])
        .chartLegend(position: .bottom, alignment: .leading)
    }

    private var lineTotalsChart: some View {
        Chart {
            RuleMark(y: .value("Zero", 0)).foregroundStyle(.secondary.opacity(0.35))
                .accessibilityHidden(true)
            ForEach(points) { week in
                if week.hasLineTotals {
                    BarMark(
                        x: .value("Week", week.size.weekStart),
                        y: .value("Lines", week.size.additions), width: .ratio(0.7)
                    )
                    .foregroundStyle(by: .value("Measure", "Added"))
                    .position(by: .value("Line type", "Added"), axis: .horizontal)
                    BarMark(
                        x: .value("Week", week.size.weekStart),
                        y: .value("Lines", week.size.deletions), width: .ratio(0.7)
                    )
                    .foregroundStyle(by: .value("Measure", "Deleted"))
                    .position(by: .value("Line type", "Deleted"), axis: .horizontal)
                    PointMark(x: .value("Week", week.size.weekStart), y: .value("Lines", week.size.net))
                        .foregroundStyle(by: .value("Measure", "Net"))
                        .symbol(.diamond)
                        .symbolSize(40)
                }
            }
        }
        .chartForegroundStyleScale(["Added": Color.teal, "Deleted": Color.orange, "Net": Color.purple])
        .chartLegend(position: .bottom, alignment: .leading)
    }

    private func weekDetails(_ week: ActivityPRTrendWeek) -> some View {
        let size = week.size
        let date = size.weekStart.formatted(date: .abbreviated, time: .omitted)
        let count = week.hasMergeCount ? week.coverage.label(size.mergedCount) : "Unavailable"
        let median = size.medianChangedLines?.formatted(.number.precision(.fractionLength(0...1))) ?? "Unavailable"
        let p90 = size.p90ChangedLines?.formatted() ?? "Unavailable"
        return VStack(alignment: .leading, spacing: 3) {
            Text(size.isPartial ? "Week of \(date) (partial week)" : "Week of \(date)").font(.subheadline.bold())
            Text("Merged PRs \(count) · PRs with size data \(size.sizedCount.formatted())")
            Text("Changed lines per PR · Median \(median) · 90th percentile \(p90)")
            if week.hasLineTotals {
                let net = size.net.formatted(.number.sign(strategy: .always(includingZero: false)))
                Text("Lines added \(size.additions.formatted()) · Deleted \(size.deletions.formatted()) · Net \(net)")
            } else {
                Text("Line totals unavailable")
            }
        }
        .font(.caption)
        .accessibilityElement(children: .combine)
    }
}
private extension View {
    func trendAxes(domain: ClosedRange<Date>) -> some View {
        let spansMonths = domain.upperBound.timeIntervalSince(domain.lowerBound) > 182 * 86_400
        return chartXScale(domain: domain)
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 5)) { value in
                    AxisGridLine()
                    AxisTick()
                    AxisValueLabel {
                        if let date = value.as(Date.self) {
                            if spansMonths {
                                Text(date, format: .dateTime.month(.abbreviated).year())
                            } else {
                                Text(date, format: .dateTime.month(.abbreviated).day())
                            }
                        }
                    }
                }
            }
            .chartYAxis {
                AxisMarks(position: .leading, values: .automatic(desiredCount: 4)) { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let number = value.as(Double.self) ?? value.as(Int.self).map(Double.init) {
                            Text(number, format: .number.notation(.compactName))
                                .frame(width: 45, alignment: .trailing)
                        }
                    }
                }
            }
    }
}
struct ActivityCalendar: View {
    var records: [Commit]
    var days: Int
    var since: Date?
    var now: Date
    var rangeLabel: String
    @Environment(\.calendar) private var calendar

    var body: some View {
        let points = Analysis.daily(records, days: days, since: since, now: now, calendar: calendar)
        let padding =
            points.first.map { (calendar.component(.weekday, from: $0.date) - calendar.firstWeekday + 7) % 7 }
            ?? 0
        VStack(alignment: .leading, spacing: 6) {
            Text("Daily activity · \(rangeLabel)").font(.caption).foregroundStyle(.secondary)
            ScrollView(.horizontal) {
                LazyHGrid(rows: Array(repeating: GridItem(.fixed(10), spacing: 3), count: 7), spacing: 3) {
                    ForEach(0..<padding, id: \.self) { _ in Color.clear.frame(width: 10, height: 10) }
                    ForEach(points, id: \.date) { point in ActivityCalendarCell(point: point) }
                }
                .frame(height: 88)
            }
        }
        .accessibilityLabel("Activity calendar · \(rangeLabel)")
    }
}
private struct ActivityCalendarCell: View {
    var point: TimelinePoint
    private var color: Color {
        if point.commits == 0 { return Color.secondary.opacity(0.1) }
        let intensity: Double = min(1.0, 0.25 + Double(point.commits) / 12.0)
        return Color.accentColor.opacity(intensity)
    }
    var body: some View {
        let date: String = point.date.formatted(date: .abbreviated, time: .omitted)
        let description: String = "\(date): \(point.commits) commits"
        return RoundedRectangle(cornerRadius: 2)
            .fill(color)
            .frame(width: 10, height: 10)
            .help(Text(verbatim: description))
            .accessibilityLabel(Text(verbatim: description))
    }
}
