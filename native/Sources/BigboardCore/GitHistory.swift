import Foundation
import CryptoKit
import Darwin

struct GitCommandFailure: LocalizedError, Sendable {
    var status: Int32
    var detail: String
    var errorDescription: String? { detail }
}
private final class ProcessControl: @unchecked Sendable {
    private let lock = NSLock()
    private var process: Process?
    private var group: Int32 = 0
    private var cancelled = false
    private var timedOut = false
    private var finished = false
    func launch(_ process: Process) throws {
        lock.lock(); defer { lock.unlock() }
        guard !cancelled else { throw CancellationError() }
        self.process = process
        try process.run()
        let pid = process.processIdentifier
        let actual = getpgid(pid)
        guard actual == pid || actual == -1 else { process.terminate(); throw BigboardError.message("Git could not launch in an isolated process group.") }
        group = pid
    }
    func stop(timedOut: Bool = false) {
        lock.lock()
        guard !finished else { lock.unlock(); return }
        cancelled = true; self.timedOut = self.timedOut || timedOut
        let group = self.group
        if group > 0 { kill(-group, SIGTERM) }
        lock.unlock()
        guard group > 0 else { return }
        DispatchQueue.global().asyncAfter(deadline: .now() + 1) { [self] in
            lock.lock(); defer { lock.unlock() }
            if !finished && self.group == group { kill(-group, SIGKILL) }
        }
    }
    func finish() { lock.lock(); finished = true; lock.unlock() }
    func checkpoint() throws {
        lock.lock(); defer { lock.unlock() }
        if timedOut { throw BigboardError.message("Git did not finish within the operation timeout.") }
        if cancelled { throw CancellationError() }
    }
    var isCancelled: Bool { lock.lock(); defer { lock.unlock() }; return cancelled }
    var didTimeOut: Bool { lock.lock(); defer { lock.unlock() }; return timedOut }
}
private final class ProcessRead: @unchecked Sendable {
    var bytes = Data()
    var error: (any Error)?
}
enum GitExecutable {
    static func select(environment: [String: String] = ProcessInfo.processInfo.environment, executable: (String) -> Bool = { path in
        var directory: ObjCBool = false
        return FileManager.default.fileExists(atPath: path, isDirectory: &directory) && !directory.boolValue && FileManager.default.isExecutableFile(atPath: path)
    }) throws -> URL {
        let directories = (environment["PATH"] ?? "").split(separator: ":").map(String.init) + ["/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"]
        for directory in directories where directory.hasPrefix("/") {
            let candidate = URL(fileURLWithPath: directory).appendingPathComponent("git")
            if executable(candidate.path) { return candidate }
        }
        throw BigboardError.message("Git is unavailable. Install Git with Homebrew or Xcode Command Line Tools, then reopen Bigboard.")
    }
}
enum GitProcess {
    static func executable() throws -> URL {
        try GitExecutable.select()
    }
    static func environment(_ inherited: [String: String] = ProcessInfo.processInfo.environment) -> [String: String] {
        var environment = inherited.filter { ["HOME", "TMPDIR", "XDG_CONFIG_HOME"].contains($0.key) }
        environment.merge(["PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1", "GIT_TERMINAL_PROMPT": "0", "GIT_OPTIONAL_LOCKS": "0", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1", "GIT_ASKPASS": "", "SSH_ASKPASS": "", "SSH_ASKPASS_REQUIRE": "never", "LC_ALL": "C"]) { _, new in new }
        return environment
    }
    static func run(_ arguments: [String], directory: URL? = nil, token: String = "", timeout: TimeInterval = 120, limit: Int = 256 * 1024 * 1024, stream: (@Sendable (Data, @Sendable () throws -> Void) throws -> Void)? = nil) async throws -> Data {
        let executable = try executable()
        var pinned = ["-c", "core.quotePath=false", "-c", "log.showRoot=true", "-c", "log.showSignature=false", "-c", "color.ui=false", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "credential.interactive=false", "-c", "credential.helper=", "-c", "http.followRedirects=false", "-c", "http.sslVerify=true", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=never"]
        if !token.isEmpty { pinned += ["--config-env=http.https://github.com/.extraHeader=BIGBOARD_GIT_AUTHORIZATION"] }
        let gitDirectory = arguments.first(where: { $0.hasPrefix("--git-dir=") }).map { URL(fileURLWithPath: String($0.dropFirst("--git-dir=".count))) }
        var environment = Self.environment()
        if !token.isEmpty { environment["BIGBOARD_GIT_AUTHORIZATION"] = "Authorization: Basic " + Data("x-access-token:\(token)".utf8).base64EncodedString() }
        return try await execute(executable, arguments: pinned + arguments, environment: environment, directory: directory ?? gitDirectory ?? URL(fileURLWithPath: "/"), timeout: timeout, limit: limit, authenticated: !token.isEmpty, stream: stream)
    }
    static func execute(_ executable: URL, arguments: [String], environment: [String: String], directory: URL, timeout: TimeInterval = 120, limit: Int = 256 * 1024 * 1024, authenticated: Bool = false, stream: (@Sendable (Data, @Sendable () throws -> Void) throws -> Void)? = nil) async throws -> Data {
        let control = ProcessControl()
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            return try await withCheckedThrowingContinuation { continuation in
                DispatchQueue.global(qos: .userInitiated).async {
                    let process = Process(), output = Pipe(), diagnostic = Pipe()
                    process.executableURL = executable; process.arguments = arguments
                    process.currentDirectoryURL = directory; process.environment = environment
                    process.standardOutput = output; process.standardError = diagnostic; process.standardInput = FileHandle.nullDevice
                    do { try control.launch(process) } catch { continuation.resume(throwing: error); return }
                    let timer = DispatchSource.makeTimerSource(queue: .global())
                    timer.schedule(deadline: .now() + timeout)
                    timer.setEventHandler { control.stop(timedOut: true) }; timer.resume()
                    let group = DispatchGroup(), out = ProcessRead(), err = ProcessRead()
                    for (pipe, result, maximum, callback) in [(output, out, limit, stream), (diagnostic, err, 64 * 1024, nil)] {
                        group.enter()
                        DispatchQueue.global(qos: .userInitiated).async {
                            defer { group.leave(); try? pipe.fileHandleForReading.close() }
                            do {
                                var count = 0
                                while let chunk = try pipe.fileHandleForReading.read(upToCount: 64 * 1024), !chunk.isEmpty {
                                    try control.checkpoint()
                                    if let callback { try callback(chunk, { try control.checkpoint() }) }
                                    else {
                                        guard chunk.count <= maximum - count else { throw BigboardError.message("Git output exceeded the safety limit.") }
                                        count += chunk.count; result.bytes.append(chunk)
                                    }
                                    try control.checkpoint()
                                }
                            } catch { result.error = error; control.stop() }
                        }
                    }
                    process.waitUntilExit(); group.wait(); timer.cancel(); control.finish()
                    let readerErrors = [out.error, err.error].compactMap { $0 }
                    if control.didTimeOut { continuation.resume(throwing: BigboardError.message("Git did not finish within the operation timeout.")) }
                    else if let error = readerErrors.first(where: { !($0 is CancellationError) }) ?? readerErrors.first { continuation.resume(throwing: error) }
                    else if control.isCancelled { continuation.resume(throwing: CancellationError()) }
                    else if process.terminationStatus != 0 {
                        if authenticated, let failure = authenticatedFailure(String(decoding: err.bytes, as: UTF8.self)) { continuation.resume(throwing: failure); return }
                        let detail = !authenticated ? String(decoding: err.bytes, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines) : "Authenticated Git request failed. Check repository access and try again."
                        continuation.resume(throwing: GitCommandFailure(status: process.terminationStatus, detail: detail.isEmpty ? "Git command failed (\(process.terminationStatus))." : detail))
                    } else { continuation.resume(returning: out.bytes) }
                }
            }
        } onCancel: { control.stop() }
    }
    static func authenticatedFailure(_ diagnostic: String) -> GitHubFailure? {
        let value = diagnostic.lowercased()
        if value.contains("rate limit") || value.contains("error: 429") || value.contains("http 429") { return .rateLimited }
        if value.contains("error: 401") || value.contains("http 401") || value.contains("authentication failed") || value.contains("invalid username or token") { return .auth }
        if value.contains("error: 403") || value.contains("http 403") || value.contains("permission denied") || value.contains("access denied") { return .permission }
        if value.contains("error: 404") || value.contains("http 404") || value.contains("repository not found") { return .notFound }
        return nil
    }
    static func text(_ arguments: [String], directory: URL? = nil, token: String = "") async throws -> String { String(decoding: try await run(arguments, directory: directory, token: token), as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines) }
}

struct AttributeRule: Sendable { var pattern: String; var generated: Bool }
enum AttributeDecision: Sendable { case generated, included, unspecified }
enum HistoryRules {
    static let maximumAttributeBytes = 4 * 1024 * 1024
    static let maximumAttributeRules = 20_000
    static let maximumAttributeWork = 250_000_000
    static let ignoredDirectories: Set<String> = ["vendor", "node_modules", "dist", "build", ".next", "target", ".yarn", ".venv", "__pycache__", "Pods", "Carthage"]
    static let ignoredGlobs = ["*.min.js", "*.min.css", "*.map", "*.snap", "*.lock", "*.pb.go", "*_pb2.py", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "Cargo.lock", "composer.lock", "Gemfile.lock", "poetry.lock"]
    static let bulkExtensions: Set<String> = ["json", "ndjson", "jsonl", "yaml", "yml", "csv", "tsv", "xml", "po", "pot", "svg"]
    static func glob(_ pattern: String, _ value: String) -> Bool { fnmatch(pattern, value, FNM_PATHNAME) == 0 }
    static func attributes(_ contents: String, byteLimit: Int = maximumAttributeBytes, ruleLimit: Int = maximumAttributeRules, checkpoint: @Sendable () throws -> Void = {}) throws -> [AttributeRule] {
        try checkpoint()
        guard contents.utf8.count <= byteLimit else { throw BigboardError.message("Repository attributes exceeded the byte limit.") }
        var rules: [AttributeRule] = []
        for line in contents.split(separator: "\n") {
            try checkpoint()
            let fields = line.split(whereSeparator: \.isWhitespace).map(String.init)
            guard fields.count > 1, let pattern = fields.first, !pattern.hasPrefix("#"), !pattern.hasPrefix("!"), !pattern.hasSuffix("/") else { continue }
            var generated: Bool?
            for raw in fields.dropFirst() {
                try checkpoint()
                if raw.hasPrefix("!") { continue }
                let pieces = raw.split(separator: "=", maxSplits: 1).map(String.init)
                var name = pieces[0], value = true
                if name.hasPrefix("-") { name.removeFirst(); value = false }
                else if pieces.count == 2 { guard ["true", "false", "0", "1"].contains(pieces[1]) else { continue }; value = pieces[1] == "true" || pieces[1] == "1" }
                if ["linguist-generated", "linguist-vendored"].contains(name), generated == nil || value { generated = value }
            }
            if let generated {
                guard rules.count < ruleLimit else { throw BigboardError.message("Repository attributes exceeded the rule limit.") }
                rules.append(AttributeRule(pattern: pattern, generated: generated))
            }
        }
        return rules
    }
    static func pathMatches(_ pattern: String, _ path: String) throws -> Bool {
        var remainingWork = maximumAttributeWork
        return try pathMatches(pattern, path, remainingWork: &remainingWork, checkpoint: {})
    }
    private static func pathMatches(_ pattern: String, _ path: String, remainingWork: inout Int, checkpoint: @Sendable () throws -> Void) throws -> Bool {
        try checkpoint()
        let patternBytes = pattern.utf8.count, pathBytes = path.utf8.count
        guard patternBytes <= 64 * 1024, pathBytes <= 64 * 1024 else { throw BigboardError.message("Repository attribute matching exceeded the safety limit.") }
        let pathComponents = path.split(separator: "/")
        let patternComponents = pattern.trimmingCharacters(in: CharacterSet(charactersIn: "/")).split(separator: "/")
        let work = pattern.contains("/") ? (patternBytes + 1) * (pathComponents.count + 1) + (pathBytes + 1) * patternComponents.count : patternBytes + pathBytes + 1
        guard work <= 1_000_000 else { throw BigboardError.message("Repository attribute matching exceeded the safety limit.") }
        guard work <= remainingWork else { throw BigboardError.message("Repository attribute matching exceeded the scan work limit.") }
        remainingWork -= work
        let p = patternComponents.map(String.init)
        let s = pathComponents.map(String.init)
        guard p.allSatisfy({ $0.utf8.count <= 4096 }), s.allSatisfy({ $0.utf8.count <= 4096 }) else { throw BigboardError.message("Repository attribute matching exceeded the safety limit.") }
        if !pattern.contains("/") { return glob(pattern, s.last ?? "") }
        var reachable = [Bool](repeating: false, count: s.count + 1)
        reachable[0] = true
        for component in p {
            try checkpoint()
            if component == "**" {
                for j in s.indices { reachable[j + 1] = reachable[j + 1] || reachable[j] }
            } else {
                for j in s.indices.reversed() { reachable[j + 1] = reachable[j] && glob(component, s[j]) }
                reachable[0] = false
            }
        }
        return reachable[s.count]
    }
    static func attributeDecision(_ path: String, attributes: [AttributeRule], remainingWork: inout Int, checkpoint: @Sendable () throws -> Void) throws -> AttributeDecision {
        for attribute in attributes.reversed() {
            try checkpoint()
            if try pathMatches(attribute.pattern, path, remainingWork: &remainingWork, checkpoint: checkpoint) { return attribute.generated ? .generated : .included }
        }
        return .unspecified
    }
    static func exclusion(_ file: FileChange, allFiles: Bool, attribute: AttributeDecision) -> String {
        if allFiles { return "" }
        if attribute == .generated { return "attribute" }
        if attribute == .included { return "" }
        if file.path.split(separator: "/").contains(where: { ignoredDirectories.contains(String($0)) }) || ignoredGlobs.contains(where: { glob($0, String(file.path.split(separator: "/").last ?? "")) }) { return "path" }
        if !file.binary && file.added + file.removed >= 2000 && bulkExtensions.contains((file.path as NSString).pathExtension.lowercased()) { return "bulk" }
        return ""
    }
    static func normalizedEmail(_ raw: String) -> String {
        var value = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if let start = value.lastIndex(of: "<"), let end = value.lastIndex(of: ">"), start < end { value = String(value[value.index(after: start)..<end]) }
        return value.trimmingCharacters(in: CharacterSet(charactersIn: "<> "))
    }
    static func isAI(_ raw: String, extra: [String]) -> Bool {
        let email = normalizedEmail(raw)
        if extra.contains(where: { let e = $0.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(); return !e.isEmpty && (e.hasPrefix("@") ? email.hasSuffix(e) : email == e) }) { return true }
        if ["noreply@anthropic.com", "noreply@openai.com", "hi@cursor.com", "hi@cursor.sh", "cursoragent@cursor.com", "copilot@github.com", "devin@cognition.ai", "noreply@aider.chat", "bot@codium.ai"].contains(email) { return true }
        if email.hasSuffix("@users.noreply.github.com") {
            var local = String(email.split(separator: "@")[0].split(separator: "+").last ?? "")
            if local.hasSuffix("[bot]") { local = String(local.dropLast(5)) }
            return ["copilot", "copilot-swe-agent", "claude", "devin-ai-integration", "google-labs-jules", "cursoragent"].contains(local)
        }
        return false
    }
    static func isBot(name: String, email: String, extra: [String]) -> Bool {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(), email = normalizedEmail(email)
        if extra.contains(where: { let e = $0.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(); return !e.isEmpty && (e.hasPrefix("@") ? email.hasSuffix(e) : e.contains("@") ? email == e : name == e) }) { return true }
        if name.contains("[bot]") || email.split(separator: "@").first?.contains("[bot]") == true { return true }
        let base = name.hasSuffix(" bot") ? String(name.dropLast(4)) : name
        return ["dependabot", "dependabot-preview", "renovate", "github-actions", "snyk-bot", "greenkeeper", "imgbot", "mergify", "allcontributors", "pre-commit-ci", "codecov"].contains(base)
    }
}

struct HistoryCacheLimits: Sendable {
    var cacheBytes = 16 * 1024 * 1024
}

struct HistoryScanCache: Codable {
    var schema: Int
    var fingerprint: String
    var scannedAt: Date
    var records: [Commit]
    static func read(_ url: URL, limits: HistoryCacheLimits = HistoryCacheLimits(), checkpoint: () throws -> Void = {}) throws -> HistoryScanCache {
        try checkpoint()
        let descriptor = open(url.path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK)
        guard descriptor >= 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? handle.close() }
        var info = stat()
        let maximum = max(0, limits.cacheBytes)
        guard fstat(descriptor, &info) == 0, info.st_mode & S_IFMT == S_IFREG, info.st_size >= 0, info.st_size <= Int64(maximum) else { throw BigboardError.message("Git history cache exceeded the file safety limit.") }
        var data = Data()
        while true {
            try checkpoint()
            let count = min(64 * 1024, maximum - data.count) + 1
            guard let chunk = try handle.read(upToCount: count), !chunk.isEmpty else { break }
            guard chunk.count <= maximum - data.count else { throw BigboardError.message("Git history cache exceeded the file safety limit.") }
            data.append(chunk)
        }
        try checkpoint()
        let cache = try JSONDecoder().decode(HistoryScanCache.self, from: data)
        try checkpoint()
        return cache
    }
    func encoded(limits: HistoryCacheLimits = HistoryCacheLimits(), checkpoint: () throws -> Void = {}) throws -> Data? {
        try checkpoint()
        var remaining = max(0, limits.cacheBytes)
        func reserve(_ count: Int) -> Bool {
            guard count <= remaining else { return false }
            remaining -= count; return true
        }
        func reserveString(_ value: String) -> Bool {
            let bytes = value.utf8.count
            guard bytes <= remaining / 6 else { return false }
            return reserve(bytes * 6)
        }
        guard reserve(256), reserveString(fingerprint) else { return nil }
        for record in records {
            try checkpoint()
            guard reserve(512) else { return nil }
            for value in [record.id, record.repo, record.author, record.email, record.subject] { guard reserveString(value) else { return nil } }
            for file in record.files {
                try checkpoint()
                guard reserve(192) else { return nil }
                for value in [file.path, file.previousPath, file.excludedReason] { guard reserveString(value) else { return nil } }
            }
        }
        try checkpoint()
        let data = try JSONEncoder().encode(self)
        try checkpoint()
        guard data.count <= limits.cacheBytes else { return nil }
        return data
    }
    @discardableResult func write(to url: URL, limits: HistoryCacheLimits = HistoryCacheLimits(), checkpoint: () throws -> Void = {}) throws -> Bool {
        guard let data = try encoded(limits: limits, checkpoint: checkpoint) else { return false }
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        try checkpoint()
        try data.write(to: url, options: .atomic)
        return true
    }
}

final class GitLogParser: @unchecked Sendable {
    private var buffer = Data()
    private var offset = 0
    private var metadata: [String] = []
    private var pendingFile: FileChange?
    private var awaitingPreviousPath = false
    private let repo: String
    private let configuration: WorkspaceConfiguration
    private let attributes: [AttributeRule]
    private var remainingAttributeWork: Int
    private var attributeDecisions: [Data: AttributeDecision] = [:]
    private var cachedPathBytes = 0
    private var failure: (any Error)?
    private var identityMap: [String: (String, String)] = [:]
    private(set) var records: [Commit] = []
    init(repo: String, configuration: WorkspaceConfiguration, attributes: [AttributeRule] = [], matchingWorkLimit: Int = HistoryRules.maximumAttributeWork) {
        self.repo = repo; self.configuration = configuration; self.attributes = attributes
        remainingAttributeWork = max(0, matchingWorkLimit)
        for (email, rule) in configuration.identities { for alias in [email] + rule.aliases { identityMap[alias.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()] = (email.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(), rule.name.trimmingCharacters(in: .whitespacesAndNewlines)) } }
    }
    func feed(_ data: Data, checkpoint: @Sendable () throws -> Void = {}) throws {
        if let failure { throw failure }
        do {
            try checkpoint()
            guard configuration.allFiles || attributes.count <= HistoryRules.maximumAttributeRules else { throw BigboardError.message("Repository attributes exceeded the rule limit.") }
            buffer.append(data)
            while let end = buffer[offset...].firstIndex(of: 0) {
                try checkpoint()
                guard end - offset <= 4 * 1024 * 1024 else { throw BigboardError.message("Git history contains an oversized field.") }
                let token = String(decoding: buffer[offset..<end], as: UTF8.self)
                offset = end + 1
                try consume(token, checkpoint: checkpoint)
            }
            guard buffer.count - offset <= 4 * 1024 * 1024 else { throw BigboardError.message("Git history contains an oversized field.") }
            if offset > 1024 * 1024 { buffer.removeSubrange(0..<offset); offset = 0 }
            try checkpoint()
        } catch { failure = error; throw error }
    }
    func finish() throws -> [Commit] {
        if let failure { throw failure }
        guard buffer.count == offset, metadata.isEmpty, pendingFile == nil else { throw BigboardError.message("Git history ended before a complete record was received.") }
        return records
    }
    private func consume(_ raw: String, checkpoint: @Sendable () throws -> Void) throws {
        if !metadata.isEmpty {
            metadata.append(raw)
            if metadata.count == 6 {
                guard let seconds = Int64(metadata[3]) else { throw BigboardError.message("Git history contains an invalid author date.") }
                var name = metadata[1].trimmingCharacters(in: .whitespacesAndNewlines), email = metadata[2].trimmingCharacters(in: .whitespacesAndNewlines)
                if let mapped = identityMap[email.lowercased()] { email = mapped.0; if !mapped.1.isEmpty { name = mapped.1 } }
                let ai = HistoryRules.isAI(email, extra: configuration.aiIdentities) || metadata[4].split(separator: "\u{1f}").contains { HistoryRules.isAI(String($0), extra: configuration.aiIdentities) }
                let commit = Commit(id: metadata[0], repo: repo, author: name, email: email, date: Date(timeIntervalSince1970: Double(seconds)), subject: metadata[5], ai: ai, bot: HistoryRules.isBot(name: name, email: email, extra: configuration.botIdentities))
                records.append(commit)
                metadata = []
            }
            return
        }
        if var file = pendingFile {
            if awaitingPreviousPath { file.previousPath = raw; pendingFile = file; awaitingPreviousPath = false }
            else { file.path = raw; pendingFile = nil; try append(file, checkpoint: checkpoint) }
            return
        }
        let token = String(raw.drop(while: { $0 == "\n" }))
        if token.isEmpty { return }
        if !token.contains("\t") {
            guard GitHistory.validOID(token) else { throw BigboardError.message("Git history contains an invalid commit identifier.") }
            metadata = [token]; return
        }
        let pieces = token.split(separator: "\t", maxSplits: 2, omittingEmptySubsequences: false).map(String.init)
        guard pieces.count == 3, !records.isEmpty else { throw BigboardError.message("Git history contains an invalid file-change record.") }
        var file = FileChange(path: pieces[2])
        if pieces[0] == "-" || pieces[1] == "-" { file.binary = true }
        else {
            guard let added = Int(pieces[0]), let removed = Int(pieces[1]), added >= 0, removed >= 0 else { throw BigboardError.message("Git history contains invalid line counts.") }
            file.added = added; file.removed = removed
        }
        if file.path.isEmpty { pendingFile = file; awaitingPreviousPath = true } else { try append(file, checkpoint: checkpoint) }
    }
    private func append(_ original: FileChange, checkpoint: @Sendable () throws -> Void) throws {
        guard !records.isEmpty, !original.path.isEmpty else { throw BigboardError.message("Git history contains an empty path.") }
        var file = original
        var attribute = AttributeDecision.unspecified
        if !configuration.allFiles, !attributes.isEmpty {
            let bytes = file.path.utf8.count
            guard bytes <= 64 * 1024 else { throw BigboardError.message("Repository attribute matching exceeded the safety limit.") }
            let key = Data(file.path.utf8)
            if let cached = attributeDecisions[key] { attribute = cached }
            else {
                attribute = try HistoryRules.attributeDecision(file.path, attributes: attributes, remainingWork: &remainingAttributeWork, checkpoint: checkpoint)
                if attributeDecisions.count < 16_384, bytes <= 4 * 1024 * 1024 - cachedPathBytes {
                    attributeDecisions[key] = attribute
                    cachedPathBytes += bytes
                }
            }
        }
        file.excludedReason = HistoryRules.exclusion(file, allFiles: configuration.allFiles, attribute: attribute)
        let index = records.count - 1
        if file.excludedReason.isEmpty { records[index].added += file.added; records[index].removed += file.removed }
        records[index].files.append(file)
    }
}

private final class RepositoryLock: @unchecked Sendable {
    private var descriptor: Int32
    init(_ descriptor: Int32) { self.descriptor = descriptor }
    func release() { if descriptor >= 0 { flock(descriptor, LOCK_UN); close(descriptor); descriptor = -1 } }
    deinit { release() }
}

actor GitHistory {
    let root: URL
    private let limits: HistoryCacheLimits
    private var active: Set<String> = []
    init(root: URL, limits: HistoryCacheLimits = HistoryCacheLimits()) { self.root = root.standardizedFileURL; self.limits = limits }
    static func validOID(_ value: String) -> Bool { [40, 64].contains(value.utf8.count) && value.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) } }
    static func validID(_ id: String) -> Bool {
        canonicalGitHubRepository(id) != nil && !id.hasPrefix("-")
    }
    private func location(_ id: String) throws -> URL {
        guard Self.validID(id) else { throw BigboardError.message("Repository must use a valid owner/name identifier.") }
        let path = root.appendingPathComponent("github.com").appendingPathComponent(id + ".git")
        try safePath(path)
        return path
    }
    private func safePath(_ path: URL) throws {
        guard path.standardizedFileURL.path.hasPrefix(root.path + "/") || path.standardizedFileURL == root else { throw BigboardError.message("Repository is outside Bigboard's cache.") }
        var current = path.standardizedFileURL
        while current.path.count >= root.path.count {
            if let info = try? FileManager.default.attributesOfItem(atPath: current.path), info[.type] as? FileAttributeType == .typeSymbolicLink { throw BigboardError.message("Bigboard cache cannot contain symbolic links.") }
            if current == root { break }; current.deleteLastPathComponent()
        }
    }
    private func acquire(_ id: String) async throws -> RepositoryLock {
        let directory = root.appendingPathComponent("locks")
        try safePath(directory)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let name = SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined()
        let path = directory.appendingPathComponent(name + ".lock")
        try safePath(path)
        let descriptor = open(path.path, O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, mode_t(0o600))
        guard descriptor >= 0 else { throw BigboardError.message("Could not open the repository cache lock.") }
        let lock = RepositoryLock(descriptor)
        var info = stat()
        guard fstat(descriptor, &info) == 0, (info.st_mode & S_IFMT) == S_IFREG, info.st_nlink == 1 else { throw BigboardError.message("Repository cache lock is not a regular owned file.") }
        let deadline = Date().addingTimeInterval(30)
        while flock(descriptor, LOCK_EX | LOCK_NB) != 0 {
            guard errno == EWOULDBLOCK || errno == EAGAIN else { throw BigboardError.message("Could not lock the repository cache.") }
            guard Date() < deadline else { throw BigboardError.message("Another Bigboard process is using this repository cache.") }
            try await Task.sleep(for: .milliseconds(100))
        }
        try Task.checkCancellation()
        return lock
    }
    private func verify(_ path: URL, id: String) async throws {
        try safePath(path)
        let marker = path.appendingPathComponent("bigboard-cache")
        guard (try? String(contentsOf: marker, encoding: .utf8)) == id + "\n" else { throw BigboardError.message("Cache ownership could not be verified for \(id).") }
        guard let enumerator = FileManager.default.enumerator(at: path, includingPropertiesForKeys: [.isSymbolicLinkKey]) else { throw BigboardError.message("Cannot inspect repository cache.") }
        while let item = enumerator.nextObject() as? URL {
            try Task.checkCancellation()
            if try item.resourceValues(forKeys: [.isSymbolicLinkKey]).isSymbolicLink == true { throw BigboardError.message("Repository cache contains a symbolic link.") }
        }
        for name in ["commondir", "gitdir", ".git", "config.worktree", "shallow", "objects/info/alternates", "objects/info/http-alternates"] {
            if FileManager.default.fileExists(atPath: path.appendingPathComponent(name).path) { throw BigboardError.message("Cache must be a complete independent bare repository.") }
        }
        let configData = try await GitProcess.run(["--git-dir=" + path.path, "config", "--local", "--no-includes", "--null", "--list"], limit: 1024 * 1024)
        var origins: [String] = []
        for entry in String(decoding: configData, as: UTF8.self).split(separator: "\0") {
            let pieces = entry.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).map(String.init)
            let key = pieces[0].lowercased(), value = pieces.count > 1 ? pieces[1] : ""
            if key == "remote.origin.url" { origins.append(value) }
            if key.hasPrefix("include.") || key.hasPrefix("includeif.") || key.hasPrefix("url.") || key.hasPrefix("credential.") || key.hasPrefix("http.") || key.hasSuffix(".proxy") || ["core.worktree", "core.sshcommand", "core.gitproxy"].contains(key) || key.hasPrefix("extensions.") || key.hasSuffix(".promisor") || key.hasSuffix(".partialclonefilter") { throw BigboardError.message("Repository cache has unsupported configuration.") }
        }
        guard origins == ["https://github.com/\(id).git"], try await GitProcess.text(["--git-dir=" + path.path, "rev-parse", "--is-bare-repository"]) == "true" else { throw BigboardError.message("Repository cache origin or format does not match \(id).") }
    }
    private func initializeCache(_ path: URL, id: String) async throws {
        try FileManager.default.createDirectory(at: path, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        _ = try await GitProcess.run(["init", "--bare", "--quiet", "--template=", path.path])
        try Data((id + "\n").utf8).write(to: path.appendingPathComponent("bigboard-cache"), options: .atomic)
        _ = try await GitProcess.run(["--git-dir=" + path.path, "config", "remote.origin.url", "https://github.com/\(id).git"])
    }
    private func fetchCache(_ path: URL, repo: Repository, token: String, empty: Bool) async throws -> String {
        let prefix = ["--git-dir=" + path.path]
        let remote = "refs/remotes/origin/" + repo.branch, head = "refs/heads/" + repo.branch
        let spec = "+refs/heads/" + repo.branch + ":" + remote
        var oid = ""
        if !empty {
            _ = try await GitProcess.run(prefix + ["fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "https://github.com/\(repo.id).git", spec], token: token)
            oid = try await GitProcess.text(prefix + ["rev-parse", "--verify", "--end-of-options", remote + "^{commit}"])
            guard Self.validOID(oid) else { throw BigboardError.message("Git returned an invalid default-branch revision.") }
            _ = try await GitProcess.run(prefix + ["update-ref", head, oid])
            _ = try await GitProcess.run(prefix + ["symbolic-ref", "refs/remotes/origin/HEAD", remote])
        }
        for (key, value) in [("remote.origin.fetch", spec), ("remote.origin.tagOpt", "--no-tags")] {
            if (try? await GitProcess.text(prefix + ["config", "--get", key])) != value { _ = try await GitProcess.run(prefix + ["config", "--replace-all", key, value]) }
        }
        _ = try await GitProcess.run(prefix + ["symbolic-ref", "HEAD", head])
        let refs = try await GitProcess.text(prefix + ["for-each-ref", "--format=%(refname)"])
        for ref in refs.split(separator: "\n").map(String.init) where empty || ![remote, head, "refs/remotes/origin/HEAD"].contains(ref) { _ = try await GitProcess.run(prefix + ["update-ref", "--no-deref", "-d", ref]) }
        if !empty { _ = try await GitProcess.run(prefix + ["fsck", "--connectivity-only", "--no-dangling"], limit: 1024 * 1024) }
        return oid
    }
    func refresh(_ repo: Repository, token: String) async throws -> Repository {
        guard !active.contains(repo.id) else { throw BigboardError.message("Repository operation is already running.") }
        guard !token.isEmpty, token.utf8.allSatisfy({ (33...126).contains($0) }) else { throw BigboardError.message("Sign in before refreshing repositories.") }
        active.insert(repo.id); defer { active.remove(repo.id) }
        let lock = try await acquire(repo.id); defer { lock.release() }
        let destination = try location(repo.id)
        guard !repo.branch.isEmpty else { throw BigboardError.message("Repository has no default branch.") }
        _ = try await GitProcess.run(["check-ref-format", "refs/heads/" + repo.branch])
        let manager = FileManager.default
        try manager.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        try safePath(destination)
        let exists = manager.fileExists(atPath: destination.path)
        if exists { try await verify(destination, id: repo.id) }
        let probe = try await GitProcess.text(["ls-remote", "--symref", "https://github.com/\(repo.id).git", "HEAD", "refs/heads/" + repo.branch], token: token)
        let expected = probe.split(separator: "\n").compactMap { line -> String? in
            let parts = line.split(separator: "\t", maxSplits: 1)
            return parts.count == 2 && parts[1] == "refs/heads/" + repo.branch && Self.validOID(String(parts[0])) ? String(parts[0]) : nil
        }.first ?? ""
        guard !expected.isEmpty || probe.isEmpty else { throw BigboardError.message("The default branch changed during refresh. Refresh repository access and try again.") }
        var replace = !exists
        if exists {
            do { _ = try await GitProcess.run(["--git-dir=" + destination.path, "fsck", "--connectivity-only", "--no-dangling"], limit: 1024 * 1024) }
            catch is GitCommandFailure { replace = true }
            if !replace {
                let local = (try? await GitProcess.text(["--git-dir=" + destination.path, "rev-parse", "--verify", "HEAD^{commit}"])) ?? ""
                let branch = try await GitProcess.text(["--git-dir=" + destination.path, "symbolic-ref", "HEAD"])
                if local == expected && branch == "refs/heads/" + repo.branch { var result = repo; result.revision = expected; result.stale = false; result.error = ""; return result }
            }
        }
        let path = replace ? destination.deletingLastPathComponent().appendingPathComponent(".bigboard-clone-" + UUID().uuidString) : destination
        do {
            if replace { try await initializeCache(path, id: repo.id) }
            let oid = try await fetchCache(path, repo: repo, token: token, empty: expected.isEmpty)
            if replace {
                try await verify(path, id: repo.id); try safePath(destination); try Task.checkCancellation()
                if exists {
                    let backup = destination.deletingLastPathComponent().appendingPathComponent(".bigboard-replaced-" + UUID().uuidString)
                    try manager.moveItem(at: destination, to: backup)
                    do { try manager.moveItem(at: path, to: destination) }
                    catch { try? manager.moveItem(at: backup, to: destination); throw error }
                    try? manager.removeItem(at: backup)
                } else { try manager.moveItem(at: path, to: destination) }
            }
            var result = repo; result.revision = oid; result.stale = false; result.error = ""; return result
        } catch { if replace { try? manager.removeItem(at: path) }; throw error }
    }
    private func scanPath(_ id: String) -> URL { root.appendingPathComponent("scans").appendingPathComponent(SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined() + ".json") }
    private func externalScanInputs(_ path: URL) async throws -> [Data] {
        let prefix = ["--git-dir=" + path.path]
        let config = try await GitProcess.run(prefix + ["config", "--local", "--no-includes", "--null", "--list"], limit: 1024 * 1024)
        var inputs: [Data] = []
        func appendFile(_ key: String, _ value: String) throws {
            let name = (value as NSString).expandingTildeInPath
            let url = (name.hasPrefix("/") ? URL(fileURLWithPath: name) : path.appendingPathComponent(name)).resolvingSymlinksInPath()
            inputs.append(Data((key + "\0" + url.path).utf8))
            if url.path == "/dev/null" { inputs.append(Data()); return }
            guard FileManager.default.fileExists(atPath: url.path) else { inputs.append(Data("missing".utf8)); return }
            let info = try FileManager.default.attributesOfItem(atPath: url.path)
            guard info[.type] as? FileAttributeType == .typeRegular, (info[.size] as? NSNumber)?.intValue ?? 0 <= 16 * 1024 * 1024 else { throw BigboardError.message("Git scan configuration references an unsupported file.") }
            inputs.append(try Data(contentsOf: url))
        }
        for entry in String(decoding: config, as: UTF8.self).split(separator: "\0") {
            let pieces = entry.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).map(String.init)
            guard pieces.count == 2, ["mailmap.file", "core.attributesfile", "diff.orderfile"].contains(pieces[0].lowercased()), !pieces[1].isEmpty else { continue }
            let value = try await GitProcess.run(prefix + ["config", "--local", "--no-includes", "--null", "--path", "--get", pieces[0]], limit: 1024 * 1024)
            let name = String(decoding: value, as: UTF8.self).trimmingCharacters(in: CharacterSet(charactersIn: "\0"))
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
    private func fingerprint(_ path: URL, revision: String, configuration: WorkspaceConfiguration, version: String, externalInputs: [Data]) throws -> String {
        let encoder = JSONEncoder(); encoder.outputFormatting = [.sortedKeys]
        var hasher = SHA256(); hasher.update(data: Data((revision + "\0" + version).utf8)); hasher.update(data: try encoder.encode(configuration))
        for data in externalInputs { hasher.update(data: Data("\(data.count):".utf8)); hasher.update(data: data) }
        let manager = FileManager.default
        guard let enumerator = manager.enumerator(at: path, includingPropertiesForKeys: [.contentModificationDateKey, .fileSizeKey, .isRegularFileKey]) else { throw BigboardError.message("Cannot inspect scan inputs.") }
        var entries: [URL] = []
        while let item = enumerator.nextObject() as? URL { try Task.checkCancellation(); entries.append(item) }
        entries.sort { $0.path.utf8.lexicographicallyPrecedes($1.path.utf8) }
        for item in entries {
            try Task.checkCancellation()
            let values = try item.resourceValues(forKeys: [.contentModificationDateKey, .fileSizeKey, .isRegularFileKey])
            guard values.isRegularFile == true else { continue }
            hasher.update(data: Data("\(item.path):\(values.fileSize ?? 0):\(values.contentModificationDate?.timeIntervalSince1970 ?? 0)".utf8))
            if !item.path.hasPrefix(path.appendingPathComponent("objects").path + "/") { hasher.update(data: try Data(contentsOf: item)) }
        }
        return hasher.finalize().map { String(format: "%02x", $0) }.joined()
    }
    private func scanFingerprint(_ path: URL, revision: String, configuration: WorkspaceConfiguration, version: String) async throws -> String? {
        do { return try fingerprint(path, revision: revision, configuration: configuration, version: version, externalInputs: try await externalScanInputs(path)) }
        catch { try Task.checkCancellation(); return nil }
    }
    func scan(_ repo: Repository, configuration: WorkspaceConfiguration) async throws -> (Repository, [Commit]) {
        guard !active.contains(repo.id) else { throw BigboardError.message("Repository operation is already running.") }
        active.insert(repo.id); defer { active.remove(repo.id) }
        let lock = try await acquire(repo.id); defer { lock.release() }
        let path = try location(repo.id)
        try await verify(path, id: repo.id)
        let prefix = ["--git-dir=" + path.path]
        let refs = try await GitProcess.text(prefix + ["for-each-ref", "--format=%(refname)"])
        if refs.isEmpty { var result = repo; result.revision = ""; result.scannedAt = repo.scannedAt ?? Date(); return (result, []) }
        let revision = try await GitProcess.text(prefix + ["rev-parse", "--verify", "HEAD^{commit}"])
        guard Self.validOID(revision) else { throw BigboardError.message("Repository has an invalid scan revision.") }
        let version = try await GitProcess.text(["--version"])
        let key = try await scanFingerprint(path, revision: revision, configuration: configuration, version: version)
        let cachePath = scanPath(repo.id)
        try safePath(cachePath)
        if let key, let cache = try? HistoryScanCache.read(cachePath, limits: limits, checkpoint: { try Task.checkCancellation() }), cache.schema == 2, cache.fingerprint == key {
            try Task.checkCancellation()
            var result = repo; result.revision = revision; result.scannedAt = cache.scannedAt; return (result, cache.records)
        }
        let attributes: [AttributeRule]
        let listing = configuration.allFiles ? "" : try await GitProcess.text(prefix + ["ls-tree", "--name-only", "--end-of-options", revision, "--", ".gitattributes"])
        if listing.isEmpty { attributes = [] }
        else {
            let data = try await GitProcess.run(prefix + ["cat-file", "blob", revision + ":.gitattributes"], limit: HistoryRules.maximumAttributeBytes)
            attributes = try HistoryRules.attributes(String(decoding: data, as: UTF8.self), checkpoint: { try Task.checkCancellation() })
        }
        let parser = GitLogParser(repo: repo.id, configuration: configuration, attributes: attributes)
        let args = ["log", "--no-merges", "--root", "-M50%", "-C50%", "-l0", "--no-ext-diff", "--no-textconv", "--no-color", "--no-relative", "--no-show-signature", "--diff-algorithm=myers", "--no-indent-heuristic", "--ignore-submodules=none", "--format=%x00%H%x00%aN%x00%aE%x00%at%x00%(trailers:key=Co-authored-by,valueonly,separator=%x1f)%x00%s%x00", "--numstat", "-z", revision, "--"]
        _ = try await GitProcess.run(prefix + args, stream: { data, checkpoint in try parser.feed(data, checkpoint: checkpoint) })
        let records = try parser.finish()
        try Task.checkCancellation()
        let after = try await GitProcess.text(prefix + ["rev-parse", "--verify", "HEAD^{commit}"])
        var result = repo; result.revision = after == revision ? revision : ""; result.scannedAt = Date()
        if let key, after == revision, try await scanFingerprint(path, revision: revision, configuration: configuration, version: version) == key {
            let cache = HistoryScanCache(schema: 2, fingerprint: key, scannedAt: result.scannedAt!, records: records)
            _ = try? cache.write(to: cachePath, limits: limits, checkpoint: { try Task.checkCancellation() })
        }
        try Task.checkCancellation()
        return (result, records)
    }
    func cachedRepositories() async throws -> [Repository] {
        let host = root.appendingPathComponent("github.com"); try safePath(host)
        guard FileManager.default.fileExists(atPath: host.path) else { return [] }
        var result: [Repository] = []
        for owner in try FileManager.default.contentsOfDirectory(at: host, includingPropertiesForKeys: [.isDirectoryKey]) {
            try safePath(owner)
            guard try owner.resourceValues(forKeys: [.isDirectoryKey]).isDirectory == true else { continue }
            for path in try FileManager.default.contentsOfDirectory(at: owner, includingPropertiesForKeys: [.isDirectoryKey]) where path.pathExtension == "git" {
                let id = owner.lastPathComponent + "/" + path.deletingPathExtension().lastPathComponent
                let lock = try await acquire(id); defer { lock.release() }
                try await verify(path, id: id)
                let ref = try await GitProcess.text(["--git-dir=" + path.path, "symbolic-ref", "HEAD"])
                result.append(Repository(id: id, branch: String(ref.dropFirst("refs/heads/".count))))
            }
        }
        return result.sorted { Analysis.literalLess($0.id, $1.id) }
    }
    func remove(_ id: String) async throws {
        guard !active.contains(id) else { throw BigboardError.message("Repository operation is already running.") }
        active.insert(id); defer { active.remove(id) }
        let lock = try await acquire(id); defer { lock.release() }
        let path = try location(id); try await verify(path, id: id); try safePath(path)
        try Task.checkCancellation()
        try FileManager.default.removeItem(at: path)
        let scan = scanPath(id)
        try safePath(scan); if FileManager.default.fileExists(atPath: scan.path) { try FileManager.default.removeItem(at: scan) }
    }
    func pathURL(repo: Repository, path literal: String) async throws -> URL {
        let lock = try await acquire(repo.id); defer { lock.release() }
        let directory = try location(repo.id); try await verify(directory, id: repo.id)
        guard Self.validOID(repo.revision) else { throw BigboardError.message("The retained scan revision is unavailable. Refresh and try again.") }
        if literal.isEmpty { return URL(string: "https://github.com/" + repo.id)! }
        guard literal.split(separator: "/", omittingEmptySubsequences: false).allSatisfy({ !$0.isEmpty && $0 != "." && $0 != ".." && !$0.contains("\0") }) else { throw BigboardError.message("This repository path is unavailable.") }
        let type = try await GitProcess.text(["--git-dir=" + directory.path, "cat-file", "-t", repo.revision + "^{commit}:" + literal])
        guard ["blob", "tree"].contains(type) else { throw BigboardError.message("This path is absent at the retained scan revision.") }
        var url = URL(string: "https://github.com/" + repo.id)!.appendingPathComponent(type == "tree" ? "tree" : "blob").appendingPathComponent(repo.revision)
        for part in literal.split(separator: "/") { url.appendPathComponent(String(part)) }
        return url
    }
}
