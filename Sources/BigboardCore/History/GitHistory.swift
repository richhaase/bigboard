import Foundation
import CryptoKit
import Darwin

private final class RepositoryLock: @unchecked Sendable {
    private var descriptor: Int32
    init(_ descriptor: Int32) { self.descriptor = descriptor }
    func release() {
        if descriptor >= 0 {
            flock(descriptor, LOCK_UN)
            close(descriptor)
            descriptor = -1
        }
    }
    deinit { release() }
}

actor GitHistory {
    let root: URL
    private let limits: HistoryCacheLimits
    private let environment: [String: String]
    private var active: Set<String> = []
    init(
        root: URL, limits: HistoryCacheLimits = HistoryCacheLimits(),
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) {
        self.root = root.standardizedFileURL
        self.limits = limits
        self.environment = environment
    }
    static func validOID(_ value: String) -> Bool {
        [40, 64].contains(value.utf8.count)
            && value.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }
    static func validID(_ id: String) -> Bool {
        canonicalGitHubRepository(id) != nil && !id.hasPrefix("-")
    }
    private func safePath(_ path: URL) throws {
        guard path.standardizedFileURL.path.hasPrefix(root.path + "/") || path.standardizedFileURL == root
        else { throw BigboardError.message("Path is outside Bigboard's history cache.") }
        var current = path.standardizedFileURL
        while current.path.count >= root.path.count {
            if let info = try? FileManager.default.attributesOfItem(atPath: current.path),
                info[.type] as? FileAttributeType == .typeSymbolicLink
            {
                throw BigboardError.message("Bigboard cache cannot contain symbolic links.")
            }
            if current == root { break }
            current.deleteLastPathComponent()
        }
    }
    private func acquire(_ id: String) async throws -> RepositoryLock {
        let directory = root.appendingPathComponent("locks")
        try safePath(directory)
        try FileManager.default.createDirectory(
            at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let name = SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined()
        let path = directory.appendingPathComponent(name + ".lock")
        try safePath(path)
        let descriptor = open(path.path, O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, mode_t(0o600))
        guard descriptor >= 0 else {
            throw BigboardError.message("Could not open the repository cache lock.")
        }
        let lock = RepositoryLock(descriptor)
        var info = stat()
        guard fstat(descriptor, &info) == 0, (info.st_mode & S_IFMT) == S_IFREG, info.st_nlink == 1 else {
            throw BigboardError.message("Repository cache lock is not a regular owned file.")
        }
        let deadline = Date().addingTimeInterval(30)
        while flock(descriptor, LOCK_EX | LOCK_NB) != 0 {
            guard errno == EWOULDBLOCK || errno == EAGAIN else {
                throw BigboardError.message("Could not lock the repository cache.")
            }
            guard Date() < deadline else {
                throw BigboardError.message("Another Bigboard process is using this repository cache.")
            }
            try await Task.sleep(for: .milliseconds(100))
        }
        try Task.checkCancellation()
        return lock
    }
    static let remoteBase = "https://github.com/"
    static func scanRevision(_ repo: Repository) -> String { "refs/remotes/\(repo.remote)/\(repo.branch)" }
    static func validRemote(_ name: String) -> Bool {
        !name.isEmpty && name.utf8.count <= 100 && !name.hasPrefix("-") && !name.contains("..")
            && name.unicodeScalars.allSatisfy {
                CharacterSet.alphanumerics.contains($0) || "-_./".unicodeScalars.contains($0)
            }
    }
    private func gitDirectory(_ repo: Repository) throws -> URL {
        guard Self.validID(repo.id), repo.path.hasPrefix("/"), Self.validRemote(repo.remote) else {
            throw BigboardError.message("Repository clone is not configured.")
        }
        let url = URL(fileURLWithPath: repo.path).standardizedFileURL
        var directory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: url.path, isDirectory: &directory), directory.boolValue else {
            throw BigboardError.message("The local clone at \(repo.path) is missing. Refresh the repository list.")
        }
        return url
    }
    private func checkBranch(_ repo: Repository) async throws {
        guard !repo.branch.isEmpty else {
            throw BigboardError.message("The default branch is unknown. Refresh to fetch it from GitHub.")
        }
        _ = try await GitProcess.run(["check-ref-format", "refs/heads/" + repo.branch])
    }
    static func fetchArguments(_ repo: Repository, gitDirectory: String) -> [String] {
        [
            "--git-dir=" + gitDirectory, "-c", "fetch.writeCommitGraph=false", "-c", "fetch.recurseSubmodules=false",
            "fetch", "--quiet", "--no-tags", "--no-prune", "--no-recurse-submodules", "--no-write-fetch-head",
            "--no-auto-maintenance", "--no-auto-gc", "--refmap=", repo.remote,
            "+refs/heads/\(repo.branch):" + scanRevision(repo),
        ]
    }
    static func batchSSH(configured: String, environment: [String: String]) -> [String] {
        guard configured.isEmpty, environment["GIT_SSH_COMMAND"] == nil, environment["GIT_SSH"] == nil else {
            return []
        }
        return ["-c", "core.sshCommand=ssh -o BatchMode=yes"]
    }
    func refresh(_ repo: Repository) async throws -> Repository {
        guard !active.contains(repo.id) else {
            throw BigboardError.message("Repository operation is already running.")
        }
        active.insert(repo.id)
        defer { active.remove(repo.id) }
        let lock = try await acquire(repo.id)
        defer { lock.release() }
        let directory = try gitDirectory(repo)
        let git = ["--git-dir=" + directory.path]
        let configured =
            (try? await GitProcess.text(git + ["config", "--get", "core.sshCommand"], userEnvironment: environment))
            ?? ""
        let ssh = Self.batchSSH(configured: configured, environment: environment)
        var result = repo
        if result.branch.isEmpty {
            let probe = try await GitProcess.text(
                ssh + git + ["ls-remote", "--symref", repo.remote, "HEAD"], userEnvironment: environment)
            guard let line = probe.split(separator: "\n").first(where: { $0.hasPrefix("ref: refs/heads/") }),
                let name = line.dropFirst("ref: refs/heads/".count).split(separator: "\t").first
            else { throw BigboardError.message("The remote did not report a default branch.") }
            result.branch = String(name)
        }
        try await checkBranch(result)
        _ = try await GitProcess.run(
            ssh + Self.fetchArguments(result, gitDirectory: directory.path), userEnvironment: environment,
            timeout: GitProcess.transferTimeout)
        let revision = try await GitProcess.text(
            git + ["rev-parse", "--verify", "--end-of-options", Self.scanRevision(result) + "^{commit}"])
        guard Self.validOID(revision) else { throw BigboardError.message("Git returned an invalid revision.") }
        result.revision = revision
        result.stale = false
        result.error = ""
        return result
    }
    private func scanPath(_ id: String) -> URL {
        root.appendingPathComponent("scans")
            .appendingPathComponent(
                SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined() + ".json")
    }
    private func fingerprint(
        _ repo: Repository, revision: String, attributes: Data, configuration: WorkspaceConfiguration,
        version: String
    ) throws -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        var hasher = SHA256()
        hasher.update(data: Data("clone-v1\0\(revision)\0\(version)\0\(repo.partial)\0\(repo.shallow)".utf8))
        hasher.update(data: try encoder.encode(configuration))
        hasher.update(data: Data("\(attributes.count):".utf8))
        hasher.update(data: attributes)
        return hasher.finalize().map { String(format: "%02x", $0) }.joined()
    }
    func scan(_ repo: Repository, configuration: WorkspaceConfiguration) async throws -> (
        Repository, [Commit]
    ) {
        guard !active.contains(repo.id) else {
            throw BigboardError.message("Repository operation is already running.")
        }
        active.insert(repo.id)
        defer { active.remove(repo.id) }
        let lock = try await acquire(repo.id)
        defer { lock.release() }
        let directory = try gitDirectory(repo)
        try await checkBranch(repo)
        let git = [
            "--git-dir=" + directory.path, "-c", "core.bare=true", "-c", "mailmap.file=", "-c",
            "core.attributesFile=/dev/null",
        ]
        let ref = Self.scanRevision(repo)
        guard
            let revision = try? await GitProcess.text(
                git + ["rev-parse", "--verify", "--quiet", "--end-of-options", ref + "^{commit}"]),
            Self.validOID(revision)
        else {
            throw BigboardError.message("\(ref) has not been fetched yet. Refresh to fetch it from GitHub.")
        }
        let listing =
            configuration.allFiles
            ? ""
            : try await GitProcess.text(
                git + ["ls-tree", "--name-only", "--end-of-options", revision, "--", ".gitattributes"])
        let attributeData =
            listing.isEmpty
            ? Data()
            : try await GitProcess.run(
                git + ["cat-file", "blob", revision + ":.gitattributes"], limit: HistoryRules.maximumAttributeBytes)
        let version = try await GitProcess.text(["--version"])
        let key = try fingerprint(
            repo, revision: revision, attributes: attributeData, configuration: configuration, version: version)
        let cachePath = scanPath(repo.id)
        try safePath(cachePath)
        func finish(_ records: [Commit], skipped: Int, scannedAt: Date) -> (Repository, [Commit]) {
            var result = repo
            result.revision = revision
            result.scannedAt = scannedAt
            var notes: [String] = []
            if skipped > 0 { notes.append(Self.skippedCommitsMessage(skipped)) }
            if repo.shallow { notes.append("Shallow clone: earlier history is not available locally.") }
            if repo.partial { notes.append("Partial clone: line counts and paths are unknown.") }
            if !notes.isEmpty { result.error = notes.joined(separator: " ") }
            return (result, records)
        }
        if let cache = try? HistoryScanCache.read(
            cachePath, limits: limits, checkpoint: { try Task.checkCancellation() }), cache.schema == 3,
            cache.fingerprint == key
        {
            try Task.checkCancellation()
            return finish(cache.records, skipped: cache.skipped, scannedAt: cache.scannedAt)
        }
        let attributes = try HistoryRules.attributes(
            String(decoding: attributeData, as: UTF8.self), checkpoint: { try Task.checkCancellation() })
        let parser = GitLogParser(repo: repo.id, configuration: configuration, attributes: attributes)
        var args = [
            "-c", "mailmap.blob=" + revision + ":.mailmap",
            "log", "--no-merges", "--root", "-M50%", "-C50%", "-l0", "--no-ext-diff", "--no-textconv",
            "--no-color", "--no-relative", "--no-show-signature", "--diff-algorithm=myers",
            "--no-indent-heuristic", "--ignore-submodules=none",
            "--format=%x00%H%x00%aN%x00%aE%x00%at%x00%(trailers:key=Co-authored-by,valueonly,separator=%x1f)%x00%s%x00",
        ]
        if !repo.partial { args.append("--numstat") }
        args += ["-z", revision, "--"]
        _ = try await GitProcess.run(
            git + args, timeout: GitProcess.transferTimeout,
            stream: { data, checkpoint in try parser.feed(data, checkpoint: checkpoint) })
        var records = try parser.finish()
        if repo.partial {
            for index in records.indices {
                records[index].linesUnknown = true
                records[index].pathsUnknown = true
            }
        }
        try Task.checkCancellation()
        let scannedAt = Date()
        let cache = HistoryScanCache(
            schema: 3, fingerprint: key, scannedAt: scannedAt, records: records, skipped: parser.skipped)
        _ = try? cache.write(to: cachePath, limits: limits, checkpoint: { try Task.checkCancellation() })
        try Task.checkCancellation()
        return finish(records, skipped: parser.skipped, scannedAt: scannedAt)
    }
    static func skippedCommitsMessage(_ count: Int) -> String {
        switch count {
        case 1: "Skipped 1 commit with an unreadable author date."
        default: "Skipped \(count) commits with unreadable author dates."
        }
    }
    func pathURL(repo: Repository, path literal: String) async throws -> URL {
        let lock = try await acquire(repo.id)
        defer { lock.release() }
        let directory = try gitDirectory(repo)
        guard Self.validOID(repo.revision) else {
            throw BigboardError.message("The retained scan revision is unavailable. Refresh and try again.")
        }
        if literal.isEmpty { return URL(string: Self.remoteBase + repo.id)! }
        guard
            literal.split(separator: "/", omittingEmptySubsequences: false)
                .allSatisfy({ !$0.isEmpty && $0 != "." && $0 != ".." && !$0.contains("\0") })
        else { throw BigboardError.message("This repository path is unavailable.") }
        let type = try await GitProcess.text([
            "--git-dir=" + directory.path, "cat-file", "-t", repo.revision + "^{commit}:" + literal,
        ])
        guard ["blob", "tree"].contains(type) else {
            throw BigboardError.message("This path is absent at the retained scan revision.")
        }
        var url = URL(string: Self.remoteBase + repo.id)!
            .appendingPathComponent(type == "tree" ? "tree" : "blob").appendingPathComponent(repo.revision)
        for part in literal.split(separator: "/") { url.appendPathComponent(String(part)) }
        return url
    }
}
