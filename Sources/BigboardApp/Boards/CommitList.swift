import SwiftUI

struct CommitList: View {
    var records: [Commit]
    var showFiles = false
    @State private var limit = 100
    var sorted: [Commit] { Analysis.unique(records).sorted { $0.date > $1.date } }
    var body: some View {
        if records.isEmpty {
            EmptyBoard(
                title: "No commits in this view", message: "Expand the date range or adjust the filters.")
        } else {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(Array(sorted.prefix(limit).enumerated()), id: \.offset) { _, commit in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(alignment: .top) {
                            Button {
                                openGitHub("https://github.com/\(commit.repo)/commit/\(commit.id)")
                            } label: {
                                Text(commit.subject.isEmpty ? "Untitled commit" : commit.subject)
                                    .multilineTextAlignment(.leading)
                            }
                            .buttonStyle(.plain).fontWeight(.medium)
                            Spacer(minLength: 12)
                            DeltaView(
                                added: commit.added, removed: commit.removed, unknown: commit.linesUnknown)
                        }
                        HStack {
                            Text(commit.author)
                            Text(commit.date, format: .dateTime.year().month().day())
                            Text(String(commit.id.prefix(7))).fontDesign(.monospaced)
                            if commit.ai { Text("AI assisted") }
                            if commit.bot { Text("Bot") }
                        }
                        .font(.caption).foregroundStyle(.secondary)
                        if showFiles {
                            ForEach(commit.files.filter { $0.excludedReason.isEmpty }, id: \.path) { file in
                                Text(file.path).font(.caption.monospaced()).foregroundStyle(.secondary)
                                    .textSelection(.enabled)
                            }
                        }
                    }
                    .padding(.vertical, 10)
                    Divider()
                }
                if sorted.count > limit {
                    Button("Show 100 more (\(sorted.count - limit) remaining)") { limit += 100 }
                        .padding(.vertical)
                }
            }
            .onChange(of: records.count) { limit = 100 }
        }
    }
}
