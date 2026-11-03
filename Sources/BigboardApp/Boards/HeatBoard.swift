import SwiftUI

struct HeatBoard: View {
    var records: [Commit]
    var repositories: [Repository]
    var workAreas: [String: [WorkArea]]
    @Bindable var navigation: BoardState
    var now: Date
    var body: some View {
        let query = navigation.activity.query
        let rows =
            Analysis.heatRows(
                records,
                repositories: repositories,
                scope: navigation.scope,
                workAreas: workAreas,
                metric: navigation.heatMetric,
                now: now
            )
            .filter { query.isEmpty || $0.label.localizedCaseInsensitiveContains(query) }
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Change relative to usual").font(.title2.bold())
                Spacer()
                Picker("Measure", selection: $navigation.heatMetric) {
                    Text("Lines").tag("lines")
                    Text("Commits").tag("commits")
                    Text("People").tag("people")
                }
                .frame(width: 160)
            }
            Text(
                "Each week compares with that area's previous 12 weeks. The change badge compares the latest 4 weeks with the 12 before them. Heat keeps its own historical window."
            )
            .font(.callout).foregroundStyle(.secondary)
            let current = Analysis.heatSeries(
                Analysis.scoped(records, scope: navigation.scope, workAreas: workAreas),
                metric: navigation.heatMetric, now: now)
            HeatStrip(series: current)
            if current.future > 0 {
                Text("\(current.future) future-dated commits are outside the Heat window.").font(.caption)
                    .foregroundStyle(.secondary)
            }
            HStack {
                Text("Area")
                Spacer()
                Text("Last 26 weeks")
                Spacer()
                Text("Change")
            }
            .font(.caption).foregroundStyle(.secondary)
            ForEach(navigation.heatExpanded ? rows : Array(rows.prefix(25))) { row in
                Button {
                    navigation.explore(row.scope)
                } label: {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Label(row.label, systemImage: row.kind == "repository" ? "shippingbox" : "folder")
                                .lineLimit(1)
                            Spacer()
                            Text(changeLabel(row.heat)).font(.caption.bold())
                            Image(systemName: "chevron.right").font(.caption)
                        }
                        HeatStrip(series: row.heat)
                    }
                    .padding(.vertical, 6).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            if rows.isEmpty && !navigation.scope.repo.isEmpty {
                Button("Browse files in this area") { navigation.lens = .directories }.buttonStyle(.bordered)
            }
            if !navigation.heatExpanded && rows.count > 25 {
                Button(
                    "Show \(rows.count - 25) more areas (\(rows.dropFirst(25).filter { ["up", "new"].contains($0.heat.status) }.count) rising or new)"
                ) { navigation.heatExpanded = true }
            }
            HStack {
                Label("Quieter", systemImage: "square.fill").foregroundStyle(.blue)
                Text("Usual")
                Label("Busier", systemImage: "square.fill").foregroundStyle(.orange)
                Label("After quiet weeks", systemImage: "square.fill").foregroundStyle(.yellow)
            }
            .font(.caption)
        }
    }
    private func changeLabel(_ value: HeatSeries) -> String {
        if value.status == "new" { return "New" }
        if value.status == "quiet" { return "Quiet" }
        guard let ratio = value.ratio else { return "—" }
        return ratio < 0.1 ? "<0.1×" : String(format: "%.1f×", ratio)
    }
}
private struct HeatStrip: View {
    var series: HeatSeries
    var body: some View {
        HStack(spacing: 3) {
            ForEach(series.series) { point in
                RoundedRectangle(cornerRadius: 3).fill(color(point.level)).frame(maxWidth: .infinity)
                    .frame(height: 22)
                    .help(
                        "Week of \(point.start.formatted(date: .abbreviated, time: .omitted)): \(point.value.formatted()) · usual \(point.usual.formatted(.number.precision(.fractionLength(1))))"
                    )
                    .accessibilityLabel(
                        "\(point.start.formatted(date: .abbreviated, time: .omitted)): \(point.value.formatted()), baseline \(point.usual.formatted())"
                    )
            }
        }
    }
    private func color(_ level: String) -> Color {
        switch level {
        case "hot": .orange
        case "warm": .orange.opacity(0.55)
        case "cold": .blue
        case "cool": .blue.opacity(0.5)
        case "first": .yellow.opacity(0.5)
        case "steady": .secondary.opacity(0.28)
        default: .secondary.opacity(0.08)
        }
    }
}
