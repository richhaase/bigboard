import SwiftUI

struct ActivityBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    @Environment(\.calendar) private var calendar
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
    private var scopedPersonName: String? {
        guard let id = navigation.scopedPerson else { return nil }
        return Analysis.people(scoped, canonicalRecords: records).first { $0.id == id }?.name ?? id
    }
    private var singlePerson: Bool { Analysis.totals(scoped).people <= 1 }
    private var title: String {
        let scope = navigation.scope
        if !scope.areaName.isEmpty { return scope.areaName }
        if !scope.path.isEmpty { return scope.path }
        return scope.repo.isEmpty ? "All repositories" : scope.repo
    }

    var body: some View {
        let linkScope = navigation.lens == .prTrends ? navigation.scope : inspectScope
        VStack(spacing: 0) {
            if navigation.lens != .prTrends {
                HStack {
                    BoardSearch(navigation: navigation)
                    Spacer()
                    PeopleFilter(records: workspace.snapshot.commits, filters: $navigation.activity)
                }
                .padding(.horizontal, 12).padding(.vertical, 10)
            }
            scopeBar.padding(.top, navigation.lens == .prTrends ? 12 : 0)
            Picker(
                "Activity lens",
                selection: Binding(get: { navigation.lens }, set: { navigation.setLens($0) })
            ) { ForEach(ActivityLens.allCases) { Text($0.rawValue).tag($0) } }
            .pickerStyle(.segmented).padding(.horizontal, 12).padding(.bottom, 12)
            if navigation.lens == .prTrends {
                prTrends
            } else {
                HSplitView {
                    lensContent.frame(minWidth: 450)
                    inspector.frame(minWidth: 260, idealWidth: 310, maxWidth: 390)
                }
            }
        }
        .focusedSceneValue(
            \.selectedGitHubTarget,
            linkScope.repo.isEmpty || workspace.pathExists(linkScope) == false ? nil : .scope(linkScope))
    }

    private var scopeBar: some View {
        HStack {
            Button("All repositories") { navigation.explore(ActivityScope()) }.buttonStyle(.link)
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
            if let name = scopedPersonName {
                Text("/").foregroundStyle(.tertiary)
                Text(name).lineLimit(1)
            }
            Spacer()
            if navigation.lens == .directories {
                Picker("Sort", selection: $navigation.activity.sort) {
                    Text("Lines changed").tag("change")
                    Text("Commits").tag("commits")
                    Text("Merged PRs").tag("mergedPRs")
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
                            CommitList(
                                records: navigation.searchCommits(scoped), showFiles: true,
                                showAuthor: { navigation.showContributor($0) }).padding(16)
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
                                records: navigation.searchCommits(scoped), showFiles: navigation.scope.isFile,
                                showAuthor: { navigation.showContributor($0) })
                        case .people: scopedPeople
                        case .related: relatedList
                        case .directories, .prTrends: EmptyView()
                        }
                    }
                    .padding(16)
                }
            }
        }
    }

    private var prTrends: some View {
        let trend = navigation.activityPRTrends(
            workspace.snapshot, configuration: workspace.configuration, calendar: calendar)
        return Group {
            if trend.weeks.contains(where: \.hasMergeCount) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("PR size is additions plus deletions. Net growth is additions minus deletions.")
                            .foregroundStyle(.secondary)
                        if !navigation.scope.path.isEmpty || !navigation.scope.areaName.isEmpty {
                            Text("Line totals cover every file in each PR that touched this scope.")
                                .foregroundStyle(.secondary)
                        }
                        if trend.coverage != .complete {
                            Text("Merge coverage is incomplete or stale. The charts show known PRs.")
                                .foregroundStyle(.secondary)
                        }
                        let merged = trend.weeks.reduce(0) { $0 + $1.size.mergedCount }
                        let sized = trend.weeks.reduce(0) { $0 + $1.size.sizedCount }
                        if sized < merged {
                            Text("Line counts available for \(sized.formatted()) of \(merged.formatted()) known merged PRs.")
                                .foregroundStyle(.secondary)
                        }
                        WeeklyPRTrends(points: trend.weeks)
                    }
                    .padding(16)
                }
            } else if trend.coverage == .complete {
                EmptyBoard(title: "No merged PRs", message: "No pull requests merged in the selected scope and date range.")
            } else {
                EmptyBoard(
                    title: "Weekly PR trends unavailable",
                    message: "Data Status shows merged PR coverage for the selected repositories.")
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
            let mergedPRs = navigation.activityMergedPRs(workspace.snapshot)
            let coverage = mergedPRs.coverage(in: navigation.scope)
            let prCount = mergedPRs.count(in: navigation.scope, workAreas: workspace.configuration.workAreas)
            HStack {
                MetricView(title: "Commits", value: total.commits.formatted())
                MetricView(title: "Merged PRs", value: coverage.label(prCount))
                MetricView(title: "Contributors", value: total.people.formatted())
                MetricView(title: "Lines changed", value: total.change.formatted())
            }
            if let note = coverage.note {
                Text(note).font(.caption).foregroundStyle(.secondary)
            }
            if let note = navigation.mergedPRScopeNote {
                Text(note).font(.caption).foregroundStyle(.secondary)
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
                let single = singlePerson
                let mergedPRs = navigation.activityMergedPRs(workspace.snapshot)
                List(rows, selection: $navigation.activitySelection) { row in
                    HStack {
                        Image(
                            systemName: row.kind == "repository"
                                ? "shippingbox" : row.kind == "directory" ? "folder" : "doc.text"
                        )
                        .foregroundStyle(.secondary)
                        VStack(alignment: .leading) {
                            HStack(spacing: 6) {
                                Text(row.label).lineLimit(1)
                                if workspace.pathExists(row.scope) == false { RemovedBadge() }
                            }
                            let coverage = mergedPRs.coverage(in: row.scope)
                            let activity = single
                                ? counted(row.totals.commits, "commit")
                                : "\(counted(row.totals.commits, "commit")) · \(counted(row.totals.people, "contributor"))"
                            Text("\(activity) · \(coverage.activityLabel(row.mergedPRCount))")
                                .font(.caption).foregroundStyle(.secondary)
                                .help(coverage.note ?? "Merged PRs that touched this scope")
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
                }
                .contextMenu(forSelectionType: String.self) { ids in
                    if ids.count == 1, let row = rows.first(where: { ids.contains($0.id) }) {
                        Button("Explore \(row.label)", systemImage: "chevron.right") { navigation.explore(row.scope) }
                        Divider()
                        ScopeLinkItems(scope: row.scope, workspace: workspace)
                    }
                } primaryAction: { ids in
                    if ids.count == 1, let row = rows.first(where: { ids.contains($0.id) }) {
                        navigation.explore(row.scope)
                    }
                }
                .task(id: rows.map(\.id)) { await workspace.checkPaths(rows.map(\.scope)) }
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
                CommitList(
                    records: navigation.searchCommits(
                        Analysis.people(scoped, canonicalRecords: records).first { $0.id == person }?.records
                            ?? []),
                    showAuthor: { navigation.showContributor($0) })
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
                        navigation.showPerson(person.id)
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
                    .contextMenu {
                        Button("Show Commits") { navigation.showPerson(person.id) }
                        Button("Show in Contributors") { navigation.showContributor(person.id) }
                        Divider()
                        PersonCopyItems(name: person.name, email: person.email)
                    }
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
                        CommitList(records: row.records, showAuthor: { navigation.showContributor($0) })
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
                Text(navigation.rangeLabel)
                    .font(.caption).foregroundStyle(.secondary)
                let total = Analysis.totals(inspectRecords)
                DeltaView(added: total.added, removed: total.removed, unknown: total.unknown)
                let mergedPRs = navigation.activityMergedPRs(workspace.snapshot)
                let coverage = mergedPRs.coverage(in: inspectScope)
                let prCount = mergedPRs.count(in: inspectScope, workAreas: workspace.configuration.workAreas)
                Text("\(counted(total.commits, "commit")) · \(coverage.activityLabel(prCount))")
                    .foregroundStyle(.secondary)
                    .help(coverage.note ?? "Merged PRs that touched this scope")
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
                    .contextMenu {
                        Button("Show in Contributors") { navigation.showContributor(person.id) }
                        Divider()
                        PersonCopyItems(name: person.name, email: person.email)
                    }
                }
                SectionTitle(title: "Recent activity")
                ActivityTimeline(
                    points: navigation.timelinePoints(
                        inspectRecords, snapshot: workspace.snapshot))
                SectionTitle(title: "Open pull requests")
                let related = relatedPRs
                if navigation.dateRange.isCustomInterval && !related.isEmpty {
                    Text("Still open, and opened before this range ended.")
                        .font(.caption).foregroundStyle(.secondary)
                }
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
                    IncompleteBadge(text: "Evidence is incomplete or stale; this list may omit matching PRs.")
                }
                ForEach(related.prefix(5)) { pr in
                    Button {
                        navigation.showPRs(repo: pr.repo, selected: pr.id)
                    } label: {
                        Text(verbatim: "#\(pr.number) \(pr.title)").multilineTextAlignment(.leading)
                    }
                    .buttonStyle(.link)
                    .contextMenu {
                        Button("Show in Pull Requests") { navigation.showPRs(repo: pr.repo, selected: pr.id) }
                        Button("Open on GitHub", systemImage: "arrow.up.right") { openGitHub(pr.url) }
                        Button("Copy Link") { copyToPasteboard(pr.url) }
                    }
                }
                if related.isEmpty && !inspectScope.repo.isEmpty {
                    Text(
                        navigation.dateRange.isCustomInterval
                            ? "No open pull requests were opened by the end of this range."
                            : "No open pull requests."
                    )
                    .font(.caption).foregroundStyle(.secondary)
                }
                if !inspectScope.repo.isEmpty {
                    Button("View repository PRs") { navigation.showPRs(repo: inspectScope.repo) }
                    let scope = inspectScope
                    if workspace.pathExists(scope) == false {
                        Text("This path is not in the latest scan, so it has no GitHub link.")
                            .font(.caption).foregroundStyle(.secondary)
                    } else {
                        Button(
                            scope.path.isEmpty ? "Open repository on GitHub" : "Open path on GitHub",
                            systemImage: "arrow.up.right"
                        ) { openGitHub(scope, workspace: workspace) }
                    }
                }
            }
            .padding(16)
        }
    }
    private var relatedPRs: [PullRequest] {
        Analysis.relatedPRs(
            workspace.snapshot.pullRequests, scope: inspectScope, workAreas: workspace.configuration.workAreas,
            openedBy: navigation.dateRange.isCustomInterval
                ? navigation.dateRange.end(relativeTo: workspace.snapshot.generatedAt) : nil)
    }
}
