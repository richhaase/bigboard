import Foundation
import Network
import Testing
@testable import Bigboard

@Suite
struct HTTPTransportTests {
    @Test func redirectIsReturnedWithoutFollowingIt() async throws {
        let server = try LocalHTTPServer(response: .redirect)
        defer { server.stop() }
        var request = URLRequest(url: try await server.start())
        request.setValue("Bearer fixture-token", forHTTPHeaderField: "Authorization")
        let response = try await URLSessionGitHubTransport().send(request, maximumBytes: 64)
        #expect(response.status == 302)
        #expect(server.requestCount == 1)
    }

    @Test(arguments: [false, true], [64, 65])
    func responseSizeLimitIsEnforced(declaresLength: Bool, size: Int) async throws {
        let server = try LocalHTTPServer(response: .body(size: size, declaresLength: declaresLength))
        defer { server.stop() }
        let request = URLRequest(url: try await server.start())
        let transport = URLSessionGitHubTransport()
        if size == 64 {
            let response = try await transport.send(request, maximumBytes: 64)
            #expect(response.status == 200)
            #expect(response.data == Data(repeating: 120, count: 64))
        } else {
            await #expect(throws: GitHubFailure.outputLimit) {
                try await transport.send(request, maximumBytes: 64)
            }
        }
    }

    @Test func cancelingStalledResponseClosesConnection() async throws {
        let server = try LocalHTTPServer(response: .stall)
        defer { server.stop() }
        let request = URLRequest(url: try await server.start())
        let transport = URLSessionGitHubTransport()
        let task = Task { try await transport.send(request, maximumBytes: 1024) }
        defer { task.cancel() }
        try await server.waitFor(.responseSent)
        task.cancel()
        try await server.waitFor(.peerClosed)
        await #expect(throws: CancellationError.self) { try await task.value }
        #expect(server.requestCount == 1)
    }
}

private final class LocalHTTPServer: @unchecked Sendable {
    enum Response {
        case redirect
        case body(size: Int, declaresLength: Bool)
        case stall
    }
    enum Event { case ready, responseSent, peerClosed }
    enum Failure: Error { case timeout, stopped, invalidRequest }

    private let listener: NWListener
    private let response: Response
    private let queue = DispatchQueue(label: "BigboardTests.HTTP")
    private let events = AsyncThrowingStream<Event, Error>.makeStream()
    private let lock = NSLock()
    private var connections: [NWConnection] = []
    private var requests = 0
    var requestCount: Int { lock.withLock { requests } }

    init(response: Response) throws {
        self.response = response
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        listener = try NWListener(using: parameters)
        listener.stateUpdateHandler = { [weak self] state in
            switch state {
            case .ready: self?.events.continuation.yield(.ready)
            case .failed(let error): self?.events.continuation.finish(throwing: error)
            default: break
            }
        }
        listener.newConnectionHandler = { [weak self] connection in
            guard let self else { connection.cancel(); return }
            self.lock.withLock { self.connections.append(connection) }
            connection.start(queue: self.queue)
            self.readRequest(connection)
        }
    }

    func start() async throws -> URL {
        listener.start(queue: queue)
        try await waitFor(.ready)
        let port = try #require(listener.port)
        return try #require(URL(string: "http://127.0.0.1:\(port.rawValue)/request"))
    }

    func stop() {
        listener.cancel()
        lock.withLock { connections }.forEach { $0.cancel() }
        events.continuation.finish()
    }

    func waitFor(_ expected: Event) async throws {
        try await withThrowingTaskGroup(of: Void.self) { group in
            defer { group.cancelAll() }
            group.addTask {
                for try await event in self.events.stream {
                    if event == expected { return }
                }
                throw Failure.stopped
            }
            group.addTask {
                try await Task.sleep(for: .seconds(5))
                throw Failure.timeout
            }
            try await group.next()
        }
    }

    private func readRequest(_ connection: NWConnection, accumulated: Data = Data()) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 4096) { [weak self] data, _, complete, error in
            guard let self else { return }
            let request = accumulated + (data ?? Data())
            if request.range(of: Data("\r\n\r\n".utf8)) != nil {
                self.lock.withLock { self.requests += 1 }
                self.respond(connection, request: String(decoding: request, as: UTF8.self))
            } else if complete || error != nil || request.count > 16384 {
                self.events.continuation.finish(throwing: error ?? Failure.invalidRequest)
                connection.cancel()
            } else {
                self.readRequest(connection, accumulated: request)
            }
        }
    }

    private func respond(_ connection: NWConnection, request: String) {
        let message: String
        let stalls: Bool
        switch response {
        case .redirect:
            message = request.hasPrefix("GET /followed ")
                ? "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"
                : "HTTP/1.1 302 Found\r\nLocation: /followed\r\nContent-Length: 0\r\n\r\n"
            stalls = false
        case .body(let size, let declaresLength):
            let length = declaresLength ? "Content-Length: \(size)\r\n" : ""
            message = "HTTP/1.1 200 OK\r\n\(length)Connection: close\r\n\r\n" + String(repeating: "x", count: size)
            stalls = false
        case .stall:
            message = "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nx"
            stalls = true
        }
        connection.send(content: Data(message.utf8), completion: .contentProcessed { [weak self] error in
            guard let self else { return }
            if let error {
                self.events.continuation.finish(throwing: error)
                connection.cancel()
            } else if stalls {
                self.events.continuation.yield(.responseSent)
                self.waitForClose(connection)
            } else {
                connection.cancel()
            }
        })
    }

    private func waitForClose(_ connection: NWConnection) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 1) { [weak self] _, _, complete, error in
            guard let self else { return }
            if complete || error != nil {
                self.events.continuation.yield(.peerClosed)
                connection.cancel()
            } else {
                self.waitForClose(connection)
            }
        }
    }
}
