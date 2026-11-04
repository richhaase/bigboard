import AppKit
import AuthenticationServices

@MainActor
protocol WebAuthenticating {
    func authenticate(url: URL, callbackScheme: String) async throws -> URL
    func cancel()
}

@MainActor
final class WebAuthentication: NSObject, WebAuthenticating, ASWebAuthenticationPresentationContextProviding {
    private var session: ASWebAuthenticationSession?
    private var continuation: CheckedContinuation<URL, Error>?
    private var attempt: UUID?
    private var window: NSWindow?

    func authenticate(url: URL, callbackScheme: String) async throws -> URL {
        cancel()
        guard let window = NSApp.keyWindow ?? NSApp.mainWindow else {
            throw BigboardError.message("Open the Bigboard window before signing in.")
        }
        let attempt = UUID()
        self.attempt = attempt
        self.window = window.sheetParent ?? window
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            return try await withCheckedThrowingContinuation { continuation in
                self.continuation = continuation
                let completion = Self.completionHandler { [weak self] result in
                    guard let self, self.attempt == attempt else { return }
                    self.finish(result)
                }
                let session = ASWebAuthenticationSession(
                    url: url, callback: .customScheme(callbackScheme), completionHandler: completion)
                self.session = session
                session.presentationContextProvider = self
                if !session.start() {
                    finish(.failure(BigboardError.message("Could not start GitHub sign-in. Try again.")))
                }
            }
        } onCancel: {
            Task { @MainActor in
                if self.attempt == attempt { self.cancel() }
            }
        }
    }

    nonisolated static func completionHandler(
        _ complete: @escaping @MainActor @Sendable (Result<URL, Error>) -> Void
    ) -> @Sendable (URL?, Error?) -> Void {
        { url, error in
            Task { @MainActor in
                if let error {
                    let canceled = (error as? ASWebAuthenticationSessionError)?.code == .canceledLogin
                    complete(.failure(canceled ? CancellationError() : error))
                } else if let url {
                    complete(.success(url))
                } else {
                    complete(.failure(AuthFailure.callback))
                }
            }
        }
    }

    func cancel() {
        let session = session
        finish(.failure(CancellationError()))
        session?.cancel()
    }

    private func finish(_ result: Result<URL, Error>) {
        let continuation = continuation
        self.continuation = nil
        session = nil
        attempt = nil
        window = nil
        continuation?.resume(with: result)
    }

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        window ?? ASPresentationAnchor()
    }
}
