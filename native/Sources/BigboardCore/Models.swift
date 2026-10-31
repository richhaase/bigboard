import Foundation

struct IdentityRule: Codable, Sendable, Equatable { var aliases: [String] = []; var name: String = "" }
struct WorkArea: Codable, Sendable, Equatable { var name: String; var paths: [String] }
struct WorkspaceConfiguration: Codable, Sendable, Equatable {
    var schemaVersion = 1
    var repositories: [String] = []
    var excludedRepositories: [String] = []
    var groups: [String: [String]] = [:]
    var identities: [String: IdentityRule] = [:]
    var workAreas: [String: [WorkArea]] = [:]
    var exclude: [String] = []
    var allFiles = false
    var aiIdentities: [String] = []
    var botIdentities: [String] = []
    var sinceDays = 14
    var contributorSort = "change"
    var requestBudget = 128
    var cachedOnly = false
}
struct FileChange: Codable, Sendable, Equatable {
    var path: String
    var previousPath: String = ""
    var added = 0
    var removed = 0
    var binary = false
    var excludedReason: String = ""
}
struct Commit: Codable, Sendable, Identifiable, Equatable {
    var id: String
    var repo: String
    var author: String
    var email: String
    var date: Date
    var subject: String
    var added = 0
    var removed = 0
    var ai = false
    var bot = false
    var linesUnknown = false
    var pathsUnknown = false
    var files: [FileChange] = []
    var identity: String { email.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "\(repo):\(author)" : email.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() }
    var change: Int { added + removed }
}
struct Repository: Codable, Sendable, Identifiable, Equatable {
    var id: String
    var branch = "main"
    var revision = ""
    var pushedAt: Date? = nil
    var scannedAt: Date? = nil
    var stale = false
    var error = ""
    var included = true
    var prCheckedAt: Date? = nil
    var prInventoryComplete = false
    var prFilesComplete = false
    var prReviewersComplete = false
    var prStale = false
    var name: String { id }
}
struct Review: Codable, Sendable, Equatable { var author: String; var state: String; var submittedAt: Date? }
struct PullRequest: Codable, Sendable, Identifiable, Equatable {
    var repo: String
    var number: Int
    var title: String
    var author: String
    var url: String
    var updatedAt: Date?
    var draft = false
    var review = "UNKNOWN"
    var checks = "UNKNOWN"
    var mergeable = "UNKNOWN"
    var headOID = ""
    var baseOID = ""
    var files: [String] = []
    var fileCount = 0
    var reviewers: [String] = []
    var reviews: [Review] = []
    var inventoryComplete = false
    var filesComplete = false
    var reviewersComplete = false
    var reviewsComplete = false
    var checkedAt: Date? = nil
    var id: String { "\(repo)#\(number)" }
}
struct Coverage: Codable, Sendable, Equatable {
    var phase = "initial"
    var message = ""
    var errors: [String] = []
    var probeRequests = 0
    var inventoryRequests = 0
    var fileRequests = 0
    var accountRequests = 0
}
struct Snapshot: Sendable {
    var generatedAt = Date()
    var accountID = ""
    var viewer = ""
    var repositories: [Repository] = []
    var commits: [Commit] = []
    var pullRequests: [PullRequest] = []
    var coverage = Coverage()
    var cachedOnly = false
    var bookmark: Date? = nil
}
struct ActivityScope: Hashable, Sendable {
    var repo = ""
    var path = ""
    var isFile = false
    var areaName = ""
    func contains(_ candidate: String) -> Bool { path.isEmpty || (isFile ? candidate == path : candidate.hasPrefix(path + "/")) }
}
struct AuthStatus: Sendable {
    var configured = false
    var signedIn = false
    var pending = false
    var login = ""
    var accountID = ""
    var error = ""
}
enum BigboardError: LocalizedError, Sendable {
    case message(String)
    var errorDescription: String? { switch self { case .message(let value): value } }
}
