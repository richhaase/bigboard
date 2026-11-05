import SwiftUI

struct PullRequestsBoard: View {
    @Bindable var workspace: WorkspaceModel
    @Bindable var navigation: BoardState
    var prs: [PullRequest] { navigation.pullRequests(workspace.snapshot) }
    var selected: PullRequest? { prs.first { $0.id == navigation.prSelection } ?? prs.first }
    var unknown: Bool {
        navigation.visibleRepositories(workspace.snapshot)
            .filter { navigation.contextualRepository == nil || $0.id == navigation.contextualRepository }
            .contains { !$0.prInventoryComplete || $0.prStale }
    }
    var body: some View {
        VStack(spacing: 0) {
            if let context = navigation.contextualRepository {
                HStack {
                    Button("Back to Activity", systemImage: "arrow.left") { navigation.returnToActivity() }
                    Text(context).foregroundStyle(.secondary)
                    Spacer()
                }
                .padding(.horizontal, 12).padding(.bottom, 10)
            }
            HStack {
                MetricView(
                    title: "Open in this view",
                    value: workspace.snapshot.cachedOnly ? "—" : "\(prs.count)\(unknown ? " + ?" : "")")
                MetricView(title: "Awaiting your review", value: awaitingReview)
                Picker("Sort", selection: $navigation.prs.sort) {
                    Text("Updated").tag("updated")
                    Text("Repository").tag("repository")
                    Text("Author").tag("author")
                    Text("Title").tag("title")
                    Text("Attention").tag("attention")
                }
                .frame(width: 200)
                Button {
                    navigation.prs.reversed.toggle()
                } label: {
                    Image(systemName: "arrow.up.arrow.down")
                }
            }
            .padding(.horizontal, 16).padding(.bottom, 12)
            HStack {
                facetMenu(
                    "Mine",
                    options: [
                        ("attention", "Needs my attention"), ("authored", "Authored by me"),
                        ("requested", "Review requested"), ("updated", "Updated since my review"),
                    ], values: $navigation.prs.mine)
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
                Button("Reset") { navigation.resetBoard() }
            }
            .padding(.horizontal, 12).padding(.bottom, 10)
            HSplitView {
                if prs.isEmpty {
                    EmptyBoard(
                        title: workspace.snapshot.cachedOnly
                            ? "PR context is unavailable offline"
                            : unknown ? "PR evidence is incomplete" : "No pull requests match",
                        message: workspace.snapshot.cachedOnly
                            ? "Turn off Cached history in Settings to load open PRs."
                            : "Adjust filters or refresh. Data status explains any missing evidence.",
                        symbol: "arrow.triangle.pull"
                    )
                    .frame(minWidth: 450)
                } else {
                    List(prs, selection: $navigation.prSelection) { pr in
                        VStack(alignment: .leading, spacing: 7) {
                            HStack {
                                Text(pr.repo).foregroundStyle(.secondary)
                                Text("#\(pr.number)")
                                if pr.draft { StateBadge(value: "DRAFT") }
                                Spacer()
                                Text(age(pr)).foregroundStyle(.secondary)
                            }
                            .font(.caption)
                            Text(pr.title).font(.headline).lineLimit(2)
                            HStack {
                                Text(pr.author)
                                    .foregroundStyle(
                                        pr.author.lowercased() == workspace.snapshot.viewer.lowercased()
                                            ? Color.accentColor : .secondary)
                                Spacer()
                                StateBadge(value: pr.review)
                                StateBadge(value: pr.checks)
                            }
                            .font(.caption)
                        }
                        .padding(.vertical, 7).tag(pr.id)
                    }
                    .frame(minWidth: 450)
                }
                ScrollView {
                    if let pr = selected {
                        detail(pr).padding(18)
                    } else {
                        Text("Select a pull request to inspect its evidence.").foregroundStyle(.secondary)
                            .padding()
                    }
                }
                .frame(minWidth: 280, idealWidth: 340, maxWidth: 450)
            }
        }
    }
    private var awaitingReview: String {
        guard !workspace.snapshot.viewer.isEmpty, !workspace.snapshot.cachedOnly else { return "—" }
        let selected = workspace.snapshot.pullRequests.filter {
            (navigation.repositories.isEmpty || navigation.repositories.contains($0.repo))
                && (navigation.contextualRepository == nil || $0.repo == navigation.contextualRepository)
                && navigation.dateMatches($0.updatedAt, snapshot: workspace.snapshot)
        }
        let count = selected.filter { Analysis.requested($0, viewer: workspace.snapshot.viewer) }.count
        return "\(count)\(unknown || selected.contains { !$0.reviewersComplete } ? " + ?" : "")"
    }
    private func age(_ pr: PullRequest) -> String {
        guard let date = pr.updatedAt else { return "Unknown" }
        let stale = workspace.snapshot.generatedAt.timeIntervalSince(date) >= 14 * 86400
        return
            "\(date.formatted(date: .abbreviated, time: .omitted))\(stale ? " !" : "")"
    }
    private func detail(_ pr: PullRequest) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("PULL REQUEST #\(pr.number)").font(.caption).foregroundStyle(.secondary)
            Text(pr.title).font(.title2.bold()).textSelection(.enabled)
            Text(pr.repo).foregroundStyle(.secondary)
            Button("Open on GitHub", systemImage: "arrow.up.right") { openGitHub(pr.url) }
                .buttonStyle(.borderedProminent)
                .focusedSceneValue(\.selectedPullRequestURL, pr.url)
            LabeledContent("Author", value: pr.author)
            LabeledContent("State", value: pr.draft ? "Draft" : "Open")
            LabeledContent("Review") { StateBadge(value: pr.review) }
            LabeledContent("Checks") { StateBadge(value: pr.checks) }
            LabeledContent("Updated", value: age(pr))
            LabeledContent(
                "Merge conflicts",
                value: pr.mergeable == "CONFLICTING"
                    ? "Conflicts reported" : pr.mergeable == "MERGEABLE" ? "None reported" : "Unknown")
            LabeledContent(
                "Reviewers", value: pr.reviewers.isEmpty ? "None known" : pr.reviewers.joined(separator: ", ")
            )
            Text("Review and check results are evidence, not merge authorization.").font(.caption)
                .foregroundStyle(.secondary)
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
            SectionTitle(title: "Changed files", detail: "\(pr.files.count) / \(pr.fileCount)")
            ForEach(pr.files, id: \.self) { Text($0).font(.callout.monospaced()).textSelection(.enabled) }
            SectionTitle(title: "Latest reviews")
            ForEach(Array(pr.reviews.enumerated()), id: \.offset) { _, review in
                HStack {
                    Text(review.author)
                    Spacer()
                    StateBadge(value: review.state)
                }
            }
        }
    }
    private func facetMenu(_ title: String, options: [(String, String)], values: Binding<Set<String>>)
        -> some View
    {
        Menu {
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
        } label: {
            Text(values.wrappedValue.isEmpty ? title : "\(title) (\(values.wrappedValue.count))")
        }
    }
}
