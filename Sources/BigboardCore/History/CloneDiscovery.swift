import Foundation

enum CloneDiscovery {
    static let skippedDirectories: Set<String> = [
        "node_modules", ".build", "build", "dist", "target", "vendor", "Pods", "DerivedData", "Library",
        ".Trash",
    ]

    static func roots(_ configured: [String]) -> [URL] {
        var seen = Set<String>()
        return configured.map { URL(fileURLWithPath: ($0 as NSString).expandingTildeInPath) }.map { $0.standardizedFileURL.resolvingSymlinksInPath() }
            .filter { url in
                var directory: ObjCBool = false
                return FileManager.default.fileExists(atPath: url.path, isDirectory: &directory)
                    && directory.boolValue && seen.insert(url.path).inserted
            }
    }

    static func candidates(in roots: [URL], maximumDepth: Int = 4, limit: Int = 2000) -> [URL] {
        var found: [URL] = []
        var queue = roots.map { ($0, 0) }
        var visited = Set<String>()
        let manager = FileManager.default
        while !queue.isEmpty, found.count < limit {
            let (directory, depth) = queue.removeFirst()
            guard visited.insert(directory.path).inserted else { continue }
            if manager.fileExists(atPath: directory.appendingPathComponent(".git").path) {
                found.append(directory)
                continue
            }
            guard depth < maximumDepth,
                let children = try? manager.contentsOfDirectory(
                    at: directory, includingPropertiesForKeys: [.isDirectoryKey, .isSymbolicLinkKey],
                    options: [.skipsHiddenFiles])
            else { continue }
            for child in children.sorted(by: { Analysis.literalLess($0.path, $1.path) }) {
                guard !skippedDirectories.contains(child.lastPathComponent),
                    let values = try? child.resourceValues(forKeys: [.isDirectoryKey, .isSymbolicLinkKey]),
                    values.isDirectory == true, values.isSymbolicLink != true
                else { continue }
                queue.append((child, depth + 1))
            }
        }
        return found
    }

    static func githubRepository(_ url: String) -> String? {
        var value = url.trimmingCharacters(in: .whitespacesAndNewlines)
        for prefix in [
            "https://github.com/", "http://github.com/", "ssh://git@github.com/", "git@github.com:",
            "git://github.com/", "ssh://github.com/",
        ] where value.lowercased().hasPrefix(prefix) {
            value = String(value.dropFirst(prefix.count))
            if value.hasSuffix("/") { value.removeLast() }
            if value.hasSuffix(".git") { value.removeLast(4) }
            guard let id = canonicalGitHubRepository(value), GitHistory.validID(id) else { return nil }
            return id
        }
        return nil
    }

    static func remote(_ remotes: [(name: String, url: String)]) -> (name: String, id: String)? {
        let github = remotes.compactMap { remote in githubRepository(remote.url).map { (remote.name, $0) } }
        for preferred in ["upstream", "origin"] {
            if let match = github.first(where: { $0.0 == preferred }) { return match }
        }
        return github.sorted { $0.0 < $1.0 }.first
    }

    static func inspect(_ directory: URL) async -> Repository? {
        let prefix = ["-C", directory.path]
        guard
            let listing = try? await GitProcess.text(
                prefix + ["config", "--local", "--get-regexp", #"^remote\..*\.url$"#]),
            let common = try? await GitProcess.text(
                prefix + ["rev-parse", "--path-format=absolute", "--git-common-dir"]), common.hasPrefix("/")
        else { return nil }
        let remotes = listing.split(separator: "\n").compactMap { line -> (name: String, url: String)? in
            let parts = line.split(separator: " ", maxSplits: 1)
            guard parts.count == 2 else { return nil }
            let key = parts[0].dropFirst("remote.".count).dropLast(".url".count)
            return (String(key), String(parts[1]))
        }
        guard let (remote, id) = remote(remotes) else { return nil }
        let git = ["--git-dir=" + common]
        let head = (try? await GitProcess.text(git + ["symbolic-ref", "--quiet", "refs/remotes/\(remote)/HEAD"])) ?? ""
        let branchPrefix = "refs/remotes/\(remote)/"
        let shallow = (try? await GitProcess.text(git + ["rev-parse", "--is-shallow-repository"])) == "true"
        let config = (try? await GitProcess.text(git + ["config", "--local", "--list"])) ?? ""
        let partial = config.split(separator: "\n").contains { line in
            let lower = line.lowercased()
            return lower.hasPrefix("extensions.partialclone=")
                || (lower.hasPrefix("remote.") && lower.contains(".promisor=true"))
        }
        var repository = Repository(id: id, branch: "")
        if head.hasPrefix(branchPrefix) {
            repository.branch = String(head.dropFirst(branchPrefix.count))
            if let seconds = try? await GitProcess.text(
                git + ["log", "-1", "--format=%ct", "--end-of-options", head, "--"]), let value = Double(seconds)
            {
                repository.pushedAt = Date(timeIntervalSince1970: value)
            }
        }
        repository.path = common
        repository.remote = remote
        repository.shallow = shallow
        repository.partial = partial
        return repository
    }

    static func find(in roots: [URL], maximumDepth: Int = 4) async -> [Repository] {
        let directories = candidates(in: roots, maximumDepth: maximumDepth)
        var results: [(URL, Repository)] = []
        for start in stride(from: 0, to: directories.count, by: 8) {
            if Task.isCancelled { break }
            let batch = Array(directories[start..<min(start + 8, directories.count)])
            let inspected = await withTaskGroup(of: (URL, Repository?).self) { group in
                for directory in batch { group.addTask { (directory, await inspect(directory)) } }
                var values: [(URL, Repository?)] = []
                for await value in group { values.append(value) }
                return values
            }
            results += inspected.compactMap { directory, repo in repo.map { (directory, $0) } }
        }
        var byID: [String: Repository] = [:]
        var commons = Set<String>()
        for (_, repo) in results.sorted(by: { Analysis.literalLess($0.0.path, $1.0.path) }) {
            guard commons.insert(repo.path).inserted, byID[repo.id] == nil else { continue }
            byID[repo.id] = repo
        }
        return byID.values.sorted { Analysis.literalLess($0.id, $1.id) }
    }
}
