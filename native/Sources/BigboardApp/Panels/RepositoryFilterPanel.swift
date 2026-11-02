import SwiftUI

struct RepositoryFilterPanel: View {
    var repositories: [Repository]
    @Bindable var navigation: BoardState
    var repositoryListHeightLimit: CGFloat? = nil
    var body: some View {
        RepositoryFilterSelection(
            repositories: repositories,
            selection: Binding(
                get: { navigation.repositories },
                set: { navigation.setRepositoryFilter($0, available: repositories) }),
            listHeightLimit: repositoryListHeightLimit
        )
        .frame(maxWidth: .infinity, alignment: .leading).padding(16)
    }
}
struct RepositoryFilterSelection: View {
    var repositories: [Repository]
    @Binding var selection: Set<String>
    var listHeightLimit: CGFloat? = nil
    @State private var query = ""
    @State private var highlightedRepository: String?
    @FocusState private var searchFocused: Bool
    @FocusState private var listFocused: Bool
    private var available: Set<String> { Set(repositories.map(\.id)) }
    private var checklist: RepositoryChecklist {
        RepositoryChecklist(selection: selection, available: available)
    }
    private var visible: [Repository] {
        repositories.sorted { $0.id < $1.id }
            .filter { query.isEmpty || $0.id.localizedCaseInsensitiveContains(query) }
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if repositories.isEmpty {
                Text("No repository data is loaded. Choose repositories in Settings to get started.")
                    .foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            } else {
                HStack {
                    Text("\(checklist.checked.count) of \(repositories.count) selected")
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Select all loaded") { selection = [] }
                }
                TextField("Search loaded repositories", text: $query)
                    .textFieldStyle(.roundedBorder)
                    .focused($searchFocused)
                    .onAppear { if listHeightLimit != nil { searchFocused = true } }
                    .onSubmit {
                        guard listHeightLimit != nil, let first = visible.first else { return }
                        highlightedRepository = first.id
                        listFocused = true
                    }
                if visible.isEmpty {
                    Text("No loaded repositories match this search.").foregroundStyle(.secondary)
                } else if let listHeightLimit {
                    List(visible, selection: $highlightedRepository) { repo in
                        repositoryRow(repo).lineLimit(1).help(repo.id).tag(repo.id)
                    }
                    .listStyle(.plain)
                    .focused($listFocused)
                    .frame(height: min(CGFloat(visible.count) * 26 + 8, listHeightLimit))
                    .onKeyPress(.space) {
                        guard let id = highlightedRepository, visible.contains(where: { $0.id == id }) else {
                            return .ignored
                        }
                        setChecked(id, checked: !checklist.checked.contains(id))
                        return .handled
                    }
                } else {
                    ForEach(visible) { repo in repositoryRow(repo) }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
    private func setChecked(_ id: String, checked: Bool) {
        if let updated = checklist.setting(id, checked: checked) { selection = updated }
    }
    private func repositoryRow(_ repo: Repository) -> some View {
        Toggle(
            repo.id,
            isOn: Binding(
                get: { checklist.checked.contains(repo.id) },
                set: { checked in
                    setChecked(repo.id, checked: checked)
                })
        )
        .toggleStyle(.checkbox)
        .disabled(checklist.checked == [repo.id])
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
