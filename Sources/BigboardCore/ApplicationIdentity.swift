import Foundation

enum ApplicationIdentity: String, CaseIterable, Sendable {
    case production = "com.richhaase.bigboard"
    case development = "com.richhaase.bigboard.dev"

    static func fromBundle(_ bundle: Bundle = .main) -> ApplicationIdentity {
        ApplicationIdentity(rawValue: bundle.bundleIdentifier ?? "") ?? .production
    }

    var directoryName: String { self == .production ? "bigboard" : "bigboard-dev" }
}
