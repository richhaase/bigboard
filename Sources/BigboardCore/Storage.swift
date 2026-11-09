import Foundation
import Darwin

final class WorkspaceStorage: @unchecked Sendable {
    let configRoot: URL
    let cacheRoot: URL
    private var expected: Data?
    private let lock = NSLock()
    static func system(
        identity: ApplicationIdentity = .fromBundle(),
        environment: [String: String] = ProcessInfo.processInfo.environment,
        home: URL = FileManager.default.homeDirectoryForCurrentUser
    ) -> WorkspaceStorage {
        func base(_ variable: String, _ fallback: String) -> URL {
            if let value = environment[variable], value.hasPrefix("/") { return URL(fileURLWithPath: value) }
            return home.appendingPathComponent(fallback)
        }
        let config =
            environment["BIGBOARD_CONFIG_ROOT"].map { URL(fileURLWithPath: $0) }
            ?? base("XDG_CONFIG_HOME", ".config").appendingPathComponent(identity.directoryName)
        let cache =
            environment["BIGBOARD_CACHE_ROOT"].map { URL(fileURLWithPath: $0) }
            ?? base("XDG_CACHE_HOME", ".cache").appendingPathComponent(identity.directoryName)
        return WorkspaceStorage(configRoot: config, cacheRoot: cache)
    }
    init(configRoot: URL, cacheRoot: URL) {
        self.configRoot = configRoot.resolvingSymlinksInPath()
        self.cacheRoot = cacheRoot.resolvingSymlinksInPath()
    }
    var configurationFile: URL { configRoot.appendingPathComponent("config.json") }
    var historyRoot: URL { cacheRoot.appendingPathComponent("history") }
    func loadConfiguration() throws -> WorkspaceConfiguration {
        lock.lock()
        defer { lock.unlock() }
        guard let data = try read(configurationFile) else {
            expected = nil
            return WorkspaceConfiguration()
        }
        let value: WorkspaceConfiguration
        do { value = try JSONDecoder().decode(WorkspaceConfiguration.self, from: data) } catch {
            throw BigboardError.message(
                "Bigboard could not read \(configurationFile.path). Fix it, or delete it to start with new settings.")
        }
        try validate(value)
        expected = data
        return value
    }
    func reloadConfiguration() throws -> WorkspaceConfiguration? {
        lock.lock()
        let unchanged = (try? read(configurationFile)) == expected
        lock.unlock()
        return unchanged ? nil : try loadConfiguration()
    }
    func prepareConfigurationDirectory() throws -> URL {
        try checkedParent(configurationFile, create: true)
    }
    func saveConfiguration(_ value: WorkspaceConfiguration) throws {
        lock.lock()
        defer { lock.unlock() }
        try validate(value)
        guard try read(configurationFile) == expected else {
            throw BigboardError.message("Bigboard settings changed in \(configurationFile.path). Reopen to see them before saving.")
        }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let data = try encoder.encode(value)
        try write(data, to: configurationFile)
        expected = data
    }
    func validate(_ config: WorkspaceConfiguration) throws {
        guard config.requestBudget > 0, (0...3650).contains(config.sinceDays) else {
            throw BigboardError.message("Invalid request budget or startup range.")
        }
        guard [0, 5, 15, 30, 60].contains(config.refreshIntervalMinutes) else {
            throw BigboardError.message("Choose a supported automatic refresh interval.")
        }
        for id in config.repositories + config.groups.values.flatMap({ $0 }) {
            guard Self.validRepository(id) else {
                throw BigboardError.message("Repository must be owner/name: \(id)")
            }
            guard canonicalGitHubRepository(id) == id else {
                throw BigboardError.message("Repository must use lowercase owner/name: \(id)")
            }
        }
        guard config.sourceRoots.count <= 32,
            config.sourceRoots.allSatisfy({ $0.hasPrefix("/") && !$0.contains("\0") && $0.utf8.count <= 1024 })
        else { throw BigboardError.message("Source folders must be absolute paths (up to 32).") }
        var identities = Set<String>()
        for (email, rule) in config.identities {
            for value in [email] + rule.aliases {
                let normalized = value.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
                guard !normalized.isEmpty, identities.insert(normalized).inserted else {
                    throw BigboardError.message("Identity emails must be nonempty and unique.")
                }
            }
        }
        for (repo, areas) in config.workAreas {
            guard Self.validRepository(repo) else {
                throw BigboardError.message("Invalid work-area repository.")
            }
            guard canonicalGitHubRepository(repo) == repo else {
                throw BigboardError.message("Work-area repository must use lowercase owner/name: \(repo)")
            }
            var names = Set<String>(), paths = Set<String>(), prefixes = Set<String>()
            for area in areas {
                guard !area.name.isEmpty, names.insert(area.name).inserted, !area.paths.isEmpty else {
                    throw BigboardError.message("Work-area names must be unique and contain paths.")
                }
                for path in area.paths {
                    let bytes = path.utf8
                    let components = bytes.split(
                        separator: UInt8(ascii: "/"), omittingEmptySubsequences: false)
                    guard bytes.first != UInt8(ascii: "/"),
                        !components.contains(where: { $0.elementsEqual("..".utf8) }), !bytes.contains(0),
                        !bytes.isEmpty, paths.insert(path).inserted
                    else {
                        throw BigboardError.message(
                            "Work-area paths must be unique literal repository paths.")
                    }
                    let prefix = WorkArea.canonicalPrefix(path)
                    let prefixComponents = prefix.utf8.split(
                        separator: UInt8(ascii: "/"), omittingEmptySubsequences: false)
                    guard prefixComponents.allSatisfy({ !$0.isEmpty && !$0.elementsEqual(".".utf8) }) else {
                        throw BigboardError.message(
                            "Work-area path \(path) in \(repo) must not contain empty or '.' components. A single trailing slash is allowed."
                        )
                    }
                    guard prefixes.insert(prefix).inserted else {
                        throw BigboardError.message(
                            "Work-area path \(path) duplicates the prefix \(prefix) in \(repo). A trailing slash does not make a separate prefix."
                        )
                    }
                }
            }
        }
    }
    static func validRepository(_ id: String) -> Bool {
        canonicalGitHubRepository(id) != nil
    }
    func loadIdentity() throws -> GitHubIdentity? {
        guard let data = try read(cacheRoot.appendingPathComponent("gh-identity.json")) else { return nil }
        let value = try JSONDecoder().decode(GitHubIdentity.self, from: data)
        guard value.valid else { throw BigboardError.message("Saved GitHub identity is invalid.") }
        return value
    }
    func saveIdentity(_ value: GitHubIdentity) throws {
        guard value.valid else { throw BigboardError.message("GitHub identity is invalid.") }
        try write(JSONEncoder().encode(value), to: cacheRoot.appendingPathComponent("gh-identity.json"))
    }
    private func checkedParent(_ url: URL, create: Bool) throws -> URL {
        let parent = url.deletingLastPathComponent().standardizedFileURL
        guard parent.resolvingSymlinksInPath().path == parent.path else {
            throw BigboardError.message("Workspace directories cannot be symbolic links.")
        }
        if create {
            try FileManager.default.createDirectory(
                at: parent, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        }
        guard parent.resolvingSymlinksInPath().path == parent.path else {
            throw BigboardError.message("Workspace directories cannot be symbolic links.")
        }
        return parent
    }
    private func read(_ url: URL) throws -> Data? {
        _ = try checkedParent(url, create: false)
        let descriptor = Darwin.open(url.path, O_RDONLY | O_NOFOLLOW | O_NONBLOCK)
        if descriptor < 0 {
            if errno == ENOENT { return nil }
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? handle.close() }
        var info = stat()
        guard fstat(descriptor, &info) == 0, info.st_mode & S_IFMT == S_IFREG else {
            throw BigboardError.message("Workspace state must be a regular file.")
        }
        guard info.st_size <= 16 * 1024 * 1024 else {
            throw BigboardError.message("Workspace state is too large.")
        }
        let data = try handle.read(upToCount: 16 * 1024 * 1024 + 1) ?? Data()
        guard data.count <= 16 * 1024 * 1024 else {
            throw BigboardError.message("Workspace state is too large.")
        }
        return data
    }
    private func write(_ data: Data, to url: URL) throws {
        let parent = try checkedParent(url, create: true)
        _ = try read(url)
        let directory = Darwin.open(parent.path, O_RDONLY | O_DIRECTORY | O_NOFOLLOW)
        guard directory >= 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
        defer { Darwin.close(directory) }
        let temporary = ".state-" + UUID().uuidString
        let descriptor = openat(directory, temporary, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0o600)
        guard descriptor >= 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
        defer {
            Darwin.close(descriptor)
            unlinkat(directory, temporary, 0)
        }
        try data.withUnsafeBytes { bytes in
            var written = 0
            while written < bytes.count {
                let count = Darwin.write(
                    descriptor, bytes.baseAddress!.advanced(by: written), bytes.count - written)
                if count < 0 && errno == EINTR { continue }
                guard count > 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
                written += count
            }
        }
        guard fsync(descriptor) == 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
        guard renameat(directory, temporary, directory, url.lastPathComponent) == 0 else {
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
        guard fsync(directory) == 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
    }
}
