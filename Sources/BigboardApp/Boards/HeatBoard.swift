import SwiftUI

enum HeatShade {
    static let steps = 4
    static func step(_ value: Int, maximum: Int) -> Int {
        guard value > 0, maximum > 0 else { return 0 }
        let fraction = (Double(min(value, maximum)) / Double(maximum)).squareRoot()
        return min(steps, max(1, Int((fraction * Double(steps)).rounded(.up))))
    }
    static func lowerBound(_ step: Int, maximum: Int) -> Int {
        let fraction = Double(step - 1) / Double(steps)
        return Int((Double(maximum) * fraction * fraction).rounded(.down)) + 1
    }
    static func color(_ step: Int) -> Color {
        step == 0 ? Color.secondary.opacity(0.1) : Color.blue.opacity(0.2 + Double(step) * 0.2)
    }
}

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
        let overallMaximum = current.series.map(\.value).max() ?? 0
        let maximum = rows.flatMap(\.heat.series).map(\.value).max() ?? 0
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                scopeBar
                HStack {
                    Text("Activity over time").font(.title2.bold())
                    Spacer()
                    PeopleFilter(records: snapshot.commits, filters: $navigation.heat)
                    Picker("Measure", selection: $navigation.heatMetric) {
                        Text("Lines").tag("lines")
                        Text("Commits").tag("commits")
                        Text("People").tag("people")
                    }
                    .frame(width: 160)
                    Toggle("Shared scale", isOn: $navigation.heatSharedScale)
                        .toggleStyle(.checkbox)
                        .help("Shade every row against the busiest row so rows can be compared")
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
                HeatStrip(series: current, metric: navigation.heatMetric, maximum: overallMaximum) {
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
                legend(maximum: navigation.heatSharedScale ? maximum : nil)
                Text(
                    navigation.heatSharedScale
                        ? "Every row below is shaded against the busiest cell of any row, so rows can be compared. The overall strip above has its own scale. Hover for counts; select a cell to explore it in Activity."
                        : "Each row below is shaded against its own busiest cell, so quiet areas still show their pattern; compare rows by their totals, or turn on Shared scale. The overall strip above has its own scale. Hover for counts; select a cell to explore it in Activity."
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
                            .contextMenu {
                                if !row.scope.isFile {
                                    Button("Explore \(row.label)", systemImage: "chevron.right") {
                                        navigation.exploreHeat(row.scope)
                                    }
                                }
                                Button("View in Activity", systemImage: "arrow.right") {
                                    navigation.showHeatActivity(
                                        row.scope, interval: range.current, includesStart: range.includesStart)
                                }
                                Divider()
                                ScopeLinkItems(scope: row.scope, workspace: workspace)
                            }
                            Spacer()
                            Text(
                                "\(row.heat.total.formatted()) \(navigation.heatMetric)\(row.heat.incomplete ? " (known)" : "")"
                                    + (navigation.heatSharedScale || rowMaximum(row) == 0
                                        ? "" : " · peak \(rowMaximum(row).formatted())")
                            )
                            .font(.callout.monospacedDigit()).foregroundStyle(.secondary)
                            Button("View in Activity", systemImage: "arrow.right") {
                                navigation.showHeatActivity(
                                    row.scope, interval: range.current, includesStart: range.includesStart)
                            }
                            .labelStyle(.iconOnly).buttonStyle(.borderless)
                            .help("View \(row.label) in Activity")
                        }
                        HeatStrip(
                            series: row.heat, metric: navigation.heatMetric,
                            maximum: navigation.heatSharedScale ? maximum : rowMaximum(row)
                        ) {
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
        .scrollEdgeEffectStyle(.hard, for: .top)
    }
    private func rowMaximum(_ row: HeatRow) -> Int {
        row.heat.series.map(\.value).max() ?? 0
    }
    private func legend(maximum: Int?) -> some View {
        HStack(alignment: .center, spacing: 10) {
            swatch(0, label: "None")
            ForEach(1...HeatShade.steps, id: \.self) { step in
                if let maximum, maximum > 0 {
                    let bound = HeatShade.lowerBound(step, maximum: maximum)
                    let previous = step == 1 ? 0 : HeatShade.lowerBound(step - 1, maximum: maximum)
                    swatch(step, label: bound == previous ? "" : "\(compact(bound))+")
                } else {
                    swatch(step, label: step == 1 ? "Low" : step == HeatShade.steps ? "Row peak" : "")
                }
            }
            if let maximum, maximum > 0 {
                Text("up to \(maximum.formatted()) \(navigation.heatMetric) per cell")
            }
        }
        .font(.caption).foregroundStyle(.secondary)
    }
    private func swatch(_ step: Int, label: String) -> some View {
        HStack(spacing: 4) {
            RoundedRectangle(cornerRadius: 2).fill(HeatShade.color(step)).frame(width: 18, height: 12)
            if !label.isEmpty { Text(label).monospacedDigit() }
        }
    }
    private func compact(_ value: Int) -> String {
        value.formatted(.number.notation(.compactName))
    }
    private func axisLabel(_ date: Date, range: HeatRange) -> String {
        date.formatted(date: .abbreviated, time: range.unit == .hour ? .shortened : .omitted)
    }
    private var scopeBar: some View {
        HStack {
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
        HeatShade.color(HeatShade.step(point.value, maximum: maximum))
    }
}
