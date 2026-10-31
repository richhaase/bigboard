import Foundation

enum SettingsRepositorySort: String, CaseIterable {
    case recent, name
}
struct SettingsRepositories {
    static func rows(available: [Repository], saved: [String], sort: SettingsRepositorySort, query: String) -> [Repository] {
        var rows = Dictionary(available.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for id in saved where rows[id] == nil { rows[id] = Repository(id: id) }
        return rows.values.filter { query.isEmpty || $0.id.localizedCaseInsensitiveContains(query) }.sorted {
            if sort == .recent, $0.pushedAt != $1.pushedAt { return ($0.pushedAt ?? .distantPast) > ($1.pushedAt ?? .distantPast) }
            return $0.id < $1.id
        }
    }
    static func applying(_ selected: Set<String>, available: Set<String>, to config: WorkspaceConfiguration) -> WorkspaceConfiguration {
        var next = config
        next.repositories = selected.sorted()
        next.excludedRepositories = Set(config.excludedRepositories).union(available.subtracting(selected)).subtracting(selected).sorted()
        return next
    }
}
