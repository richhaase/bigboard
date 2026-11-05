import SwiftUI

struct HeatBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @Environment(\.calendar) private var calendar

    var body: some View {
        let snapshot = workspace.snapshot
        let range = navigation.heatRange(snapshot, calendar: calendar)
        let records = navigation.heatRecords(snapshot)
        let repositories = navigation.visibleRepositories(snapshot)
        let scope = navigation.heatScope
        let scoped = Analysis.scoped(
            records, scope: scope, workAreas: workspace.configuration.workAreas)
        let scopeRepositories = repositories.filter { scope.repo.isEmpty || $0.id == scope.repo }
        let evidence = records.filter { commit in
            (scope.repo.isEmpty || commit.repo == scope.repo)
                && range.contains(commit.date, in: range.current)
        }
        let current = Analysis.heatSeries(
            scoped, metric: navigation.heatMetric, range: range,
            complete: Analysis.heatHistoryComplete(scopeRepositories, records: evidence))
        let rows = Analysis.heatRows(
            records, repositories: repositories, scope: scope,
            workAreas: workspace.configuration.workAreas, metric: navigation.heatMetric, range: range
        )
        .filter {
            navigation.heat.query.isEmpty
                || $0.label.localizedCaseInsensitiveContains(navigation.heat.query)
        }
        let maximum = ([current] + rows.map(\.heat)).flatMap(\.series).map(\.value).max() ?? 0
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                scopeBar
                HStack {
                    Text("Activity over time").font(.title2.bold())
                    Spacer()
                    Picker("Measure", selection: $navigation.heatMetric) {
                        Text("Lines").tag("lines")
                        Text("Commits").tag("commits")
                        Text("People").tag("people")
                    }
                    .frame(width: 160)
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text("Selected: \(HeatRange.label(range.current))")
                    Text("\(range.unitLabel) activity. Edge cells may cover part of an interval.")
                }
                .font(.callout).foregroundStyle(.secondary)
                Text("\(current.total.formatted()) \(navigation.heatMetric) in this range")
                    .font(.title3.monospacedDigit())
                if current.incomplete || rows.contains(where: { $0.heat.incomplete }) {
                    Text(
                        "Some history is unavailable, incomplete, or stale. Affected cells and totals show known activity only."
                    )
                    .font(.callout).foregroundStyle(.secondary)
                }
                if current.future > 0 {
                    Text("\(current.future) future-dated commits are outside the selected range.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                HeatStrip(series: current, metric: navigation.heatMetric, maximum: maximum) {
                    interval, includesStart in
                    navigation.showHeatActivity(
                        scope, interval: interval, includesStart: includesStart)
                }
                HStack {
                    Text(axisLabel(range.current.start, range: range))
                    Spacer()
                    Text(range.unitLabel)
                    Spacer()
                    Text(axisLabel(range.current.end, range: range))
                }
                .font(.caption).foregroundStyle(.secondary)
                HStack(spacing: 8) {
                    Text("0")
                    ForEach(0..<5) { step in
                        RoundedRectangle(cornerRadius: 2)
                            .fill(
                                step == 0
                                    ? Color.secondary.opacity(0.1)
                                    : Color.blue.opacity(0.2 + Double(step) * 0.2)
                            )
                            .frame(width: 18, height: 12)
                    }
                    Text("\(maximum.formatted()) \(navigation.heatMetric) per cell")
                }
                .font(.caption).foregroundStyle(.secondary)
                Text(
                    "Darker cells mean more activity. All rows use the same scale. Hover for counts; select a cell to explore its activity by directory."
                )
                .font(.caption).foregroundStyle(.secondary)
                Divider()
                ForEach(navigation.heatExpanded ? rows : Array(rows.prefix(25))) { row in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Button {
                                if row.scope.isFile {
                                    navigation.showHeatActivity(
                                        row.scope, interval: range.current, includesStart: range.includesStart
                                    )
                                } else {
                                    navigation.exploreHeat(row.scope)
                                }
                            } label: {
                                Label(
                                    row.label,
                                    systemImage: row.kind == "repository"
                                        ? "shippingbox" : row.scope.isFile ? "doc.text" : "folder"
                                )
                                .lineLimit(1)
                            }
                            .buttonStyle(.link)
                            Spacer()
                            Text(
                                "\(row.heat.total.formatted()) \(navigation.heatMetric)\(row.heat.incomplete ? " (known)" : "")"
                            )
                            .font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                            Button("View in Activity", systemImage: "arrow.right") {
                                navigation.showHeatActivity(
                                    row.scope, interval: range.current, includesStart: range.includesStart)
                            }
                            .labelStyle(.iconOnly).buttonStyle(.borderless)
                            .help("View \(row.label) in Activity")
                        }
                        HeatStrip(series: row.heat, metric: navigation.heatMetric, maximum: maximum) {
                            interval, includesStart in
                            navigation.showHeatActivity(
                                row.scope, interval: interval, includesStart: includesStart)
                        }
                    }
                    .padding(.vertical, 4)
                }
                if rows.isEmpty {
                    Text("No areas match this range and search.").foregroundStyle(.secondary)
                }
                if !navigation.heatExpanded && rows.count > 25 {
                    Button("Show \(rows.count - 25) more areas") { navigation.heatExpanded = true }
                }
            }
            .padding(16)
        }
    }
    private func axisLabel(_ date: Date, range: HeatRange) -> String {
        date.formatted(date: .abbreviated, time: range.unit == .hour ? .shortened : .omitted)
    }
    private var scopeBar: some View {
        HStack {
            Button {
                navigation.backInHeat()
            } label: {
                Image(systemName: "chevron.left")
            }
            .disabled(navigation.heatScope.repo.isEmpty)
            .help("Back in Heat Maps")
            Button("All repositories") { navigation.exploreHeat(ActivityScope()) }
                .buttonStyle(.link)
            if !navigation.heatScope.repo.isEmpty {
                Text("/").foregroundStyle(.tertiary)
                Button(navigation.heatScope.repo) {
                    navigation.exploreHeat(ActivityScope(repo: navigation.heatScope.repo))
                }
                .buttonStyle(.link)
            }
            if !navigation.heatScope.path.isEmpty || !navigation.heatScope.areaName.isEmpty {
                Text("/").foregroundStyle(.tertiary)
                Text(
                    navigation.heatScope.areaName.isEmpty
                        ? navigation.heatScope.path : navigation.heatScope.areaName
                )
                .lineLimit(1).truncationMode(.middle)
            }
            Spacer()
        }
    }
}

private struct HeatStrip: View {
    var series: HeatSeries
    var metric: String
    var maximum: Int
    var explore: (DateInterval, Bool) -> Void

    var body: some View {
        HStack(spacing: 3) {
            ForEach(series.series) { point in
                Button {
                    explore(point.interval, point.includesStart)
                } label: {
                    RoundedRectangle(cornerRadius: 3)
                        .fill(color(point))
                        .overlay {
                            if series.incomplete {
                                RoundedRectangle(cornerRadius: 3)
                                    .strokeBorder(
                                        .secondary.opacity(0.4), style: StrokeStyle(lineWidth: 1, dash: [2]))
                            }
                        }
                        .frame(maxWidth: .infinity).frame(height: 24)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(description(point))
                .accessibilityLabel(description(point))

            }
        }
    }
    private func description(_ point: HeatPoint) -> String {
        "\(HeatRange.label(point.interval)): \(point.value.formatted()) \(metric)"
            + (series.incomplete ? "\nKnown activity only; history is incomplete or stale." : "")
    }
    private func color(_ point: HeatPoint) -> Color {
        guard point.value > 0, maximum > 0 else { return .secondary.opacity(0.1) }
        return .blue.opacity(0.2 + 0.8 * Double(point.value) / Double(maximum))
    }
}
