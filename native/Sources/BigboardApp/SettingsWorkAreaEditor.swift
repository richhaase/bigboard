import Foundation

struct SettingsWorkAreaEditor {
    struct Identity: Equatable {
        var repository: String
        var name: String
    }
    var repository = "" { didSet { error = "" } }
    var name = "" { didSet { error = "" } }
    var paths = "" { didSet { error = "" } }
    private(set) var original: Identity?
    private(set) var error = ""
    private var originalPaths: [String]?
    var isEditing: Bool { original != nil }
    var canSave: Bool { !destination.repository.isEmpty && !destination.name.isEmpty && !parsedPaths.isEmpty }
    private var destination: Identity { Identity(repository: repository.trimmingCharacters(in: .whitespacesAndNewlines), name: name.trimmingCharacters(in: .whitespacesAndNewlines)) }
    private var parsedPaths: [String] { if let originalPaths, paths.utf8.elementsEqual(originalPaths.joined(separator: ", ").utf8) { originalPaths } else { entries(paths) } }

    mutating func edit(repository: String, area: WorkArea) {
        self.repository = repository
        name = area.name
        paths = area.paths.joined(separator: ", ")
        original = Identity(repository: repository, name: area.name)
        originalPaths = area.paths
        error = ""
    }
    mutating func reset() { self = Self() }
    @discardableResult mutating func save(to workAreas: inout [String: [WorkArea]]) -> Bool {
        guard canSave else { error = "Enter a repository, area name, and at least one path."; return false }
        let target = destination
        let area = WorkArea(name: target.name, paths: parsedPaths)
        if let original {
            guard let index = workAreas[original.repository]?.firstIndex(where: { $0.name == original.name }) else {
                error = "This work area was removed. Cancel editing and add it again."
                return false
            }
            guard target == original || !(workAreas[target.repository] ?? []).contains(where: { $0.name == target.name }) else {
                error = "A work area named \(target.name) already exists in \(target.repository). Choose another name or repository."
                return false
            }
            if target.repository == original.repository {
                workAreas[original.repository]?[index] = area
            } else {
                workAreas[original.repository]?.remove(at: index)
                if workAreas[original.repository]?.isEmpty == true { workAreas.removeValue(forKey: original.repository) }
                workAreas[target.repository, default: []].append(area)
            }
        } else if let index = workAreas[target.repository]?.firstIndex(where: { $0.name == target.name }) {
            workAreas[target.repository]?[index] = area
        } else {
            workAreas[target.repository, default: []].append(area)
        }
        reset()
        return true
    }
    mutating func remove(repository: String, name: String, from workAreas: inout [String: [WorkArea]]) {
        workAreas[repository]?.removeAll { $0.name == name }
        if workAreas[repository]?.isEmpty == true { workAreas.removeValue(forKey: repository) }
        if original == Identity(repository: repository, name: name) { reset() }
    }
}
