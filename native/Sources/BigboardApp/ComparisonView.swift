import SwiftUI
import AppKit

struct ComparisonPanel: View {
    var snapshot: Snapshot
    @Bindable var navigation: BoardState
    @Environment(\.dismiss) private var dismiss
    @State private var draft = HistoryComparison.preset(days: 14, now: Date())
    private var calendar: Calendar { var value = Calendar(identifier: .gregorian); value.timeZone = draft.timeZone; return value }
    private func dateBinding(a: Bool, end: Bool) -> Binding<Date> {
        Binding(get: { let period = a ? draft.a : draft.b; return end ? period.end.addingTimeInterval(-1) : period.start }, set: { value in
            var period = a ? draft.a : draft.b
            if end { period.end = calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: value))! }
            else { period.start = calendar.startOfDay(for: value) }
            if a { draft.a = period } else { draft.b = period }
        })
    }
    var body: some View {
        Form {
            Text("Compare Git history in Activity. Dates include the selected end day.").foregroundStyle(.secondary)
            HStack {
                Text("Last complete days")
                ForEach([7, 14, 30, 90, 365], id: \.self) { days in Button("\(days)") { draft = .preset(days: days, now: snapshot.generatedAt, calendar: calendar) } }
            }
            Section("Period A") {
                DatePicker("Start A", selection: dateBinding(a: true, end: false), displayedComponents: .date)
                DatePicker("End A", selection: dateBinding(a: true, end: true), displayedComponents: .date)
            }
            Section("Period B") {
                DatePicker("Start B", selection: dateBinding(a: false, end: false), displayedComponents: .date)
                DatePicker("End B", selection: dateBinding(a: false, end: true), displayedComponents: .date)
                HStack {
                    Button("Previous equivalent period") { draft.b = HistoryComparison.previous(draft.a, calendar: calendar) }.disabled(draft.a.start >= draft.a.end)
                    Button("Swap periods") { let a = draft.a; draft.a = draft.b; draft.b = a }
                }
            }
            Text("Timezone: \(draft.timeZoneID) · Snapshot: \(snapshot.generatedAt.formatted(date: .abbreviated, time: .shortened))").font(.caption)
            if draft.overlaps { Label("Periods overlap; shared dates count independently in each period.", systemImage: "info.circle").foregroundStyle(.secondary) }
            if draft.a.partial(at: snapshot.generatedAt) || draft.b.partial(at: snapshot.generatedAt) { Text("A period includes a partial day, ending at the snapshot time.").foregroundStyle(.orange) }
            if let error = draft.validation(at: snapshot.generatedAt) { Text(error).foregroundStyle(.red) }
            HStack { Spacer(); Button("Cancel") { dismiss() }; Button("Apply comparison") { navigation.applyComparison(draft); dismiss() }.buttonStyle(.borderedProminent).disabled(draft.validation(at: snapshot.generatedAt) != nil) }
        }.formStyle(.grouped).environment(\.timeZone, draft.timeZone)
        .onAppear { draft = navigation.comparison ?? .preset(days: navigation.days > 0 && !navigation.sinceLastLook ? navigation.days : 14, now: snapshot.generatedAt) }
    }
}
struct ComparisonRequest: Equatable {
    var snapshotTime: Date
    var account: String
    var repositories: [Repository]
    var recordCount: Int
    var configuration: WorkspaceConfiguration
    var comparison: HistoryComparison?
    var selectedRepositories: Set<String>
    var filters: BoardFilters
    var scope: ActivityScope
    var selected: String?
    var periodA: Bool
    var relatedMode: String
    var relatedEvidence: RelatedEvidence?
}
struct ComparisonPresentation: Sendable {
    var projection: ComparisonProjection
    var inspector: ComparisonEvidence
    var inspectorScope: ActivityScope
    var related: [RelatedRow]
    var timeline: [TimelinePoint]
}
struct ComparisonBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @State private var presentation: ComparisonPresentation?
    @State private var loadedRequest: ComparisonRequest?
    @State private var failure = ""
    private var request: ComparisonRequest { ComparisonRequest(snapshotTime: workspace.snapshot.generatedAt, account: workspace.snapshot.accountID, repositories: workspace.snapshot.repositories, recordCount: workspace.snapshot.commits.count, configuration: workspace.configuration, comparison: navigation.comparison, selectedRepositories: navigation.repositories, filters: navigation.activity, scope: navigation.scope, selected: navigation.activitySelection, periodA: navigation.comparisonPeriodA, relatedMode: navigation.relatedMode, relatedEvidence: navigation.relatedEvidence) }
    private var sortedRows: [ComparisonArea] {
        guard let p = presentation, let comparison = navigation.comparison else { return [] }
        return p.projection.sortedRows(order: navigation.comparisonOrder, measure: navigation.comparisonMeasure, comparison: comparison, snapshot: workspace.snapshot.generatedAt, rates: navigation.comparisonRates, query: navigation.activity.query)
    }
    var body: some View {
        VStack(spacing: 8) {
            header
            HStack {
                Button { navigation.back() } label: { Image(systemName: "chevron.left") }.help("Back")
                Button("Repositories") { navigation.explore(ActivityScope()) }.buttonStyle(.link)
                Text(scopeLabel(navigation.scope)).lineLimit(1).truncationMode(.middle)
                Spacer()
                Picker("Measure", selection: $navigation.comparisonMeasure) { ForEach(ComparisonMeasure.allCases) { Text($0.rawValue).tag($0) } }.frame(width: 185)
                Toggle("Per 24 hours", isOn: $navigation.comparisonRates).toggleStyle(.checkbox).disabled(!navigation.comparisonMeasure.permitsRate)
            }.padding(.horizontal, 12)
            Picker("Activity lens", selection: Binding(get: { navigation.lens }, set: { navigation.setLens($0) })) { ForEach(ActivityLens.allCases) { Text($0.rawValue).tag($0) } }.pickerStyle(.segmented).padding(.horizontal, 12)
            if loadedRequest == request, let p = presentation {
                HSplitView {
                    VStack(alignment: .leading, spacing: 8) {
                        ComparisonSummary(values: p.projection.evidence.values, comparison: navigation.comparison!, snapshot: workspace.snapshot.generatedAt, rates: navigation.comparisonRates)
                        warnings(p.projection.evidence)
                        if navigation.lens == .directories && !navigation.scope.isFile { directoryList }
                        else {
                            evidencePicker
                            ScrollView {
                                VStack(alignment: .leading, spacing: 12) {
                                    switch navigation.lens {
                                    case .commits: CommitList(records: searched(evidence(p.projection.evidence)), showFiles: navigation.scope.isFile)
                                    case .people: peopleList(p.projection.evidence, canonical: p.projection.canonical)
                                    case .related: relatedList(p)
                                    case .directories: CommitList(records: searched(evidence(p.projection.evidence)), showFiles: true)
                                    case .heat: EmptyView()
                                    }
                                }.padding(.vertical, 8).frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                    }.padding(12).frame(minWidth: 430)
                    inspector(p).frame(minWidth: 270, idealWidth: 320, maxWidth: 390)
                }
            } else if !failure.isEmpty { Text(failure).foregroundStyle(.orange); Spacer() }
            else { ProgressView("Comparing history…"); Spacer() }
        }
        .task(id: request) { await load() }
    }
    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("Compare Git history").font(.headline)
                Spacer()
                Button("Edit periods…") { navigation.panel = .comparison }
                Button("Swap") { if var c = navigation.comparison { let a = c.a; c.a = c.b; c.b = a; navigation.applyComparison(c) } }
                Button("Exit comparison") { navigation.exitComparison() }
            }
            if let c = navigation.comparison {
                Text("A: \(c.a.label(timeZone: c.timeZone))\(c.a.partial(at: workspace.snapshot.generatedAt) ? " · partial" : "")    B: \(c.b.label(timeZone: c.timeZone))\(c.b.partial(at: workspace.snapshot.generatedAt) ? " · partial" : "")").font(.callout).textSelection(.enabled)
                Text("\(c.timeZoneID) · A − B · \(String(format: "%.2f", c.a.duration(at: workspace.snapshot.generatedAt) / 86400)) vs \(String(format: "%.2f", c.b.duration(at: workspace.snapshot.generatedAt) / 86400)) elapsed 24-hour days\(c.overlaps ? " · Overlapping periods" : "")").font(.caption).foregroundStyle(.secondary)
            }
        }.padding(.horizontal, 12)
    }
    private var directoryList: some View {
        VStack(spacing: 4) {
            Picker("Comparison sort", selection: $navigation.comparisonOrder) { ForEach(ComparisonOrder.allCases) { Text($0.rawValue).tag($0) } }.frame(maxWidth: 250)
            if sortedRows.isEmpty { EmptyBoard(title: "No paths match", message: "Try a broader scope or different periods.") }
            List(sortedRows, selection: $navigation.activitySelection) { row in
                HStack {
                    Label(row.label, systemImage: row.kind == "repository" ? "shippingbox" : row.kind == "directory" ? "folder" : "doc.text").lineLimit(1)
                    Spacer()
                    ComparisonNumbers(values: row.values, comparison: navigation.comparison!, snapshot: workspace.snapshot.generatedAt, measure: navigation.comparisonMeasure, rates: navigation.comparisonRates)
                    Button { navigation.explore(row.scope) } label: { Image(systemName: "chevron.right") }.buttonStyle(.borderless).help("Explore \(row.label)")
                }.padding(.vertical, 6).tag(row.id).contentShape(Rectangle()).onTapGesture(count: 2) { navigation.explore(row.scope) }
            }
            .onKeyPress(.return) { exploreSelected() }
            .onKeyPress(.rightArrow) { exploreSelected() }
        }
    }
    private func exploreSelected() -> KeyPress.Result {
        guard let row = sortedRows.first(where: { $0.id == navigation.activitySelection }) else { return .ignored }
        navigation.explore(row.scope); return .handled
    }
    private var evidencePicker: some View {
        Picker("Evidence period", selection: $navigation.comparisonPeriodA) { Text("Period A").tag(true); Text("Period B").tag(false) }.pickerStyle(.segmented).frame(maxWidth: 280)
    }
    private func evidence(_ value: ComparisonEvidence) -> [Commit] { navigation.comparisonPeriodA ? value.a : value.b }
    private func searched(_ records: [Commit]) -> [Commit] { navigation.searchCommits(records) }
    @ViewBuilder private func warnings(_ value: ComparisonEvidence) -> some View { ForEach(value.warnings, id: \.self) { Text($0).font(.caption).foregroundStyle(.orange) } }
    @ViewBuilder private func peopleList(_ value: ComparisonEvidence, canonical: [Commit]) -> some View {
        if let person = navigation.scopedPerson {
            Button("Back to people") { navigation.scopedPerson = nil }
            CommitList(records: searched(Analysis.people(evidence(value), canonicalRecords: canonical).first { $0.id == person }?.records ?? []))
        } else {
            ForEach(value.people.filter { navigation.activity.query.isEmpty || "\($0.name) \($0.email)".localizedCaseInsensitiveContains(navigation.activity.query) }) { person in
                Button { navigation.scopedPerson = person.id } label: {
                    HStack { Text(person.name); Spacer(); ComparisonNumbers(values: person.values, comparison: navigation.comparison!, snapshot: workspace.snapshot.generatedAt, measure: navigation.comparisonMeasure, rates: navigation.comparisonRates) }.padding(.vertical, 8)
                }.buttonStyle(.plain)
                Divider()
            }
        }
    }
    private func relatedList(_ p: ComparisonPresentation) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Picker("Related by", selection: $navigation.relatedMode) { Text("Shared commits").tag("commits"); Text("Contributor overlap").tag("people") }.pickerStyle(.segmented)
            Text("Relationships use the selected period. Historical overlap is not ownership or current presence.").font(.caption).foregroundStyle(.secondary)
            ForEach(p.related.filter { navigation.activity.query.isEmpty || scopeLabel($0.scope).localizedCaseInsensitiveContains(navigation.activity.query) }) { row in
                DisclosureGroup {
                    if navigation.relatedMode == "people" { ForEach(Analysis.people(row.records)) { Text($0.name) } }
                    else { CommitList(records: row.records) }
                    Button("Explore matching work") { navigation.exploreRelated(row) }
                } label: { Text("\(scopeLabel(row.scope)) · \(row.count) \(navigation.relatedMode == "people" ? "people" : "commits")") }
            }
        }
    }
    private func inspector(_ p: ComparisonPresentation) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                Text(scopeLabel(p.inspectorScope)).font(.headline).textSelection(.enabled)
                ComparisonSummary(values: p.inspector.values, comparison: navigation.comparison!, snapshot: workspace.snapshot.generatedAt, rates: navigation.comparisonRates)
                warnings(p.inspector)
                evidencePicker
                SectionTitle(title: "Contributors")
                ForEach(p.inspector.people.prefix(8)) { person in
                    Button(person.name) { navigation.explore(p.inspectorScope); navigation.setLens(.people); navigation.scopedPerson = person.id }.buttonStyle(.link)
                }
                Button("Open ordinary Contributors board") { navigation.switchBoard(.contributors) }.font(.caption)
                if !p.inspectorScope.repo.isEmpty {
                    Button("Open path on GitHub") { Task { do { NSWorkspace.shared.open(try await workspace.openPath(p.inspectorScope)) } catch { workspace.error = error.localizedDescription } } }
                    Button("View repository PRs") { navigation.showPRs(repo: p.inspectorScope.repo) }
                }
                SectionTitle(title: "Selected-period activity")
                ActivityTimeline(points: p.timeline)
                SectionTitle(title: "Current open pull requests")
                Text("Current evidence, not historical PR state.").font(.caption).foregroundStyle(.secondary)
                if workspace.snapshot.cachedOnly { Text("PR context unavailable offline.").font(.caption) }
                else {
                    if workspace.snapshot.repositories.contains(where: { $0.id == p.inspectorScope.repo && (!$0.prInventoryComplete || $0.prStale || ((!p.inspectorScope.path.isEmpty || !p.inspectorScope.areaName.isEmpty) && !$0.prFilesComplete)) }) { Text("PR evidence is incomplete or stale.").font(.caption).foregroundStyle(.orange) }
                    ForEach(Analysis.relatedPRs(workspace.snapshot.pullRequests, scope: p.inspectorScope, workAreas: workspace.configuration.workAreas).prefix(5)) { pr in
                        Button("#\(pr.number) \(pr.title)") { navigation.showPRs(repo: pr.repo, selected: pr.id) }.buttonStyle(.link)
                    }
                }
            }.padding(12)
        }
    }
    private func scopeLabel(_ scope: ActivityScope) -> String { scope.repo.isEmpty ? "All repositories" : scope.repo + (scope.path.isEmpty ? "" : " / " + scope.path) + (scope.areaName.isEmpty ? "" : " / " + scope.areaName) }
    private func load() async {
        guard let comparison = navigation.comparison else { return }
        let input = request, snapshot = workspace.snapshot, configuration = workspace.configuration
        presentation = nil; failure = ""
        let task = Task.detached(priority: .userInitiated) { () throws -> ComparisonPresentation in
            var p = try ComparisonProjection.build(snapshot: snapshot, configuration: configuration, comparison: comparison, repositories: input.selectedRepositories, scope: input.scope, hideBots: input.filters.hideBots, people: input.filters.people)
            if let restriction = input.relatedEvidence {
                func restricted(_ records: [Commit]) -> [Commit] {
                    if restriction.mode == "people" { return Analysis.people(records, canonicalRecords: p.canonical).filter { restriction.values.contains($0.id) }.flatMap(\.records) }
                    return records.filter { restriction.values.contains($0.id) }
                }
                p.evidence = ComparisonProjection.evidence(restricted(p.recordsA), restricted(p.recordsB), repos: snapshot.repositories.filter { input.selectedRepositories.isEmpty || input.selectedRepositories.contains($0.id) }, scope: input.scope, areas: configuration.workAreas, canonical: p.canonical)
            }
            let scope = p.rows.first { $0.id == input.selected }?.scope ?? input.scope
            let inspector = scope == input.scope ? p.evidence : ComparisonProjection.evidence(p.recordsA, p.recordsB, repos: snapshot.repositories.filter { input.selectedRepositories.isEmpty || input.selectedRepositories.contains($0.id) }, scope: scope, areas: configuration.workAreas, canonical: p.canonical)
            let records = input.periodA ? p.recordsA : p.recordsB
            let period = input.periodA ? comparison.a : comparison.b
            var calendar = Calendar(identifier: .gregorian); calendar.timeZone = comparison.timeZone
            let scoped = input.periodA ? inspector.a : inspector.b
            let timeline = try ComparisonTimeline.points(scoped, period: period, snapshot: snapshot.generatedAt, calendar: calendar)
            try Task.checkCancellation()
            return ComparisonPresentation(projection: p, inspector: inspector, inspectorScope: scope, related: Analysis.relatedScopes(records, scope: input.scope, mode: input.relatedMode, workAreas: configuration.workAreas), timeline: timeline)
        }
        do {
            let value = try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
            guard !Task.isCancelled, request == input else { return }
            loadedRequest = input
            presentation = value
        } catch is CancellationError {} catch { if !Task.isCancelled, request == input { failure = error.localizedDescription } }
    }
}
struct ComparisonNumbers: View {
    var values: ComparisonValues
    var comparison: HistoryComparison
    var snapshot: Date
    var measure: ComparisonMeasure
    var rates: Bool
    var body: some View {
        let pair = values.values(measure, comparison: comparison, snapshot: snapshot, rates: rates)
        VStack(alignment: .trailing, spacing: 2) {
            Text("A \(number(pair.0))   B \(number(pair.1))   Δ \(pair.0 - pair.1 >= 0 ? "+" : "")\(number(pair.0 - pair.1))\(values.reliable && (measure == .commits || measure == .people || (!values.a.unknown && !values.b.unknown)) ? "" : " + ?")").monospacedDigit()
            Text(values.relative(measure, a: pair.0, b: pair.1)).font(.caption).foregroundStyle(.secondary)
        }.accessibilityElement(children: .combine)
    }
    private func number(_ value: Double) -> String {
        if rates && measure.permitsRate { return value.formatted(.number.precision(.significantDigits(1...4))) }
        return value.formatted(.number.precision(.fractionLength(0)))
    }
}
struct ComparisonSummary: View {
    var values: ComparisonValues
    var comparison: HistoryComparison
    var snapshot: Date
    var rates: Bool
    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 4) {
            ForEach(ComparisonMeasure.allCases) { measure in
                GridRow { Text(measure.rawValue + (rates && measure.permitsRate ? " / 24h" : "")).font(.caption); ComparisonNumbers(values: values, comparison: comparison, snapshot: snapshot, measure: measure, rates: rates).font(.caption) }
            }
        }
    }
}
