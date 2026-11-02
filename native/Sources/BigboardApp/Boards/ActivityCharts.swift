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
