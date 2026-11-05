import Foundation
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
        lock.lock()
        defer { lock.unlock() }
        guard !cancelled else { throw CancellationError() }
        self.process = process
        try process.run()
        let pid = process.processIdentifier
        let actual = getpgid(pid)
        guard actual == pid || actual == -1 else {
            process.terminate()
            throw BigboardError.message("Git could not launch in an isolated process group.")
        }
        group = pid
    }
    func stop(timedOut: Bool = false) {
        lock.lock()
        guard !finished else {
            lock.unlock()
            return
        }
        cancelled = true
        self.timedOut = self.timedOut || timedOut
        let group = self.group
        if group > 0 { kill(-group, SIGTERM) }
        lock.unlock()
        guard group > 0 else { return }
        DispatchQueue.global()
            .asyncAfter(deadline: .now() + 1) { [self] in
                lock.lock()
                defer { lock.unlock() }
                if !finished && self.group == group { kill(-group, SIGKILL) }
            }
    }
    func finish() {
        lock.lock()
        finished = true
        lock.unlock()
    }
    func checkpoint() throws {
        lock.lock()
        defer { lock.unlock() }
        if timedOut { throw BigboardError.message("Git did not finish within the operation timeout.") }
        if cancelled { throw CancellationError() }
    }
    var isCancelled: Bool {
        lock.lock()
        defer { lock.unlock() }
        return cancelled
    }
    var didTimeOut: Bool {
        lock.lock()
        defer { lock.unlock() }
        return timedOut
    }
}
private final class ProcessRead: @unchecked Sendable {
    var bytes = Data()
    var error: (any Error)?
}
enum GitExecutable {
    static func select(
        _ name: String = "git", environment: [String: String] = ProcessInfo.processInfo.environment,
        executable: (String) -> Bool = { path in
            var directory: ObjCBool = false
            return FileManager.default.fileExists(atPath: path, isDirectory: &directory)
                && !directory.boolValue && FileManager.default.isExecutableFile(atPath: path)
        }
    ) throws -> URL {
        let directories =
            (environment["PATH"] ?? "").split(separator: ":").map(String.init) + [
                "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin",
            ]
        for directory in directories where directory.hasPrefix("/") {
            let candidate = URL(fileURLWithPath: directory).appendingPathComponent(name)
            if executable(candidate.path) { return candidate }
        }
        if name == "gh" { throw GitHubCLIFailure.missing }
        throw BigboardError.message(
            "Git is unavailable. Install Git with Homebrew or Xcode Command Line Tools, then reopen Bigboard."
        )
    }
}
enum GitProcess {
    static let transferTimeout: TimeInterval = 30 * 60
    static func executable() throws -> URL {
        try GitExecutable.select()
    }
    static func environment(_ inherited: [String: String] = ProcessInfo.processInfo.environment) -> [String:
        String]
    {
        var environment = inherited.filter { ["HOME", "TMPDIR", "XDG_CONFIG_HOME"].contains($0.key) }
        environment.merge([
            "PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1",
            "GIT_TERMINAL_PROMPT": "0", "GIT_OPTIONAL_LOCKS": "0", "GIT_CONFIG_GLOBAL": "/dev/null",
            "GIT_CONFIG_SYSTEM": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1", "GIT_ASKPASS": "",
            "SSH_ASKPASS": "", "SSH_ASKPASS_REQUIRE": "never", "LC_ALL": "C",
        ]) { _, new in new }
        return environment
    }
    static func run(
        _ arguments: [String], directory: URL? = nil, token: String = "", timeout: TimeInterval = 120,
        limit: Int = 256 * 1024 * 1024,
        stream: (@Sendable (Data, @Sendable () throws -> Void) throws -> Void)? = nil
    ) async throws -> Data {
        let executable = try executable()
        var pinned = [
            "-c", "core.quotePath=false", "-c", "log.showRoot=true", "-c", "log.showSignature=false", "-c",
            "color.ui=false", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c",
            "credential.interactive=false", "-c", "credential.helper=", "-c", "http.followRedirects=false",
            "-c", "http.sslVerify=true", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c",
            "protocol.ext.allow=never", "-c", "protocol.file.allow=never",
        ]
        if !token.isEmpty {
            pinned += ["--config-env=http.https://github.com/.extraHeader=BIGBOARD_GIT_AUTHORIZATION"]
        }
        let gitDirectory = arguments.first(where: { $0.hasPrefix("--git-dir=") })
            .map { URL(fileURLWithPath: String($0.dropFirst("--git-dir=".count))) }
        var environment = Self.environment()
        if !token.isEmpty {
            environment["BIGBOARD_GIT_AUTHORIZATION"] =
                "Authorization: Basic " + Data("x-access-token:\(token)".utf8).base64EncodedString()
        }
        return try await execute(
            executable, arguments: pinned + arguments, environment: environment,
            directory: directory ?? gitDirectory ?? URL(fileURLWithPath: "/"), timeout: timeout, limit: limit,
            authenticated: !token.isEmpty, stream: stream)
    }
    static func execute(
        _ executable: URL, arguments: [String], environment: [String: String], directory: URL,
        timeout: TimeInterval = 120, limit: Int = 256 * 1024 * 1024, authenticated: Bool = false,
        stream: (@Sendable (Data, @Sendable () throws -> Void) throws -> Void)? = nil
    ) async throws -> Data {
        let control = ProcessControl()
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            return try await withCheckedThrowingContinuation { continuation in
                DispatchQueue.global(qos: .userInitiated)
                    .async {
                        let process = Process(), output = Pipe(), diagnostic = Pipe()
                        process.executableURL = executable
                        process.arguments = arguments
                        process.currentDirectoryURL = directory
                        process.environment = environment
                        process.standardOutput = output
                        process.standardError = diagnostic
                        process.standardInput = FileHandle.nullDevice
                        do { try control.launch(process) } catch {
                            continuation.resume(throwing: error)
                            return
                        }
                        let timer = DispatchSource.makeTimerSource(queue: .global())
                        timer.schedule(deadline: .now() + timeout)
                        timer.setEventHandler { control.stop(timedOut: true) }
                        timer.resume()
                        let group = DispatchGroup(), out = ProcessRead(), err = ProcessRead()
                        for (pipe, result, maximum, callback) in [
                            (output, out, limit, stream), (diagnostic, err, 64 * 1024, nil),
                        ] {
                            group.enter()
                            DispatchQueue.global(qos: .userInitiated)
                                .async {
                                    defer {
                                        group.leave()
                                        try? pipe.fileHandleForReading.close()
                                    }
                                    do {
                                        var count = 0
                                        while let chunk = try pipe.fileHandleForReading.read(
                                            upToCount: 64 * 1024), !chunk.isEmpty
                                        {
                                            try control.checkpoint()
                                            if let callback {
                                                try callback(chunk, { try control.checkpoint() })
                                            } else {
                                                guard chunk.count <= maximum - count else {
                                                    throw BigboardError.message(
                                                        "Git output exceeded the safety limit.")
                                                }
                                                count += chunk.count
                                                result.bytes.append(chunk)
                                            }
                                            try control.checkpoint()
                                        }
                                    } catch {
                                        result.error = error
                                        control.stop()
                                    }
                                }
                        }
                        process.waitUntilExit()
                        group.wait()
                        timer.cancel()
                        control.finish()
                        let readerErrors = [out.error, err.error].compactMap { $0 }
                        if control.didTimeOut {
                            continuation.resume(
                                throwing: BigboardError.message(
                                    "Git did not finish within the operation timeout."))
                        } else if let error = readerErrors.first(where: { !($0 is CancellationError) })
                            ?? readerErrors.first
                        {
                            continuation.resume(throwing: error)
                        } else if control.isCancelled {
                            continuation.resume(throwing: CancellationError())
                        } else if process.terminationStatus != 0 {
                            if authenticated,
                                let failure = authenticatedFailure(String(decoding: err.bytes, as: UTF8.self))
                            {
                                continuation.resume(throwing: failure)
                                return
                            }
                            let detail =
                                !authenticated
                                ? String(decoding: err.bytes, as: UTF8.self)
                                    .trimmingCharacters(in: .whitespacesAndNewlines)
                                : "Authenticated Git request failed. Check repository access and try again."
                            continuation.resume(
                                throwing: GitCommandFailure(
                                    status: process.terminationStatus,
                                    detail: detail.isEmpty
                                        ? "Git command failed (\(process.terminationStatus))." : detail))
                        } else {
                            continuation.resume(returning: out.bytes)
                        }
                    }
            }
        } onCancel: {
            control.stop()
        }
    }
    static func authenticatedFailure(_ diagnostic: String) -> GitHubFailure? {
        let value = diagnostic.lowercased()
        if value.contains("rate limit") || value.contains("error: 429") || value.contains("http 429") {
            return .rateLimited
        }
        if value.contains("error: 401") || value.contains("http 401")
            || value.contains("authentication failed") || value.contains("invalid username or token")
        {
            return .auth
        }
        if value.contains("error: 403") || value.contains("http 403") || value.contains("remote: permission to") {
            return .permission
        }
        if value.contains("error: 404") || value.contains("http 404")
            || value.contains("repository not found")
        {
            return .notFound
        }
        return nil
    }
    static func text(_ arguments: [String], directory: URL? = nil, token: String = "") async throws -> String
    {
        String(decoding: try await run(arguments, directory: directory, token: token), as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
