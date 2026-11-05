import SwiftUI

struct DataStatusPanel: View {
    @Bindable var workspace: WorkspaceModel
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    Text(workspace.snapshot.coverage.phase.capitalized).font(.title2.bold())
                    Spacer()
                    Button("Refresh") { Task { await workspace.refresh() } }.disabled(workspace.busy)
                }
                Text(workspace.snapshot.coverage.message)
                LabeledContent("Snapshot", value: workspace.snapshot.generatedAt.formatted())
                LabeledContent(
                    "Requests",
                    value:
                        "\(workspace.snapshot.coverage.probeRequests) probes · \(workspace.snapshot.coverage.inventoryRequests) inventory · \(workspace.snapshot.coverage.fileRequests) files"
                )
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
}
