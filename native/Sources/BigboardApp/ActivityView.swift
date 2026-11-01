import SwiftUI
import AppKit

struct ActivityBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    var records: [Commit] { navigation.filteredCommits(workspace.snapshot, heat: navigation.lens == .heat) }
    var scoped: [Commit] { navigation.evidenceRecords(Analysis.scoped(records, scope: navigation.scope, workAreas: workspace.configuration.workAreas), canonical: records) }
    var rows: [ActivityRow] { navigation.activityRows(workspace.snapshot, configuration: workspace.configuration) }
    var inspectScope: ActivityScope { rows.first { $0.id == navigation.activitySelection }?.scope ?? navigation.scope }
    var inspectRecords: [Commit] { navigation.evidenceRecords(Analysis.scoped(records, scope: inspectScope, workAreas: workspace.configuration.workAreas), canonical: records) }
    var title: String { navigation.scope.areaName.isEmpty ? navigation.scope.path.isEmpty ? navigation.scope.repo.isEmpty ? "All repositories" : navigation.scope.repo : navigation.scope.path : navigation.scope.areaName }
    var body: some View {
        VStack(spacing: 0) {
            if navigation.comparison != nil {
                HStack { Text("Comparison paused · Heat uses its own historical baseline.").font(.caption); Spacer(); Button("Resume comparison") { navigation.setLens(.directories) }; Button("Exit comparison") { navigation.exitComparison() } }.padding(.horizontal, 12).padding(.bottom, 8)
            }
            HStack {
                Button { navigation.back() } label: { Image(systemName: "chevron.left") }.disabled(navigation.scope.repo.isEmpty && navigation.lens == .heat)
                Button("Repositories") { navigation.explore(ActivityScope()) }.buttonStyle(.link)
                if !navigation.scope.repo.isEmpty { Text("/").foregroundStyle(.tertiary); Button(navigation.scope.repo) { navigation.explore(ActivityScope(repo: navigation.scope.repo)) }.buttonStyle(.link) }
                if !navigation.scope.path.isEmpty || !navigation.scope.areaName.isEmpty { Text("/").foregroundStyle(.tertiary); Text(navigation.scope.areaName.isEmpty ? navigation.scope.path : navigation.scope.areaName).lineLimit(1).truncationMode(.middle) }
                Spacer()
                if navigation.lens == .directories {
                    Picker("Sort", selection: $navigation.activity.sort) { Text("Lines changed").tag("change"); Text("Commits").tag("commits"); Text("Recent").tag("recent"); Text("Name").tag("name") }.frame(width: 175)
                    Button { navigation.activity.reversed.toggle() } label: { Image(systemName: navigation.activity.reversed ? "arrow.up" : "arrow.down") }
                }
            }.padding(.horizontal, 12).padding(.bottom, 10)
            Picker("Activity lens", selection: Binding(get: { navigation.lens }, set: { navigation.setLens($0) })) { ForEach(ActivityLens.allCases) { Text($0.rawValue).tag($0) } }.pickerStyle(.segmented).padding(.horizontal, 12).padding(.bottom, 12)
            HSplitView {
                Group {
                    if navigation.lens == .directories {
                        VStack(alignment: .leading, spacing: 0) { activitySummary.padding(16); if navigation.scope.isFile { ScrollView { CommitList(records: searched(scoped), showFiles: true).padding(16) } } else { directoryList } }
                    } else {
                        ScrollView {
                            VStack(alignment: .leading, spacing: 12) {
                                if navigation.lens == .heat { HeatBoard(records: records, repositories: navigation.visibleRepositories(workspace.snapshot), workAreas: workspace.configuration.workAreas, navigation: navigation, now: workspace.snapshot.generatedAt) }
                                else {
                                    activitySummary
                                    switch navigation.lens {
                                    case .commits: CommitList(records: searched(scoped), showFiles: navigation.scope.isFile)
                                    case .people: scopedPeople
                                    case .related: relatedList
                                    case .directories, .heat: EmptyView()
                                    }
                                }
                            }.padding(16)
                        }
                    }
                }.frame(minWidth: 450)
                inspector.frame(minWidth: 260, idealWidth: 310, maxWidth: 390)
            }
        }
    }
    private var activitySummary: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let evidence = navigation.relatedEvidence { Text(evidence.mode == "people" ? "Showing contributors shared with the previous area" : "Showing commits shared with the previous area").font(.caption).foregroundStyle(.secondary) }
            let total = Analysis.totals(scoped)
            HStack { MetricView(title: "Commits", value: formatted(total.commits)); MetricView(title: "Contributors", value: formatted(total.people)); MetricView(title: "Lines changed", value: formatted(total.change)) }
            if scoped.contains(where: \.pathsUnknown) { Text("Some commits have unknown paths and cannot be assigned to directories.").font(.caption).foregroundStyle(.orange) }
            let excluded = Analysis.excludedLines(scoped)
            if !excluded.isEmpty { Text("\(formatted(excluded.values.reduce(0,+))) generated, vendored, or bulk-data lines excluded.").font(.caption).foregroundStyle(.secondary) }
        }
    }
    private var directoryList: some View {
        Group {
            if rows.isEmpty { EmptyBoard(title: "No paths match", message: "Try a broader scope or another date range.") }
            else {
                List(rows, selection: $navigation.activitySelection) { row in
                    HStack {
                        Image(systemName: row.kind == "repository" ? "shippingbox" : row.kind == "directory" ? "folder" : "doc.text").foregroundStyle(.secondary)
                        VStack(alignment: .leading) { Text(row.label).lineLimit(1); Text("\(row.totals.commits) commits · \(row.totals.people) contributors").font(.caption).foregroundStyle(.secondary) }
                        Spacer()
                        DeltaView(added: row.totals.added, removed: row.totals.removed, unknown: row.totals.unknown)
                        Button { navigation.explore(row.scope) } label: { Image(systemName: "chevron.right") }.buttonStyle(.borderless).help("Explore \(row.label)")
                    }.padding(.vertical, 8).contentShape(Rectangle()).tag(row.id)
                    .onTapGesture(count: 2) { navigation.explore(row.scope) }
                }
                .onKeyPress(.rightArrow) { exploreSelected() }
                .onKeyPress(.return) { exploreSelected() }
            }
        }
    }
    private func exploreSelected() -> KeyPress.Result {
        guard let row = rows.first(where: { $0.id == navigation.activitySelection }) else { return .ignored }
        navigation.explore(row.scope); return .handled
    }
    private var scopedPeople: some View {
        VStack(alignment: .leading) {
            if let person = navigation.scopedPerson {
                Button("Back to people") { navigation.scopedPerson = nil }
                CommitList(records: searched(Analysis.people(scoped, canonicalRecords: records).first { $0.id == person }?.records ?? []))
            } else {
                ForEach(Analysis.people(scoped, canonicalRecords: records).filter { navigation.activity.query.isEmpty || "\($0.name) \($0.email)".localizedCaseInsensitiveContains(navigation.activity.query) }) { person in
                    Button { navigation.scopedPerson = person.id } label: { HStack { Image(systemName: "person.crop.circle"); VStack(alignment: .leading) { Text(person.name); Text("\(person.totals.commits) commits").font(.caption).foregroundStyle(.secondary) }; Spacer(); DeltaView(added: person.totals.added, removed: person.totals.removed) }.padding(.vertical, 8) }.buttonStyle(.plain)
                    Divider()
                }
            }
        }
    }
    private var relatedList: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Related by", selection: $navigation.relatedMode) { Text("Shared commits").tag("commits"); Text("Contributor overlap").tag("people") }.pickerStyle(.segmented)
            Text("Historical overlap describes related work, not ownership or current presence.").font(.caption).foregroundStyle(.secondary)
            let related = Analysis.relatedScopes(records, scope: navigation.scope, mode: navigation.relatedMode, workAreas: workspace.configuration.workAreas).filter { navigation.activity.query.isEmpty || "\($0.scope.repo) \($0.scope.path) \($0.scope.areaName)".localizedCaseInsensitiveContains(navigation.activity.query) }
            if related.isEmpty { EmptyBoard(title: "No related evidence", message: "Choose a repository or directory with activity.") }
            ForEach(related) { row in
                DisclosureGroup {
                    if navigation.relatedMode == "people" { ForEach(Analysis.people(row.records), id: \.id) { Text($0.name).font(.callout) } }
                    else { CommitList(records: row.records) }
                    Button("Explore matching work") { navigation.exploreRelated(row) }
                } label: { HStack { Text(row.scope.areaName.isEmpty ? "\(row.scope.repo) / \(row.scope.path)" : "\(row.scope.repo) / \(row.scope.areaName)"); Spacer(); Text("\(row.count) \(navigation.relatedMode == "people" ? "people" : "commits")").foregroundStyle(.secondary) } }.padding(.vertical, 5)
            }
        }
    }
    private var inspector: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Text(!inspectScope.areaName.isEmpty ? inspectScope.areaName : inspectScope.path.isEmpty ? inspectScope.repo.isEmpty ? "Selection" : inspectScope.repo : inspectScope.path).font(.title3.bold()).textSelection(.enabled)
                Text(navigation.lens == .heat ? "All available history" : navigation.rangeLabel).font(.caption).foregroundStyle(.secondary)
                let total = Analysis.totals(inspectRecords)
                DeltaView(added: total.added, removed: total.removed, unknown: total.unknown)
                Text("\(total.commits) commits").foregroundStyle(.secondary)
                SectionTitle(title: "Contributors")
                ForEach(Array(Analysis.people(inspectRecords, canonicalRecords: records).prefix(8))) { person in
                    Button { navigation.contributorSelection = person.id; navigation.board = .contributors } label: { HStack { Text(person.name).lineLimit(1); Spacer(); Text("\(person.totals.commits)").foregroundStyle(.secondary) } }.buttonStyle(.plain)
                }
                SectionTitle(title: "Recent activity")
                ActivityTimeline(points: navigation.timelinePoints(inspectRecords, snapshot: workspace.snapshot, heat: navigation.lens == .heat))
                SectionTitle(title: "Open pull requests")
                let related = relatedPRs
                if workspace.snapshot.cachedOnly { Text("PR context is unavailable offline.").font(.caption).foregroundStyle(.secondary) }
                else if workspace.snapshot.repositories.contains(where: { $0.id == inspectScope.repo && (!$0.prInventoryComplete || $0.prStale || ((!inspectScope.path.isEmpty || !inspectScope.areaName.isEmpty) && !$0.prFilesComplete)) }) { Text("Evidence is incomplete or stale; this list may omit matching PRs.").font(.caption).foregroundStyle(.orange) }
                ForEach(related.prefix(5)) { pr in Button { navigation.showPRs(repo: pr.repo, selected: pr.id) } label: { Text("#\(pr.number) \(pr.title)").multilineTextAlignment(.leading) }.buttonStyle(.link) }
                if !inspectScope.repo.isEmpty {
                    Button("View repository PRs") { navigation.showPRs(repo: inspectScope.repo) }
                    Button(inspectScope.path.isEmpty ? "Open repository on GitHub" : "Open path on GitHub", systemImage: "arrow.up.right") { Task { do { let url = try await workspace.openPath(inspectScope); NSWorkspace.shared.open(url) } catch { workspace.error = error.localizedDescription } } }
                }
            }.padding(16)
        }
    }
    private var relatedPRs: [PullRequest] {
        Analysis.relatedPRs(workspace.snapshot.pullRequests, scope: inspectScope, workAreas: workspace.configuration.workAreas).filter { navigation.dateMatches($0.updatedAt, snapshot: workspace.snapshot) }
    }
    private func searched(_ records: [Commit]) -> [Commit] { records.filter { navigation.activity.query.isEmpty || "\($0.subject) \($0.author) \($0.email)".localizedCaseInsensitiveContains(navigation.activity.query) } }
}
struct HeatBoard: View {
    var records: [Commit]; var repositories: [Repository]; var workAreas: [String: [WorkArea]]
    @Bindable var navigation: BoardState
    var now: Date
    var rows: [HeatRow] { Analysis.heatRows(records, repositories: repositories, scope: navigation.scope, workAreas: workAreas, metric: navigation.heatMetric, now: now).filter { navigation.activity.query.isEmpty || $0.label.localizedCaseInsensitiveContains(navigation.activity.query) } }
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack { Text("Change relative to usual").font(.title2.bold()); Spacer(); Picker("Measure", selection: $navigation.heatMetric) { Text("Lines").tag("lines"); Text("Commits").tag("commits"); Text("People").tag("people") }.frame(width: 160) }
            Text("Each week compares with that area's previous 12 weeks. The change badge compares the latest 4 weeks with the 12 before them. Heat keeps its own historical window.").font(.callout).foregroundStyle(.secondary)
            let current = Analysis.heatSeries(Analysis.scoped(records, scope: navigation.scope, workAreas: workAreas), metric: navigation.heatMetric, now: now)
            HeatStrip(series: current)
            if current.future > 0 { Text("\(current.future) future-dated commits are outside the Heat window.").font(.caption).foregroundStyle(.secondary) }
            HStack { Text("Area"); Spacer(); Text("Last 26 weeks"); Spacer(); Text("Change") }.font(.caption).foregroundStyle(.secondary)
            ForEach(navigation.heatExpanded ? rows : Array(rows.prefix(25))) { row in
                Button { navigation.explore(row.scope) } label: {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack { Label(row.label, systemImage: row.kind == "repository" ? "shippingbox" : "folder").lineLimit(1); Spacer(); Text(changeLabel(row.heat)).font(.caption.bold()); Image(systemName: "chevron.right").font(.caption) }
                        HeatStrip(series: row.heat)
                    }.padding(.vertical, 6).contentShape(Rectangle())
                }.buttonStyle(.plain)
            }
            if rows.isEmpty && !navigation.scope.repo.isEmpty { Button("Browse files in this area") { navigation.lens = .directories }.buttonStyle(.bordered) }
            if !navigation.heatExpanded && rows.count > 25 { Button("Show \(rows.count - 25) more areas (\(rows.dropFirst(25).filter { ["up", "new"].contains($0.heat.status) }.count) rising or new)") { navigation.heatExpanded = true } }
            HStack { Label("Quieter", systemImage: "square.fill").foregroundStyle(.blue); Text("Usual"); Label("Busier", systemImage: "square.fill").foregroundStyle(.orange); Label("After quiet weeks", systemImage: "square.fill").foregroundStyle(.yellow) }.font(.caption)
        }
    }
    private func changeLabel(_ value: HeatSeries) -> String { if value.status == "new" { return "New" }; if value.status == "quiet" { return "Quiet" }; guard let ratio = value.ratio else { return "—" }; return ratio < 0.1 ? "<0.1×" : String(format: "%.1f×", ratio) }
}
struct HeatStrip: View {
    var series: HeatSeries
    var body: some View {
        HStack(spacing: 3) { ForEach(series.series) { point in RoundedRectangle(cornerRadius: 3).fill(color(point.level)).frame(maxWidth: .infinity).frame(height: 22).help("Week of \(point.start.formatted(date: .abbreviated, time: .omitted)): \(point.value.formatted()) · usual \(point.usual.formatted(.number.precision(.fractionLength(1))))").accessibilityLabel("\(point.start.formatted(date: .abbreviated, time: .omitted)): \(point.value.formatted()), baseline \(point.usual.formatted())") } }
    }
    private func color(_ level: String) -> Color { switch level { case "hot": .orange; case "warm": .orange.opacity(0.55); case "cold": .blue; case "cool": .blue.opacity(0.5); case "first": .yellow.opacity(0.5); case "steady": .secondary.opacity(0.28); default: .secondary.opacity(0.08) } }
}
