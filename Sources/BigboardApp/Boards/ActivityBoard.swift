import SwiftUI

struct ActivityBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    var records: [Commit] { navigation.filteredCommits(workspace.snapshot, for: .activity) }
    var scoped: [Commit] {
        navigation.evidenceRecords(
            Analysis.scoped(
                records, scope: navigation.scope, workAreas: workspace.configuration.workAreas),
            canonical: records)
    }
    var rows: [ActivityRow] {
        navigation.activityRows(workspace.snapshot, configuration: workspace.configuration)
    }
    var inspectScope: ActivityScope {
        rows.first { $0.id == navigation.activitySelection }?.scope ?? navigation.scope
    }
    var inspectRecords: [Commit] {
        navigation.evidenceRecords(
            Analysis.scoped(records, scope: inspectScope, workAreas: workspace.configuration.workAreas),
            canonical: records)
    }
    private var title: String {
        let scope = navigation.scope
        if !scope.areaName.isEmpty { return scope.areaName }
        if !scope.path.isEmpty { return scope.path }
        return scope.repo.isEmpty ? "All repositories" : scope.repo
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                BoardSearch(navigation: navigation)
                Spacer()
                PeopleFilter(records: workspace.snapshot.commits, filters: $navigation.activity)
            }
            .padding(.horizontal, 12).padding(.vertical, 10)
            if navigation.activityInterval != nil {
                HStack {
                    Text("From Heat Maps: \(navigation.activityRangeLabel)")
                    Spacer()
                    Button("Back to Heat Maps") { navigation.switchBoard(.heat) }
                    Button("Use shared date range") { navigation.activityInterval = nil }
                }
                .font(.callout).padding(.horizontal, 12).padding(.bottom, 10)
            }
            scopeBar
            Picker(
                "Activity lens",
                selection: Binding(get: { navigation.lens }, set: { navigation.setLens($0) })
            ) { ForEach(ActivityLens.allCases) { Text($0.rawValue).tag($0) } }
            .pickerStyle(.segmented).padding(.horizontal, 12).padding(.bottom, 12)
            HSplitView {
                lensContent.frame(minWidth: 450)
                inspector.frame(minWidth: 260, idealWidth: 310, maxWidth: 390)
            }
        }
    }

    private var scopeBar: some View {
        HStack {
            Button {
                navigation.back()
            } label: {
                Image(systemName: "chevron.left")
            }
            .disabled(!navigation.canGoBackInActivity)
            Button("Repositories") { navigation.explore(ActivityScope()) }.buttonStyle(.link)
            if !navigation.scope.repo.isEmpty {
                Text("/").foregroundStyle(.tertiary)
                Button(navigation.scope.repo) {
                    navigation.explore(ActivityScope(repo: navigation.scope.repo))
                }
                .buttonStyle(.link)
            }
            if !navigation.scope.path.isEmpty || !navigation.scope.areaName.isEmpty {
                Text("/").foregroundStyle(.tertiary)
                Text(
                    navigation.scope.areaName.isEmpty ? navigation.scope.path : navigation.scope.areaName
                )
                .lineLimit(1).truncationMode(.middle)
            }
            Spacer()
            if navigation.lens == .directories {
                Picker("Sort", selection: $navigation.activity.sort) {
                    Text("Lines changed").tag("change")
                    Text("Commits").tag("commits")
                    Text("Recent").tag("recent")
                    Text("Name").tag("name")
                }
                .frame(width: 175)
                Button {
                    navigation.activity.reversed.toggle()
                } label: {
                    Image(systemName: navigation.activity.reversed ? "arrow.up" : "arrow.down")
                }
            }
        }
        .padding(.horizontal, 12).padding(.bottom, 10)
    }

    private var lensContent: some View {
        Group {
            if navigation.lens == .directories {
                VStack(alignment: .leading, spacing: 0) {
                    activitySummary.padding(16)
                    if navigation.scope.isFile {
                        ScrollView {
                            CommitList(records: navigation.searchCommits(scoped), showFiles: true).padding(16)
                        }
                    } else {
                        directoryList
                    }
                }
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        activitySummary
                        switch navigation.lens {
                        case .commits:
                            CommitList(
                                records: navigation.searchCommits(scoped), showFiles: navigation.scope.isFile)
                        case .people: scopedPeople
                        case .related: relatedList
                        case .directories: EmptyView()
                        }
                    }
                    .padding(16)
                }
            }
        }
    }

    private var activitySummary: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let evidence = navigation.relatedEvidence {
                Text(
                    evidence.mode == "people"
                        ? "Showing contributors shared with the previous area"
                        : "Showing commits shared with the previous area"
                )
                .font(.caption).foregroundStyle(.secondary)
            }
            let total = Analysis.totals(scoped)
            HStack {
                MetricView(title: "Commits", value: total.commits.formatted())
                MetricView(title: "Contributors", value: total.people.formatted())
                MetricView(title: "Lines changed", value: total.change.formatted())
            }
            if scoped.contains(where: \.pathsUnknown) {
                Text("Some commits have unknown paths and cannot be assigned to directories.").font(.caption)
                    .foregroundStyle(.orange)
            }
            let excluded = Analysis.excludedLines(scoped)
            if !excluded.isEmpty {
                Text(
                    "\(excluded.values.reduce(0, +).formatted()) generated, vendored, fixture, or bulk-data lines excluded."
                )
                .font(.caption).foregroundStyle(.secondary)
            }
        }
    }
    private var directoryList: some View {
        Group {
            if rows.isEmpty {
                EmptyBoard(title: "No paths match", message: "Try a broader scope or another date range.")
            } else {
                List(rows, selection: $navigation.activitySelection) { row in
                    HStack {
                        Image(
                            systemName: row.kind == "repository"
                                ? "shippingbox" : row.kind == "directory" ? "folder" : "doc.text"
                        )
                        .foregroundStyle(.secondary)
                        VStack(alignment: .leading) {
                            Text(row.label).lineLimit(1)
                            Text("\(row.totals.commits) commits · \(row.totals.people) contributors")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        DeltaView(
                            added: row.totals.added, removed: row.totals.removed, unknown: row.totals.unknown)
                        Button {
                            navigation.explore(row.scope)
                        } label: {
                            Image(systemName: "chevron.right")
                        }
                        .buttonStyle(.borderless).help("Explore \(row.label)")
                    }
                    .padding(.vertical, 8).contentShape(Rectangle()).tag(row.id)
                    .onTapGesture(count: 2) { navigation.explore(row.scope) }
                }
                .onKeyPress(.rightArrow) { exploreSelected() }
                .onKeyPress(.return) { exploreSelected() }
            }
        }
    }
    private func exploreSelected() -> KeyPress.Result {
        guard let row = rows.first(where: { $0.id == navigation.activitySelection }) else { return .ignored }
        navigation.explore(row.scope)
        return .handled
    }
    private var scopedPeople: some View {
        VStack(alignment: .leading) {
            if let person = navigation.scopedPerson {
                Button("Back to people") { navigation.scopedPerson = nil }
                CommitList(
                    records: navigation.searchCommits(
                        Analysis.people(scoped, canonicalRecords: records).first { $0.id == person }?.records
                            ?? []))
            } else {
                ForEach(
                    Analysis.people(scoped, canonicalRecords: records)
                        .filter {
                            navigation.activityPeopleQuery.isEmpty
                                || "\($0.name) \($0.email)"
                                    .localizedCaseInsensitiveContains(navigation.activityPeopleQuery)
                        }
                ) { person in
                    Button {
                        navigation.scopedPerson = person.id
                    } label: {
                        HStack {
                            Image(systemName: "person.crop.circle")
                            VStack(alignment: .leading) {
                                Text(person.name)
                                Text("\(person.totals.commits) commits").font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                            DeltaView(added: person.totals.added, removed: person.totals.removed)
                        }
                        .padding(.vertical, 8)
                    }
                    .buttonStyle(.plain)
                    Divider()
                }
            }
        }
    }
    private var relatedList: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Related by", selection: $navigation.relatedMode) {
                Text("Shared commits").tag("commits")
                Text("Contributor overlap").tag("people")
            }
            .pickerStyle(.segmented)
            Text("Historical overlap describes related work, not ownership or current presence.")
                .font(.caption).foregroundStyle(.secondary)
            let related =
                Analysis.related(
                    records, scope: navigation.scope, mode: navigation.relatedMode,
                    workAreas: workspace.configuration.workAreas
                )
            if related.isEmpty {
                EmptyBoard(
                    title: "No related evidence", message: "Choose a repository or directory with activity.")
            }
            ForEach(related) { row in
                DisclosureGroup {
                    if navigation.relatedMode == "people" {
                        ForEach(Analysis.people(row.records), id: \.id) { Text($0.name).font(.callout) }
                    } else {
                        CommitList(records: row.records)
                    }
                    Button("Explore matching work") { navigation.exploreRelated(row) }
                } label: {
                    HStack {
                        Text(
                            row.scope.areaName.isEmpty
                                ? "\(row.scope.repo) / \(row.scope.path)"
                                : "\(row.scope.repo) / \(row.scope.areaName)")
                        Spacer()
                        Text("\(row.count) \(navigation.relatedMode == "people" ? "people" : "commits")")
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 5)
            }
        }
    }
    private var inspector: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Text(
                    !inspectScope.areaName.isEmpty
                        ? inspectScope.areaName
                        : inspectScope.path.isEmpty
                            ? inspectScope.repo.isEmpty ? "Selection" : inspectScope.repo : inspectScope.path
                )
                .font(.title3.bold()).textSelection(.enabled)
                Text(navigation.activityRangeLabel)
                    .font(.caption).foregroundStyle(.secondary)
                let total = Analysis.totals(inspectRecords)
                DeltaView(added: total.added, removed: total.removed, unknown: total.unknown)
                Text("\(total.commits) commits").foregroundStyle(.secondary)
                SectionTitle(title: "Contributors")
                ForEach(Array(Analysis.people(inspectRecords, canonicalRecords: records).prefix(8))) {
                    person in
                    Button {
                        navigation.showContributor(person.id)
                    } label: {
                        HStack {
                            Text(person.name).lineLimit(1)
                            Spacer()
                            Text("\(person.totals.commits)").foregroundStyle(.secondary)
                        }
                    }
                    .buttonStyle(.plain)
                }
                SectionTitle(title: "Recent activity")
                ActivityTimeline(
                    points: navigation.timelinePoints(
                        inspectRecords, snapshot: workspace.snapshot))
                SectionTitle(title: "Open pull requests")
                let related = relatedPRs
                if workspace.snapshot.cachedOnly {
                    Text("PR context is unavailable offline.").font(.caption).foregroundStyle(.secondary)
                } else if workspace.busy && workspace.snapshot.repositories.contains(where: {
                    $0.id == inspectScope.repo && $0.prCheckedAt == nil
                }) {
                    Text("Loading pull requests…").font(.caption).foregroundStyle(.secondary)
                } else if workspace.snapshot.repositories.contains(where: {
                    $0.id == inspectScope.repo
                        && (!$0.prInventoryComplete || $0.prStale
                            || ((!inspectScope.path.isEmpty || !inspectScope.areaName.isEmpty)
                                && !$0.prFilesComplete))
                }) {
                    Text("Evidence is incomplete or stale; this list may omit matching PRs.").font(.caption)
                        .foregroundStyle(.orange)
                }
                ForEach(related.prefix(5)) { pr in
                    Button {
                        navigation.showPRs(repo: pr.repo, selected: pr.id)
                    } label: {
                        Text("#\(pr.number) \(pr.title)").multilineTextAlignment(.leading)
                    }
                    .buttonStyle(.link)
                }
                if !inspectScope.repo.isEmpty {
                    Button("View repository PRs") { navigation.showPRs(repo: inspectScope.repo) }
                    let scope = inspectScope
                    Button(
                        scope.path.isEmpty ? "Open repository on GitHub" : "Open path on GitHub",
                        systemImage: "arrow.up.right"
                    ) { openGitHub(scope, workspace: workspace) }
                    .focusedSceneValue(\.selectedActivityScope, scope)
                }
            }
            .padding(16)
        }
    }
    private var relatedPRs: [PullRequest] {
        Analysis.relatedPRs(
            workspace.snapshot.pullRequests, scope: inspectScope, workAreas: workspace.configuration.workAreas
        )
    }
}
