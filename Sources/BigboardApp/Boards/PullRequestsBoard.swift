import SwiftUI
import AppKit

struct PRRow: Identifiable {
    var pr: PullRequest
    var attention: Int
    var id: String { pr.id }
    var title: String { pr.title }
    var repo: String { pr.repo }
    var author: String { pr.author.lowercased() }
    var reviewRank: Int { Analysis.reviewRank(pr) }
    var checksRank: Int { Analysis.checksRank(pr) }
    var size: Int { Analysis.prSize(pr) }
    var opened: Date { pr.createdAt ?? .distantPast }
    var updated: Date { pr.updatedAt ?? .distantPast }
}

struct PullRequestsBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    var prs: [PullRequest] { navigation.pullRequests(workspace.snapshot) }
    var viewer: String { workspace.snapshot.viewer }
    var rows: [PRRow] {
        prs.map { pr in
            PRRow(
                pr: pr,
                attention: viewer.isEmpty
                    ? 1 : Analysis.requested(pr, viewer: viewer) ? 0 : Analysis.needsAttention(pr, viewer: viewer) ? 0 : 1)
        }
    }
    var selected: PullRequest? { prs.first { $0.id == navigation.prSelection } }
    var repositories: [Repository] {
        navigation.visibleRepositories(workspace.snapshot)
            .filter { navigation.contextualRepository == nil || $0.id == navigation.contextualRepository }
    }
    var loading: Bool {
        workspace.busy && !workspace.snapshot.cachedOnly && repositories.contains { $0.prCheckedAt == nil }
    }
    var unknown: Bool {
        repositories.contains {
            ($0.prCheckedAt != nil || !workspace.busy) && (!$0.prInventoryComplete || $0.prStale)
        }
    }
    private static let sortKeys: [(String, KeyPathComparator<PRRow>)] = [
        ("attention", KeyPathComparator(\PRRow.attention)),
        ("title", KeyPathComparator(\PRRow.title)),
        ("repository", KeyPathComparator(\PRRow.repo)),
        ("author", KeyPathComparator(\PRRow.author)),
        ("review", KeyPathComparator(\PRRow.reviewRank)),
        ("checks", KeyPathComparator(\PRRow.checksRank)),
        ("size", KeyPathComparator(\PRRow.size, order: .reverse)),
        ("created", KeyPathComparator(\PRRow.opened, order: .reverse)),
        ("updated", KeyPathComparator(\PRRow.updated, order: .reverse)),
    ]
    private var sortOrder: Binding<[KeyPathComparator<PRRow>]> {
        Binding(
            get: {
                guard var comparator = Self.sortKeys.first(where: { $0.0 == navigation.prs.sort })?.1 else {
                    return []
                }
                if navigation.prs.reversed {
                    comparator.order = comparator.order == .forward ? .reverse : .forward
                }
                return [comparator]
            },
            set: { order in
                guard let chosen = order.first,
                    let match = Self.sortKeys.first(where: { $0.1.keyPath == chosen.keyPath })
                else { return }
                navigation.prs.sort = match.0
                navigation.prs.reversed = chosen.order != match.1.order
            })
    }
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                BoardSearch(navigation: navigation)
                Spacer()
                if !workspace.snapshot.cachedOnly, incomplete {
                    IncompleteBadge(text: loading ? "Still loading" : "Incomplete")
                }
                Text(summary).foregroundStyle(.secondary).monospacedDigit()
            }
            .padding(.horizontal, 12).padding(.vertical, 10)
            if let context = navigation.contextualRepository {
                HStack {
                    Text("Showing pull requests in \(context)").foregroundStyle(.secondary)
                    Spacer()
                    Button("Show All Repositories") { navigation.showAllPRs() }
                }
                .padding(.horizontal, 12).padding(.bottom, 10)
            }
            HStack {
                facetMenu(
                    "Mine",
                    options: [
                        ("attention", "Needs my attention"), ("authored", "Authored by me"),
                        ("requested", "Review requested"), ("updated", "Updated since my review"),
                    ], values: $navigation.prs.mine)
                    .keyboardShortcut("f", modifiers: [.command, .shift])
                facetMenu(
                    "Review",
                    options: [
                        ("APPROVED", "Approved"), ("CHANGES_REQUESTED", "Changes requested"),
                        ("REVIEW_REQUIRED", "Needs review"), ("UNKNOWN", "Unknown"),
                    ], values: $navigation.prs.reviews)
                facetMenu(
                    "Checks",
                    options: [
                        ("SUCCESS", "Passing"), ("FAILURE", "Failing"), ("ERROR", "Error"),
                        ("PENDING", "Pending"), ("UNKNOWN", "Unknown"),
                    ], values: $navigation.prs.checks)
                facetMenu(
                    "State", options: [("open", "Ready for review"), ("draft", "Draft")],
                    values: $navigation.prs.states)
                facetMenu(
                    "Author",
                    options: Set(workspace.snapshot.pullRequests.map(\.author)).sorted().map { ($0, $0) },
                    values: $navigation.prs.authors)
                Spacer()
                Button("Clear Filters") { navigation.resetBoard() }
                    .disabled(!filtered)
            }
            .padding(.horizontal, 12).padding(.bottom, 10)
            VSplitView {
                content.frame(minHeight: 160, idealHeight: 320)
                if let pr = selected {
                    detailPane(pr).frame(minHeight: 280, idealHeight: 400)
                }
            }
        }
    }
    private func detailPane(_ pr: PullRequest) -> some View {
        VStack(spacing: 0) {
            HStack(alignment: .firstTextBaseline) {
                Text("\(pr.repo) #\(pr.number)").font(.caption).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                Spacer()
                Button("Open on GitHub", systemImage: "arrow.up.right") { openGitHub(pr.url) }
                Button("Copy Link", systemImage: "link") { copyToPasteboard(pr.url) }
                Button("Close", systemImage: "xmark") { navigation.prSelection = nil }
                    .labelStyle(.iconOnly).buttonStyle(.borderless)
                    .keyboardShortcut(.cancelAction)
                    .help("Close details")
            }
            .padding(.horizontal, 16).padding(.top, 10).padding(.bottom, 6)
            ScrollView { detail(pr).padding(.horizontal, 16).padding(.bottom, 14) }
        }
        .background(.background)
    }
    @ViewBuilder private var content: some View {
        if prs.isEmpty && loading {
            ProgressView("Loading pull requests…").frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if prs.isEmpty {
            EmptyBoard(
                title: workspace.snapshot.cachedOnly
                    ? "PR context is unavailable offline"
                    : unknown ? "PR evidence is incomplete" : "No pull requests match",
                message: workspace.snapshot.cachedOnly
                    ? "Turn off Use cached history only in Settings → General to load open PRs."
                    : "Adjust filters or refresh. Data status explains any missing evidence.",
                symbol: "arrow.triangle.pull"
            )
        } else {
            table
        }
    }
    private var table: some View {
        Table(rows, selection: $navigation.prSelection, sortOrder: sortOrder) {
            TableColumn("", sortUsing: KeyPathComparator(\PRRow.attention)) { row in
                if row.attention == 0 {
                    Image(systemName: "circle.fill").font(.system(size: 7)).foregroundStyle(.tint)
                        .help("Needs your attention")
                        .accessibilityLabel("Needs your attention")
                }
            }
            .width(14)
            TableColumn("Pull Request", sortUsing: KeyPathComparator(\PRRow.title)) { row in
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Text("#\(row.pr.number)").foregroundStyle(.secondary).monospacedDigit()
                        Text(row.pr.title).lineLimit(1)
                        if row.pr.draft { StateBadge(value: "DRAFT") }
                    }
                    if !row.pr.labels.isEmpty {
                        Text(row.pr.labels.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
            }
            .width(min: 220, ideal: 360)
            TableColumn("Repository", sortUsing: KeyPathComparator(\PRRow.repo)) { row in
                Text(row.pr.repo).foregroundStyle(.secondary).lineLimit(1)
            }
            .width(min: 90, ideal: 150)
            TableColumn("Author", sortUsing: KeyPathComparator(\PRRow.author)) { row in
                Text(row.pr.author).lineLimit(1)
                    .foregroundStyle(row.pr.author.lowercased() == viewer.lowercased() ? Color.accentColor : .primary)
            }
            .width(min: 70, ideal: 100)
            TableColumn("Review", sortUsing: KeyPathComparator(\PRRow.reviewRank)) { row in
                StateBadge(value: row.pr.review)
            }
            .width(min: 80, ideal: 120)
            TableColumn("Checks", sortUsing: KeyPathComparator(\PRRow.checksRank)) { row in
                HStack(spacing: 4) {
                    StateBadge(value: row.pr.checks)
                    if !row.pr.failingChecks.isEmpty {
                        Text("\(row.pr.failingChecks.count)").font(.caption).foregroundStyle(.orange)
                            .help(row.pr.failingChecks.joined(separator: "\n"))
                    }
                }
            }
            .width(min: 80, ideal: 110)
            TableColumn("Size", sortUsing: KeyPathComparator(\PRRow.size, order: .reverse)) { row in
                if let additions = row.pr.additions, let deletions = row.pr.deletions {
                    HStack(spacing: 4) {
                        Text("+\(additions)").foregroundStyle(.green)
                        Text("−\(deletions)").foregroundStyle(.red)
                    }
                    .font(.callout.monospacedDigit())
                } else {
                    Text("—").foregroundStyle(.secondary)
                }
            }
            .width(min: 70, ideal: 100)
            TableColumn("Opened", sortUsing: KeyPathComparator(\PRRow.opened, order: .reverse)) { row in
                Text(relative(row.pr.createdAt)).foregroundStyle(.secondary)
            }
            .width(min: 60, ideal: 90)
            TableColumn("Updated", sortUsing: KeyPathComparator(\PRRow.updated, order: .reverse)) { row in
                HStack(spacing: 4) {
                    Text(relative(row.pr.updatedAt)).foregroundStyle(.secondary)
                    if idle(row.pr) {
                        Image(systemName: "hourglass").foregroundStyle(.orange)
                            .help("No updates in 14 days or more")
                            .accessibilityLabel("Idle")
                    }
                }
            }
            .width(min: 70, ideal: 100)
        }
        .contextMenu(forSelectionType: String.self) { ids in
            if let pr = prs.first(where: { ids.contains($0.id) }), ids.count == 1 {
                Button("Open on GitHub") { openGitHub(pr.url) }
                Button("Copy Link") { copyToPasteboard(pr.url) }
                Button("Copy Title and Number") { copyToPasteboard("\(pr.repo)#\(pr.number) \(pr.title)") }
            }
        } primaryAction: { ids in
            for pr in prs where ids.contains(pr.id) { openGitHub(pr.url) }
        }
        .focusedSceneValue(\.selectedGitHubTarget, selected.map { .url($0.url) })
    }
    private var filtered: Bool {
        let filters = navigation.prs
        return !filters.query.isEmpty || !filters.authors.isEmpty || !filters.reviews.isEmpty
            || !filters.checks.isEmpty || !filters.states.isEmpty || !filters.mine.isEmpty
    }
    private var incomplete: Bool {
        unknown || loading
            || workspace.snapshot.pullRequests.contains {
                !$0.reviewersComplete
                    && (navigation.repositories.isEmpty || navigation.repositories.contains($0.repo))
                    && (navigation.contextualRepository == nil || $0.repo == navigation.contextualRepository)
            }
    }
    private var summary: String {
        guard !workspace.snapshot.cachedOnly else { return "Unavailable offline" }
        if loading && prs.isEmpty { return "Loading…" }
        let count = "\(prs.count) open"
        guard !viewer.isEmpty else { return count }
        let scoped = workspace.snapshot.pullRequests.filter {
            (navigation.repositories.isEmpty || navigation.repositories.contains($0.repo))
                && (navigation.contextualRepository == nil || $0.repo == navigation.contextualRepository)
        }
        let awaiting = scoped.filter { Analysis.requested($0, viewer: viewer) }.count
        return "\(count) · \(awaiting) awaiting your review"
    }
    private func relative(_ date: Date?) -> String {
        guard let date else { return "Unknown" }
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .short
        return formatter.localizedString(for: date, relativeTo: workspace.snapshot.generatedAt)
    }
    private func idle(_ pr: PullRequest) -> Bool {
        guard let date = pr.updatedAt else { return false }
        return workspace.snapshot.generatedAt.timeIntervalSince(date) >= 14 * 86400
    }
    private func dated(_ date: Date?) -> String {
        guard let date else { return "Unknown" }
        return "\(date.formatted(date: .abbreviated, time: .shortened)) (\(relative(date)))"
    }
    private func detail(_ pr: PullRequest) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(pr.title).font(.title3.bold()).textSelection(.enabled)
            if !pr.labels.isEmpty {
                Text(pr.labels.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
            }
            HStack(alignment: .top, spacing: 32) {
                facts(pr).frame(minWidth: 240, maxWidth: 360, alignment: .leading)
                VStack(alignment: .leading, spacing: 8) {
                    checks(pr)
                    reviews(pr)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            DisclosureGroup("Changed files (\(pr.files.count) of \(pr.fileCount))") {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(pr.files, id: \.self) {
                        Text($0).font(.callout.monospaced()).textSelection(.enabled)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            if !pr.inventoryComplete || !pr.filesComplete || !pr.reviewersComplete || !pr.reviewsComplete {
                Text(
                    [
                        !pr.inventoryComplete ? "Inventory incomplete" : "",
                        !pr.filesComplete ? "Changed files incomplete" : "",
                        !pr.reviewersComplete ? "Reviewer requests incomplete" : "",
                        !pr.reviewsComplete ? "Review history incomplete" : "",
                    ]
                    .filter { !$0.isEmpty }.joined(separator: " · ")
                )
                .font(.caption).foregroundStyle(.orange)
            }
            Text("Review and check results are evidence, not merge authorization.").font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
    private func facts(_ pr: PullRequest) -> some View {
        Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 6) {
            GridRow {
                Text("Author").foregroundStyle(.secondary)
                Text(pr.author)
            }
            GridRow {
                Text("State").foregroundStyle(.secondary)
                Text(pr.draft ? "Draft" : "Ready for review")
            }
            GridRow {
                Text("Opened").foregroundStyle(.secondary)
                Text(dated(pr.createdAt))
            }
            GridRow {
                Text("Updated").foregroundStyle(.secondary)
                Text(dated(pr.updatedAt)).foregroundStyle(idle(pr) ? .orange : .primary)
            }
            GridRow {
                Text("Size").foregroundStyle(.secondary)
                if let additions = pr.additions, let deletions = pr.deletions {
                    Text("+\(additions) −\(deletions) in \(pr.fileCount) files").monospacedDigit()
                } else {
                    Text("\(pr.fileCount) files")
                }
            }
            GridRow {
                Text("Conflicts").foregroundStyle(.secondary)
                Text(
                    pr.mergeable == "CONFLICTING"
                        ? "Conflicts reported" : pr.mergeable == "MERGEABLE" ? "None reported" : "Unknown"
                )
                .foregroundStyle(pr.mergeable == "CONFLICTING" ? .orange : .primary)
            }
        }
        .font(.callout)
    }
    @ViewBuilder private func checks(_ pr: PullRequest) -> some View {
        HStack {
            Text("Checks").foregroundStyle(.secondary)
            StateBadge(value: pr.checks)
        }
        .font(.callout)
        ForEach(pr.failingChecks, id: \.self) { name in
            Label(name, systemImage: "xmark.circle").foregroundStyle(.orange).font(.callout)
        }
        if !pr.failingChecksComplete && pr.checks != "SUCCESS" {
            Text("More checks ran than Bigboard collected.").font(.caption).foregroundStyle(.secondary)
        }
    }
    @ViewBuilder private func reviews(_ pr: PullRequest) -> some View {
        HStack {
            Text("Review").foregroundStyle(.secondary)
            StateBadge(value: pr.review)
        }
        .font(.callout)
        .padding(.top, 4)
        ForEach(Array(pr.reviews.enumerated()), id: \.offset) { _, review in
            HStack {
                Text(review.author)
                StateBadge(value: review.state)
                if let date = review.submittedAt {
                    Text(relative(date)).font(.caption).foregroundStyle(.secondary)
                }
            }
            .font(.callout)
        }
        let waiting = pr.reviewers.filter { reviewer in
            !pr.reviews.contains { $0.author.lowercased() == reviewer.lowercased() }
        }
        ForEach(waiting, id: \.self) { reviewer in
            HStack {
                Text(reviewer)
                Text("requested").font(.caption).foregroundStyle(.secondary)
            }
            .font(.callout)
        }
        if pr.reviews.isEmpty && waiting.isEmpty {
            Text("No reviews or requests known.").font(.callout).foregroundStyle(.secondary)
        }
    }
    private func facetMenu(_ title: String, options: [(String, String)], values: Binding<Set<String>>)
        -> some View
    {
        FilterPopover(title: values.wrappedValue.isEmpty ? title : "\(title) (\(values.wrappedValue.count))") {
            Button("All") { values.wrappedValue = [] }
            ForEach(options, id: \.0) { key, label in
                Toggle(
                    label,
                    isOn: Binding(
                        get: { values.wrappedValue.contains(key) },
                        set: {
                            if $0 { values.wrappedValue.insert(key) } else { values.wrappedValue.remove(key) }
                        }))
            }
        }
    }
}
