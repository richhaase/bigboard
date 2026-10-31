import SwiftUI
import AppKit
import Charts

func formatted(_ count: Int) -> String { count.formatted() }
func openGitHub(_ raw: String) {
    guard let url = URL(string: raw), url.scheme == "https", url.host == "github.com", url.user == nil, url.password == nil, !raw.contains("\n"), !raw.contains("\r") else { return }
    NSWorkspace.shared.open(url)
}
struct DeltaView: View {
    var added: Int; var removed: Int; var unknown = false
    var body: some View { HStack(spacing: 5) { Text("+\(formatted(added))").foregroundStyle(.green); Text("−\(formatted(removed))").foregroundStyle(.red); if unknown { Text("?").help("Some line counts are unavailable") } }.font(.system(.callout, design: .monospaced)).monospacedDigit() }
}
struct StateBadge: View {
    var value: String
    var color: Color { ["SUCCESS", "APPROVED", "MERGEABLE"].contains(value) ? .green : ["FAILURE", "ERROR", "CHANGES_REQUESTED", "CONFLICTING"].contains(value) ? .orange : .secondary }
    var body: some View { Text(value.replacingOccurrences(of: "_", with: " ").localizedCapitalized).font(.caption).foregroundStyle(color).padding(.horizontal, 7).padding(.vertical, 3).background(color.opacity(0.1), in: Capsule()) }
}
struct EmptyBoard: View {
    var title: String; var message: String; var symbol = "tray"
    var body: some View { ContentUnavailableView(title, systemImage: symbol, description: Text(message)).frame(maxWidth: .infinity, maxHeight: .infinity) }
}
struct SectionTitle: View {
    var title: String; var detail = ""
    var body: some View { HStack { Text(title).font(.headline); Spacer(); Text(detail).font(.caption).foregroundStyle(.secondary) }.padding(.top, 12) }
}
struct MetricView: View {
    var title: String; var value: String
    var body: some View { VStack(alignment: .leading, spacing: 4) { Text(title).font(.caption).foregroundStyle(.secondary); Text(value).font(.title2).monospacedDigit() }.frame(maxWidth: .infinity, alignment: .leading) }
}
struct CommitList: View {
    var records: [Commit]
    var showFiles = false
    @State private var limit = 100
    var sorted: [Commit] { Analysis.unique(records).sorted { $0.date > $1.date } }
    var body: some View {
        if records.isEmpty { EmptyBoard(title: "No commits in this view", message: "Expand the date range or adjust the filters.") }
        else {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(Array(sorted.prefix(limit).enumerated()), id: \.offset) { _, commit in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(alignment: .top) {
                            Button { openGitHub("https://github.com/\(commit.repo)/commit/\(commit.id)") } label: { Text(commit.subject.isEmpty ? "Untitled commit" : commit.subject).multilineTextAlignment(.leading) }.buttonStyle(.plain).fontWeight(.medium)
                            Spacer(minLength: 12)
                            DeltaView(added: commit.added, removed: commit.removed, unknown: commit.linesUnknown)
                        }
                        HStack { Text(commit.author); Text(commit.date, format: .dateTime.year().month().day()); Text(String(commit.id.prefix(7))).fontDesign(.monospaced); if commit.ai { Text("AI assisted") }; if commit.bot { Text("Bot") } }.font(.caption).foregroundStyle(.secondary)
                        if showFiles { ForEach(commit.files.filter { $0.excludedReason.isEmpty }, id: \.path) { file in Text(file.path).font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled) } }
                    }.padding(.vertical, 10)
                    Divider()
                }
                if sorted.count > limit { Button("Show 100 more (\(sorted.count - limit) remaining)") { limit += 100 }.padding(.vertical) }
            }.onChange(of: records.count) { limit = 100 }
        }
    }
}
struct ActivityTimeline: View {
    var points: [TimelinePoint]
    var body: some View {
        Chart(points, id: \.date) { point in BarMark(x: .value("Date", point.date), y: .value("Commits", point.commits)).foregroundStyle(Color.accentColor.gradient) }
            .chartYAxis { AxisMarks(position: .leading) }
            .frame(height: 120)
            .accessibilityLabel("Commit activity timeline")
    }
}
struct ActivityCalendar: View {
    var records: [Commit]; var now: Date
    var points: [TimelinePoint] { Analysis.daily(records, days: 365, now: now) }
    var padding: Int { guard let first = points.first else { return 0 }; return (Calendar.current.component(.weekday, from: first.date) - Calendar.current.firstWeekday + 7) % 7 }
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Daily activity · past year").font(.caption).foregroundStyle(.secondary)
            ScrollView(.horizontal) {
                LazyHGrid(rows: Array(repeating: GridItem(.fixed(10), spacing: 3), count: 7), spacing: 3) {
                    ForEach(0..<padding, id: \.self) { _ in Color.clear.frame(width: 10, height: 10) }
                    ForEach(points, id: \.date) { point in ActivityCalendarCell(point: point) }
                }.frame(height: 88)
            }
        }.accessibilityLabel("Activity calendar for the latest year")
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
