import SwiftUI

struct DataStatusPanel: View {
    @Bindable var workspace: WorkspaceModel
    private var phaseTitle: String {
        switch workspace.snapshot.coverage.phase {
        case "signin": "Sign-in needed"
        case "setup": "Setup needed"
        case "refreshing": "Refreshing"
        case "ready": "Up to date"
        case "partial": "Partly up to date"
        case "stopped": "Refresh stopped"
        default: "Waiting for data"
        }
    }
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    Text(phaseTitle).font(.title2.bold())
                    Spacer()
                    if workspace.canStopRefresh {
                        Button("Stop") { Task { await workspace.stop() } }
                    } else {
                        Button("Refresh") { Task { await workspace.refresh() } }
                            .disabled(workspace.busy || !workspace.authStatus.signedIn)
                    }
                }
                Text(workspace.snapshot.coverage.message)
                if let progress = workspace.progress, progress.total > 0 {
                    ProgressView(value: Double(progress.completed), total: Double(progress.total)) {
                        Text("Updating history · \(progress.completed) of \(progress.total) repositories")
                            .font(.caption)
                    }
                }
                LabeledContent("Last updated", value: workspace.lastUpdated?.formatted() ?? "Not this session")
                LabeledContent("Snapshot", value: workspace.snapshot.generatedAt.formatted())
                LabeledContent(
                    "Requests",
                    value:
                        "\(workspace.snapshot.coverage.inventoryRequests) inventory · \(workspace.snapshot.coverage.fileRequests) files"
                )
                if !workspace.snapshot.cachedOnly {
                    LabeledContent(
                        "Merged PR requests",
                        value:
                            "\(workspace.snapshot.coverage.mergedInventoryRequests) inventory · \(workspace.snapshot.coverage.mergedFileRequests) files"
                    )
                }
                SectionTitle(title: "Repository evidence")
                ForEach(workspace.snapshot.repositories) { repo in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(repo.id).fontWeight(.medium)
                            Spacer()
                            Text(repo.stale ? "Stale history" : "History loaded")
                                .foregroundStyle(repo.stale ? .orange : .secondary)
                        }
                        Text(
                            "History: \(repo.scannedAt?.formatted() ?? "unknown") · PRs: \(repo.prCheckedAt?.formatted() ?? "unknown")"
                        )
                        .font(.caption)
                        Text(
                            "PR inventory \(repo.prInventoryComplete ? "complete" : "incomplete") · files \(repo.prFilesComplete ? "complete" : "incomplete") · reviewers \(repo.prReviewersComplete ? "complete" : "incomplete")\(repo.prStale ? " · stale" : "")"
                        )
                        .font(.caption).foregroundStyle(.secondary)
                        if !workspace.snapshot.cachedOnly {
                            Text(mergedPRStatus(repo.mergedPRCoverage))
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        if !repo.error.isEmpty {
                            Text(repo.error).font(.caption).foregroundStyle(.orange).textSelection(.enabled)
                        }
                    }
                    Divider()
                }
                if !workspace.snapshot.coverage.errors.isEmpty {
                    SectionTitle(title: "Issues")
                    ForEach(Array(workspace.snapshot.coverage.errors.enumerated()), id: \.offset) {
                        _, error in Text(error).foregroundStyle(.orange).textSelection(.enabled)
                    }
                }
                SectionTitle(title: "Local storage")
                Text(workspace.storageDescription).font(.callout.monospaced()).textSelection(.enabled)
            }
            .padding(24)
        }
    }

    private func mergedPRStatus(_ coverage: MergedPRCoverage) -> String {
        guard let checkedAt = coverage.checkedAt, coverage.inventoryComplete else {
            return "Merged PR inventory incomplete"
        }
        let end = checkedAt.formatted()
        let range = coverage.coveredFrom.map { "\($0.formatted()) – \(end)" } ?? "all history through \(end)"
        return "Merged PR inventory \(range)\(coverage.stale ? " · stale" : "")"
    }
}
