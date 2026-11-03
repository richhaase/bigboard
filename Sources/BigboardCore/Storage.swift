import Foundation
import Darwin

struct AccessGrant: Codable, Sendable {
    var accountID: String
    var repositories: [Repository]
}
final class WorkspaceStorage: @unchecked Sendable {
    let supportRoot: URL
    let cacheRoot: URL
    private var expected: [String: Data] = [:]
    private let lock = NSLock()
    static func system(
        identity: ApplicationIdentity = .fromBundle(),
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) -> WorkspaceStorage {
        let support =
            environment["BIGBOARD_DATA_ROOT"].map { URL(fileURLWithPath: $0) }
            ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent(identity.storageDirectory)
        let cache =
            environment["BIGBOARD_CACHE_ROOT"].map { URL(fileURLWithPath: $0) }
            ?? FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appendingPathComponent(identity.storageDirectory)
        return WorkspaceStorage(supportRoot: support, cacheRoot: cache)
    }
    init(supportRoot: URL, cacheRoot: URL) {
        self.supportRoot = supportRoot.resolvingSymlinksInPath()
        self.cacheRoot = cacheRoot.resolvingSymlinksInPath()
    }
    func accountRoot(_ id: String) throws -> URL {
        guard !id.isEmpty, id.allSatisfy(\.isNumber), UInt64(id) != nil else {
            throw BigboardError.message("Invalid GitHub account identifier")
        }
        return supportRoot.appendingPathComponent("accounts/\(id)")
    }
    func repositoryRoot(_ id: String) throws -> URL {
        _ = try accountRoot(id)
        return cacheRoot.appendingPathComponent("accounts/\(id)/repos")
    }
    func loadConfiguration(_ id: String) throws -> WorkspaceConfiguration {
        lock.lock()
        defer { lock.unlock() }
        let url = try accountRoot(id).appendingPathComponent("workspace.json")
        guard let data = try read(url) else {
            expected.removeValue(forKey: id)
            return WorkspaceConfiguration()
        }
        let value = try JSONDecoder().decode(WorkspaceConfiguration.self, from: data)
        try validate(value, forSaving: false)
        expected[id] = data
        return value
    }
    func saveConfiguration(_ value: WorkspaceConfiguration, account id: String) throws {
        lock.lock()
        defer { lock.unlock() }
        try validate(value)
        let url = try accountRoot(id).appendingPathComponent("workspace.json")
        guard try read(url) == expected[id] else {
            throw BigboardError.message("Workspace settings changed outside BigBoard. Restart before saving.")
        }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let data = try encoder.encode(value)
        try write(data, to: url)
        expected[id] = data
    }
    func validate(_ config: WorkspaceConfiguration) throws {
        try validate(config, forSaving: true)
    }
    private func validate(_ config: WorkspaceConfiguration, forSaving: Bool) throws {
        guard config.schemaVersion == 1 else {
            throw BigboardError.message("This workspace was created by an unsupported BigBoard version.")
        }
        guard config.requestBudget > 0, (0...3650).contains(config.sinceDays) else {
            throw BigboardError.message("Invalid request budget or startup range.")
        }
        for id in config.repositories + config.excludedRepositories + config.groups.values.flatMap({ $0 }) {
            guard Self.validRepository(id) else {
                throw BigboardError.message("Repository must be owner/name: \(id)")
            }
            guard !forSaving || canonicalGitHubRepository(id) == id else {
                throw BigboardError.message("Repository must use lowercase owner/name: \(id)")
            }
        }
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
            guard !forSaving || canonicalGitHubRepository(repo) == repo else {
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
                    if forSaving {
                        let prefix = WorkArea.canonicalPrefix(path)
                        let prefixComponents = prefix.utf8.split(
                            separator: UInt8(ascii: "/"), omittingEmptySubsequences: false)
                        guard prefixComponents.allSatisfy({ !$0.isEmpty && !$0.elementsEqual(".".utf8) })
                        else {
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
    }
    static func validRepository(_ id: String) -> Bool {
        canonicalGitHubRepository(id) != nil
    }
    func loadGrant(_ id: String) throws -> [Repository] {
        guard let data = try read(accountRoot(id).appendingPathComponent("access.json")), !data.isEmpty else {
            return []
        }
        let grant = try decoder().decode(AccessGrant.self, from: data)
        guard grant.accountID == id, grant.repositories.allSatisfy({ Self.validRepository($0.id) }) else {
            throw BigboardError.message("Stored access does not match this GitHub account.")
        }
        return grant.repositories
    }
    func saveGrant(_ repos: [Repository], account id: String) throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        try write(
            encoder.encode(AccessGrant(accountID: id, repositories: repos)),
            to: accountRoot(id).appendingPathComponent("access.json"))
    }
    func invalidateGrant(_ id: String) throws {
        try write(Data(), to: accountRoot(id).appendingPathComponent("access.json"))
    }
    func bookmarks(_ id: String) throws -> [String: Date] {
        guard let data = try read(accountRoot(id).appendingPathComponent("bookmarks.json")) else {
            return [:]
        }
        return try decoder().decode([String: Date].self, from: data)
    }
    func saveBookmarks(_ values: [String: Date], account id: String) throws {
        var merged = try bookmarks(id)
        for (key, value) in values { merged[key] = max(merged[key] ?? .distantPast, value) }
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        try write(encoder.encode(merged), to: accountRoot(id).appendingPathComponent("bookmarks.json"))
    }
    private func decoder() -> JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
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
