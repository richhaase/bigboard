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
    private var active: Set<String> = []
    init(root: URL, limits: HistoryCacheLimits = HistoryCacheLimits()) {
        self.root = root.standardizedFileURL
        self.limits = limits
    }
    static func validOID(_ value: String) -> Bool {
        [40, 64].contains(value.utf8.count)
            && value.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }
    static func validID(_ id: String) -> Bool {
        canonicalGitHubRepository(id) != nil && !id.hasPrefix("-")
    }
    private func location(_ id: String) throws -> URL {
        guard Self.validID(id) else {
            throw BigboardError.message("Repository must use a valid owner/name identifier.")
        }
        let path = root.appendingPathComponent("github.com").appendingPathComponent(id + ".git")
        try safePath(path)
        return path
    }
    private func safePath(_ path: URL) throws {
        guard path.standardizedFileURL.path.hasPrefix(root.path + "/") || path.standardizedFileURL == root
        else { throw BigboardError.message("Repository is outside Bigboard's cache.") }
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
    private func verify(_ path: URL, id: String) async throws {
        try safePath(path)
        let marker = path.appendingPathComponent("bigboard-cache")
        guard (try? String(contentsOf: marker, encoding: .utf8)) == id + "\n" else {
            throw BigboardError.message("Cache ownership could not be verified for \(id).")
        }
        guard
            let enumerator = FileManager.default.enumerator(
                at: path, includingPropertiesForKeys: [.isSymbolicLinkKey])
        else { throw BigboardError.message("Cannot inspect repository cache.") }
        while let item = enumerator.nextObject() as? URL {
            try Task.checkCancellation()
            if try item.resourceValues(forKeys: [.isSymbolicLinkKey]).isSymbolicLink == true {
                throw BigboardError.message("Repository cache contains a symbolic link.")
            }
        }
        for name in [
            "commondir", "gitdir", ".git", "config.worktree", "shallow", "objects/info/alternates",
            "objects/info/http-alternates",
        ] {
            if FileManager.default.fileExists(atPath: path.appendingPathComponent(name).path) {
                throw BigboardError.message("Cache must be a complete independent bare repository.")
            }
        }
        let configData = try await GitProcess.run(
            ["--git-dir=" + path.path, "config", "--local", "--no-includes", "--null", "--list"],
            limit: 1024 * 1024)
        var origins: [String] = []
        for entry in String(decoding: configData, as: UTF8.self).split(separator: "\0") {
            let pieces = entry.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false)
                .map(String.init)
            let key = pieces[0].lowercased(), value = pieces.count > 1 ? pieces[1] : ""
            if key == "remote.origin.url" { origins.append(value) }
            if key.hasPrefix("include.") || key.hasPrefix("includeif.") || key.hasPrefix("url.")
                || key.hasPrefix("credential.") || key.hasPrefix("http.") || key.hasSuffix(".proxy")
                || ["core.worktree", "core.sshcommand", "core.gitproxy"].contains(key)
                || key.hasPrefix("extensions.") || key.hasSuffix(".promisor")
                || key.hasSuffix(".partialclonefilter")
            {
                throw BigboardError.message("Repository cache has unsupported configuration.")
            }
        }
        guard origins == ["https://github.com/\(id).git"],
            try await GitProcess.text(["--git-dir=" + path.path, "rev-parse", "--is-bare-repository"])
                == "true"
        else { throw BigboardError.message("Repository cache origin or format does not match \(id).") }
    }
    private func initializeCache(_ path: URL, id: String) async throws {
        try FileManager.default.createDirectory(
            at: path, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        _ = try await GitProcess.run(["init", "--bare", "--quiet", "--template=", path.path])
        try Data((id + "\n").utf8).write(to: path.appendingPathComponent("bigboard-cache"), options: .atomic)
        _ = try await GitProcess.run([
            "--git-dir=" + path.path, "config", "remote.origin.url", "https://github.com/\(id).git",
        ])
    }
    private func fetchCache(_ path: URL, repo: Repository, token: String, empty: Bool) async throws -> String
    {
        let prefix = ["--git-dir=" + path.path]
        let remote = "refs/remotes/origin/" + repo.branch, head = "refs/heads/" + repo.branch
        let spec = "+refs/heads/" + repo.branch + ":" + remote
        var oid = ""
        if !empty {
            _ = try await GitProcess.run(
                prefix + [
                    "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head",
                    "--no-auto-maintenance", "--refmap=", "https://github.com/\(repo.id).git", spec,
                ], token: token, timeout: GitProcess.transferTimeout)
            oid = try await GitProcess.text(
                prefix + ["rev-parse", "--verify", "--end-of-options", remote + "^{commit}"])
            guard Self.validOID(oid) else {
                throw BigboardError.message("Git returned an invalid default-branch revision.")
            }
            _ = try await GitProcess.run(prefix + ["update-ref", head, oid])
            _ = try await GitProcess.run(prefix + ["symbolic-ref", "refs/remotes/origin/HEAD", remote])
        }
        for (key, value) in [("remote.origin.fetch", spec), ("remote.origin.tagOpt", "--no-tags")] {
            if (try? await GitProcess.text(prefix + ["config", "--get", key])) != value {
                _ = try await GitProcess.run(prefix + ["config", "--replace-all", key, value])
            }
        }
        _ = try await GitProcess.run(prefix + ["symbolic-ref", "HEAD", head])
        let refs = try await GitProcess.text(prefix + ["for-each-ref", "--format=%(refname)"])
        for ref in refs.split(separator: "\n").map(String.init)
        where empty || ![remote, head, "refs/remotes/origin/HEAD"].contains(ref) {
            _ = try await GitProcess.run(prefix + ["update-ref", "--no-deref", "-d", ref])
        }
        if !empty {
            _ = try await GitProcess.run(
                prefix + ["-c", "gc.auto=6700", "-c", "gc.autoDetach=false", "gc", "--auto", "--quiet"],
                timeout: GitProcess.transferTimeout)
            _ = try await GitProcess.run(
                prefix + ["fsck", "--connectivity-only", "--no-dangling"], timeout: GitProcess.transferTimeout,
                limit: 1024 * 1024)
        }
        return oid
    }
    func refresh(_ repo: Repository, token: String) async throws -> Repository {
        guard !active.contains(repo.id) else {
            throw BigboardError.message("Repository operation is already running.")
        }
        guard !token.isEmpty, token.utf8.allSatisfy({ (33...126).contains($0) }) else {
            throw BigboardError.message("Sign in before refreshing repositories.")
        }
        active.insert(repo.id)
        defer { active.remove(repo.id) }
        let lock = try await acquire(repo.id)
        defer { lock.release() }
        let destination = try location(repo.id)
        guard !repo.branch.isEmpty else { throw BigboardError.message("Repository has no default branch.") }
        _ = try await GitProcess.run(["check-ref-format", "refs/heads/" + repo.branch])
        let manager = FileManager.default
        try manager.createDirectory(
            at: destination.deletingLastPathComponent(), withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        try safePath(destination)
        let exists = manager.fileExists(atPath: destination.path)
        if exists { try await verify(destination, id: repo.id) }
        let probe = try await GitProcess.text(
            [
                "ls-remote", "--symref", "https://github.com/\(repo.id).git", "HEAD",
                "refs/heads/" + repo.branch,
            ], token: token)
        let expected =
            probe.split(separator: "\n")
            .compactMap { line -> String? in
                let parts = line.split(separator: "\t", maxSplits: 1)
                return parts.count == 2 && parts[1] == "refs/heads/" + repo.branch
                    && Self.validOID(String(parts[0])) ? String(parts[0]) : nil
            }
            .first ?? ""
        guard !expected.isEmpty || probe.isEmpty else {
            throw BigboardError.message(
                "The default branch changed during refresh. Refresh repository access and try again.")
        }
        var replace = !exists
        if exists {
            do {
                _ = try await GitProcess.run(
                    ["--git-dir=" + destination.path, "fsck", "--connectivity-only", "--no-dangling"],
                    timeout: GitProcess.transferTimeout, limit: 1024 * 1024)
            } catch is GitCommandFailure { replace = true }
            if !replace {
                let local =
                    (try? await GitProcess.text([
                        "--git-dir=" + destination.path, "rev-parse", "--verify", "HEAD^{commit}",
                    ])) ?? ""
                let branch = try await GitProcess.text([
                    "--git-dir=" + destination.path, "symbolic-ref", "HEAD",
                ])
                if local == expected && branch == "refs/heads/" + repo.branch {
                    var result = repo
                    result.revision = expected
                    result.stale = false
                    result.error = ""
                    return result
                }
            }
        }
        let path =
            replace
            ? destination.deletingLastPathComponent()
                .appendingPathComponent(".bigboard-clone-" + UUID().uuidString) : destination
        do {
            if replace { try await initializeCache(path, id: repo.id) }
            let oid = try await fetchCache(path, repo: repo, token: token, empty: expected.isEmpty)
            if replace {
                try await verify(path, id: repo.id)
                try safePath(destination)
                try Task.checkCancellation()
                if exists {
                    let backup = destination.deletingLastPathComponent()
                        .appendingPathComponent(".bigboard-replaced-" + UUID().uuidString)
                    try manager.moveItem(at: destination, to: backup)
                    do { try manager.moveItem(at: path, to: destination) } catch {
                        try? manager.moveItem(at: backup, to: destination)
                        throw error
                    }
                    try? manager.removeItem(at: backup)
                } else {
                    try manager.moveItem(at: path, to: destination)
                }
            }
            var result = repo
            result.revision = oid
            result.stale = false
            result.error = ""
            return result
        } catch {
            if replace { try? manager.removeItem(at: path) }
            throw error
        }
    }
    private func scanPath(_ id: String) -> URL {
        root.appendingPathComponent("scans")
            .appendingPathComponent(
                SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined() + ".json")
    }
    private func externalScanInputs(_ path: URL) async throws -> [Data] {
        let prefix = ["--git-dir=" + path.path]
        let config = try await GitProcess.run(
            prefix + ["config", "--local", "--no-includes", "--null", "--list"], limit: 1024 * 1024)
        var inputs: [Data] = []
        func appendFile(_ key: String, _ value: String) throws {
            let name = (value as NSString).expandingTildeInPath
            let url = (name.hasPrefix("/") ? URL(fileURLWithPath: name) : path.appendingPathComponent(name))
                .resolvingSymlinksInPath()
            inputs.append(Data((key + "\0" + url.path).utf8))
            if url.path == "/dev/null" {
                inputs.append(Data())
                return
            }
            guard FileManager.default.fileExists(atPath: url.path) else {
                inputs.append(Data("missing".utf8))
                return
            }
            let info = try FileManager.default.attributesOfItem(atPath: url.path)
            guard info[.type] as? FileAttributeType == .typeRegular,
                (info[.size] as? NSNumber)?.intValue ?? 0 <= 16 * 1024 * 1024
            else { throw BigboardError.message("Git scan configuration references an unsupported file.") }
            inputs.append(try Data(contentsOf: url))
        }
        for entry in String(decoding: config, as: UTF8.self).split(separator: "\0") {
            let pieces = entry.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false)
                .map(String.init)
            guard pieces.count == 2,
                ["mailmap.file", "core.attributesfile", "diff.orderfile"].contains(pieces[0].lowercased()),
                !pieces[1].isEmpty
            else { continue }
            let value = try await GitProcess.run(
                prefix + ["config", "--local", "--no-includes", "--null", "--path", "--get", pieces[0]],
                limit: 1024 * 1024)
            let name = String(decoding: value, as: UTF8.self)
                .trimmingCharacters(in: CharacterSet(charactersIn: "\0"))
            try appendFile(pieces[0], name)
        }
        for key in ["GIT_ATTR_SYSTEM", "GIT_ATTR_GLOBAL"] {
            let data = try await GitProcess.run(prefix + ["var", key], limit: 1024 * 1024)
            var name = String(decoding: data, as: UTF8.self)
            if name.hasSuffix("\n") { name.removeLast() }
            if !name.isEmpty { try appendFile(key, name) }
        }
        return inputs
    }
    private func fingerprint(
        _ path: URL, revision: String, configuration: WorkspaceConfiguration, version: String,
        externalInputs: [Data]
    ) throws -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        var hasher = SHA256()
        hasher.update(data: Data((revision + "\0" + version).utf8))
        hasher.update(data: try encoder.encode(configuration))
        for data in externalInputs {
            hasher.update(data: Data("\(data.count):".utf8))
            hasher.update(data: data)
        }
        let manager = FileManager.default
        guard
            let enumerator = manager.enumerator(
                at: path,
                includingPropertiesForKeys: [.contentModificationDateKey, .fileSizeKey, .isRegularFileKey])
        else { throw BigboardError.message("Cannot inspect scan inputs.") }
        var entries: [URL] = []
        while let item = enumerator.nextObject() as? URL {
            try Task.checkCancellation()
            entries.append(item)
        }
        entries.sort { $0.path.utf8.lexicographicallyPrecedes($1.path.utf8) }
        for item in entries {
            try Task.checkCancellation()
            let values = try item.resourceValues(forKeys: [
                .contentModificationDateKey, .fileSizeKey, .isRegularFileKey,
            ])
            guard values.isRegularFile == true else { continue }
            hasher.update(
                data: Data(
                    "\(item.path):\(values.fileSize ?? 0):\(values.contentModificationDate?.timeIntervalSince1970 ?? 0)"
                        .utf8))
            if !item.path.hasPrefix(path.appendingPathComponent("objects").path + "/") {
                hasher.update(data: try Data(contentsOf: item))
            }
        }
        return hasher.finalize().map { String(format: "%02x", $0) }.joined()
    }
    private func scanFingerprint(
        _ path: URL, revision: String, configuration: WorkspaceConfiguration, version: String
    ) async throws -> String? {
        do {
            return try fingerprint(
                path, revision: revision, configuration: configuration, version: version,
                externalInputs: try await externalScanInputs(path))
        } catch {
            try Task.checkCancellation()
            return nil
        }
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
        let path = try location(repo.id)
        try await verify(path, id: repo.id)
        let prefix = ["--git-dir=" + path.path]
        let refs = try await GitProcess.text(prefix + ["for-each-ref", "--format=%(refname)"])
        if refs.isEmpty {
            var result = repo
            result.revision = ""
            result.scannedAt = repo.scannedAt ?? Date()
            return (result, [])
        }
        let revision = try await GitProcess.text(prefix + ["rev-parse", "--verify", "HEAD^{commit}"])
        guard Self.validOID(revision) else {
            throw BigboardError.message("Repository has an invalid scan revision.")
        }
        let version = try await GitProcess.text(["--version"])
        let key = try await scanFingerprint(
            path, revision: revision, configuration: configuration, version: version)
        let cachePath = scanPath(repo.id)
        try safePath(cachePath)
        if let key,
            let cache = try? HistoryScanCache.read(
                cachePath, limits: limits, checkpoint: { try Task.checkCancellation() }), cache.schema == 2,
            cache.fingerprint == key
        {
            try Task.checkCancellation()
            var result = repo
            result.revision = revision
            result.scannedAt = cache.scannedAt
            if let skipped = cache.skipped, skipped > 0 { result.error = Self.skippedCommitsMessage(skipped) }
            return (result, cache.records)
        }
        let attributes: [AttributeRule]
        let listing =
            configuration.allFiles
            ? ""
            : try await GitProcess.text(
                prefix + ["ls-tree", "--name-only", "--end-of-options", revision, "--", ".gitattributes"])
        if listing.isEmpty {
            attributes = []
        } else {
            let data = try await GitProcess.run(
                prefix + ["cat-file", "blob", revision + ":.gitattributes"],
                limit: HistoryRules.maximumAttributeBytes)
            attributes = try HistoryRules.attributes(
                String(decoding: data, as: UTF8.self), checkpoint: { try Task.checkCancellation() })
        }
        let parser = GitLogParser(repo: repo.id, configuration: configuration, attributes: attributes)
        let args = [
            "log", "--no-merges", "--root", "-M50%", "-C50%", "-l0", "--no-ext-diff", "--no-textconv",
            "--no-color", "--no-relative", "--no-show-signature", "--diff-algorithm=myers",
            "--no-indent-heuristic", "--ignore-submodules=none",
            "--format=%x00%H%x00%aN%x00%aE%x00%at%x00%(trailers:key=Co-authored-by,valueonly,separator=%x1f)%x00%s%x00",
            "--numstat", "-z", revision, "--",
        ]
        _ = try await GitProcess.run(
            prefix + args, timeout: GitProcess.transferTimeout, stream: { data, checkpoint in try parser.feed(data, checkpoint: checkpoint) })
        let records = try parser.finish()
        try Task.checkCancellation()
        let after = try await GitProcess.text(prefix + ["rev-parse", "--verify", "HEAD^{commit}"])
        var result = repo
        result.revision = after == revision ? revision : ""
        result.scannedAt = Date()
        if parser.skipped > 0 { result.error = Self.skippedCommitsMessage(parser.skipped) }
        if let key, after == revision,
            try await scanFingerprint(
                path, revision: revision, configuration: configuration, version: version) == key
        {
            let cache = HistoryScanCache(
                schema: 2, fingerprint: key, scannedAt: result.scannedAt!, records: records,
                skipped: parser.skipped)
            _ = try? cache.write(to: cachePath, limits: limits, checkpoint: { try Task.checkCancellation() })
        }
        try Task.checkCancellation()
        return (result, records)
    }
    static func skippedCommitsMessage(_ count: Int) -> String {
        switch count {
        case 1: "Skipped 1 commit with an unreadable author date."
        default: "Skipped \(count) commits with unreadable author dates."
        }
    }
    func cachedRepositories() async throws -> [Repository] {
        let host = root.appendingPathComponent("github.com")
        try safePath(host)
        guard FileManager.default.fileExists(atPath: host.path) else { return [] }
        var result: [Repository] = []
        for owner in try FileManager.default.contentsOfDirectory(
            at: host, includingPropertiesForKeys: [.isDirectoryKey])
        {
            try safePath(owner)
            guard try owner.resourceValues(forKeys: [.isDirectoryKey]).isDirectory == true else { continue }
            for path in try FileManager.default.contentsOfDirectory(
                at: owner, includingPropertiesForKeys: [.isDirectoryKey]) where path.pathExtension == "git"
            {
                let id = owner.lastPathComponent + "/" + path.deletingPathExtension().lastPathComponent
                let lock = try await acquire(id)
                defer { lock.release() }
                try await verify(path, id: id)
                let ref = try await GitProcess.text(["--git-dir=" + path.path, "symbolic-ref", "HEAD"])
                result.append(Repository(id: id, branch: String(ref.dropFirst("refs/heads/".count))))
            }
        }
        return result.sorted { Analysis.literalLess($0.id, $1.id) }
    }
    func remove(_ id: String) async throws {
        guard !active.contains(id) else {
            throw BigboardError.message("Repository operation is already running.")
        }
        active.insert(id)
        defer { active.remove(id) }
        let lock = try await acquire(id)
        defer { lock.release() }
        let path = try location(id)
        try await verify(path, id: id)
        try safePath(path)
        try Task.checkCancellation()
        try FileManager.default.removeItem(at: path)
        let scan = scanPath(id)
        try safePath(scan)
        if FileManager.default.fileExists(atPath: scan.path) { try FileManager.default.removeItem(at: scan) }
    }
    func pathURL(repo: Repository, path literal: String) async throws -> URL {
        let lock = try await acquire(repo.id)
        defer { lock.release() }
        let directory = try location(repo.id)
        try await verify(directory, id: repo.id)
        guard Self.validOID(repo.revision) else {
            throw BigboardError.message("The retained scan revision is unavailable. Refresh and try again.")
        }
        if literal.isEmpty { return URL(string: "https://github.com/" + repo.id)! }
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
        var url = URL(string: "https://github.com/" + repo.id)!
            .appendingPathComponent(type == "tree" ? "tree" : "blob").appendingPathComponent(repo.revision)
        for part in literal.split(separator: "/") { url.appendPathComponent(String(part)) }
        return url
    }
}
