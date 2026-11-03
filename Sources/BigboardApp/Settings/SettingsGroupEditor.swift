import Foundation

struct SettingsGroupEditor {
    var name = "" { didSet { error = "" } }
    var repositories = "" { didSet { error = "" } }
    private(set) var original: String?
    private(set) var error = ""
    var isEditing: Bool { original != nil }
    var canSave: Bool { !destination.isEmpty && !parsedRepositories.isEmpty }
    private var destination: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var parsedRepositories: [String] {
        Array(Set(parseListEntries(repositories).map { $0.lowercased() })).sorted()
    }

    mutating func edit(name: String, repositories: [String]) {
        self.name = name
        self.repositories = repositories.joined(separator: "\n")
        original = name
        error = ""
    }
    mutating func reset() { self = Self() }
    @discardableResult mutating func save(to groups: inout [String: [String]]) -> Bool {
        guard canSave else {
            error = "Enter a group name and at least one repository."
            return false
        }
        let target = destination
        if let original {
            guard groups[original] != nil else {
                error = "This group was removed. Cancel editing and add it again."
                return false
            }
            guard target == original || groups[target] == nil else {
                error = "A group named \(target) already exists. Choose another name."
                return false
            }
            if target != original { groups.removeValue(forKey: original) }
        }
        groups[target] = parsedRepositories
        reset()
        return true
    }
    mutating func remove(name: String, from groups: inout [String: [String]]) {
        groups.removeValue(forKey: name)
        if original == name { reset() }
    }
}
