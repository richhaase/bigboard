import AppKit
import Foundation

enum SettingsRepositorySort: String, CaseIterable {
    case recent, name
}
struct SettingsRepositories {
    static func displayPath(_ path: String, home: String = NSHomeDirectory()) -> String {
        var value = path
        if value.hasSuffix("/.git") { value.removeLast(5) }
        if value == home { return "~" }
        return value.hasPrefix(home + "/") ? "~" + value.dropFirst(home.count) : value
    }
    @MainActor static func chooseFolders() -> [String] {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.prompt = "Use Folders"
        panel.message = "Choose the folders that hold your Git clones. Bigboard searches up to four levels deep."
        guard panel.runModal() == .OK else { return [] }
        return panel.urls.map(\.standardizedFileURL.path)
    }
    static func rows(available: [Repository], saved: [String], sort: SettingsRepositorySort, query: String)
        -> [Repository]
    {
        var rows = Dictionary(available.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for id in saved where rows[id] == nil { rows[id] = Repository(id: id) }
        return rows.values.filter { query.isEmpty || $0.id.localizedCaseInsensitiveContains(query) }
            .sorted {
                if sort == .recent, $0.pushedAt != $1.pushedAt {
                    return ($0.pushedAt ?? .distantPast) > ($1.pushedAt ?? .distantPast)
                }
                return $0.id < $1.id
            }
    }
    static func applying(_ selected: Set<String>, available: Set<String>, to config: WorkspaceConfiguration)
        -> WorkspaceConfiguration
    {
        let selectedIDs = Set(selected.map { $0.lowercased() })
        let availableIDs = Set(available.map { $0.lowercased() })
        let excludedIDs = Set(config.excludedRepositories.map { $0.lowercased() })
        var next = config
        next.repositories = selectedIDs.sorted()
        next.excludedRepositories = excludedIDs.union(availableIDs.subtracting(selectedIDs))
            .subtracting(selectedIDs).sorted()
        return next
    }
}
