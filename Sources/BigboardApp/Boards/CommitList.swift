import SwiftUI

struct CommitList: View {
    var records: [Commit]
    var showFiles = false
    var showAuthor: ((String) -> Void)? = nil
    @State private var limit = 100
    var sorted: [Commit] { Analysis.unique(records).sorted { $0.date > $1.date } }
    var body: some View {
        if records.isEmpty {
            EmptyBoard(
                title: "No commits in this view", message: "Expand the date range or adjust the filters.")
        } else {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(sorted.prefix(limit)) { commit in
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(alignment: .top) {
                            Button {
                                openGitHub(commitURL(commit))
                            } label: {
                                let glyph = Text(Image(systemName: "arrow.up.right")).font(.caption)
                                    .foregroundStyle(.secondary)
                                Text("\(commit.subject.isEmpty ? "Untitled commit" : commit.subject) \(glyph)")
                                    .multilineTextAlignment(.leading)
                            }
                            .buttonStyle(.plain).fontWeight(.medium).help("Open commit on GitHub")
                            Spacer(minLength: 12)
                            DeltaView(
                                added: commit.added, removed: commit.removed, unknown: commit.linesUnknown)
                        }
                        HStack {
                            Text(commit.author)
                            Text(commit.date, format: .dateTime.year().month().day())
                            Text(String(commit.id.prefix(7))).fontDesign(.monospaced).textSelection(.enabled)
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
                    .contentShape(Rectangle())
                    .contextMenu { menu(commit) }
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
    @ViewBuilder private func menu(_ commit: Commit) -> some View {
        Button("Open on GitHub", systemImage: "arrow.up.right") { openGitHub(commitURL(commit)) }
        Button("Copy Link") { copyToPasteboard(commitURL(commit)) }
        Button("Copy SHA") { copyToPasteboard(commit.id) }
        Button("Copy Subject") { copyToPasteboard(commit.subject) }
        if let showAuthor {
            Divider()
            Button("Show \(commit.author) in Contributors") { showAuthor(commit.identity) }
        }
    }
}
