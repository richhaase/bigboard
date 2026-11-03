import Foundation

enum ApplicationIdentity: String, CaseIterable, Sendable {
    case production = "com.richhaase.bigboard"
    case development = "com.richhaase.bigboard.dev"

    static func fromBundle(_ bundle: Bundle = .main) -> ApplicationIdentity {
        ApplicationIdentity(rawValue: bundle.bundleIdentifier ?? "") ?? .production
    }

    var callbackURI: String { "\(rawValue)://oauth/github" }
    var keychainService: String { "\(rawValue).github" }
    var storageDirectory: String { "\(rawValue).native" }
}
