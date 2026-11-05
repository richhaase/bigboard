import AppKit
import Darwin
import Foundation
import Observation

@MainActor @Observable
final class WorkspaceModel {
    var snapshot = Snapshot()
    var configuration = WorkspaceConfiguration()
    var authStatus = AuthStatus()
    var checkingSignIn = true
    var busy = false
    private(set) var signingOut = false
    var error = ""
    var discovered: [Repository] = []
    var cachedRepositoryRows: [Repository] = []
    var removableCacheIDs: Set<String> {
        inventoryVerified ? Set(cachedRepositoryRows.map(\.id)).subtracting(authorized.map(\.id)) : []
    }
    var activeGroup = ""
    var storageDescription: String {
        "Settings: \(storage.supportRoot.path)\nHistory: \(storage.cacheRoot.path)"
    }
    private let storage: WorkspaceStorage
    private let auth: AuthManager
    private let webAuthentication: any WebAuthenticating
    private var signInAttempt: UUID?
    private let transport: any GitHubTransport
    private let now: @Sendable () -> Date
    private var lastRefreshFinished: Date?
    private var shuttingDown = false
    private let refreshHistory: @Sendable (GitHistory, Repository, String) async throws -> Repository
    private var github: GitHubService?
    private var history: GitHistory?
    private var account = ""
    private var authorized: [Repository] = []
    private var generation = 0
    private var refreshTask: Task<Void, Never>?
    private var discoveryTask: Task<Void, Never>?
    private var shutdownOperation: Int?
    private var started = false
    private var inventoryVerified = false
    private var discoveryVerified = false
    private var pendingGitDenials: Set<String> = []
    private var pendingGitAuthentication = false
    init(
        storage: WorkspaceStorage = .system(), authentication: AuthManager? = nil,
        webAuthentication: any WebAuthenticating = WebAuthentication(),
        transport: any GitHubTransport = URLSessionGitHubTransport(),
        now: @escaping @Sendable () -> Date = Date.init,
        refreshHistory: @escaping @Sendable (GitHistory, Repository, String) async throws -> Repository = {
            try await $0.refresh($1, token: $2)
        }
    ) {
        self.storage = storage
        self.webAuthentication = webAuthentication
        self.transport = transport
        self.now = now
        self.refreshHistory = refreshHistory
        self.auth =
            authentication
            ?? AuthManager(configuration: AuthConfiguration.fromBundle(), store: KeychainCredentialStore())
    }
    func start() async {
        guard !started else { return }
        started = true
        let operation = generation
        let state = await auth.status()
        guard operation == generation else { return }
        checkingSignIn = false
        authStatus = state
        if !state.error.isEmpty { error = state.error }
        guard authStatus.signedIn else {
            snapshot.coverage.phase = "signin"
            return
        }
        do {
            try connect()
            await refresh()
        } catch { self.error = error.localizedDescription }
    }
    private func connect() throws {
        guard authStatus.signedIn else {
            throw BigboardError.message("Sign in with GitHub to open a workspace.")
        }
        if account == authStatus.accountID, history != nil { return }
        generation += 1
        account = authStatus.accountID
        lastRefreshFinished = nil
        configuration = try storage.loadConfiguration(account)
        authorized = try storage.loadGrant(account)
        discovered = []
        cachedRepositoryRows = []
        inventoryVerified = false
        discoveryVerified = false
        pendingGitDenials = []
        pendingGitAuthentication = false
        activeGroup = ""
        history = GitHistory(root: try storage.repositoryRoot(account))
        let manager = auth
        github = GitHubService(token: { try await manager.token() }, transport: transport)
        snapshot = Snapshot(accountID: account, viewer: authStatus.login)
    }
    func refresh() async {
        await beginRefresh()?.value
    }
    func nextAutomaticRefresh(visible: Bool, modalOpen: Bool) -> Date? {
        guard visible, !modalOpen, !shuttingDown, !checkingSignIn, !busy, !signingOut,
            authStatus.signedIn, !configuration.cachedOnly, !configuration.repositories.isEmpty,
            configuration.refreshIntervalMinutes > 0, let lastRefreshFinished
        else { return nil }
        return lastRefreshFinished.addingTimeInterval(Double(configuration.refreshIntervalMinutes) * 60)
    }
    func refreshAutomatically(visible: Bool, modalOpen: Bool) async {
        guard let deadline = nextAutomaticRefresh(visible: visible, modalOpen: modalOpen), deadline <= now()
        else { return }
        await refresh()
    }
    private func beginRefresh() -> Task<Void, Never>? {
        guard !busy, history != nil, github != nil, !account.isEmpty else { return nil }
        busy = true
        snapshot.coverage.phase = "refreshing"
        snapshot.coverage.message = "Refreshing workspace…"
        let operation = generation
        let task = Task {
            await self.performRefresh(operation: operation)
            if operation == self.generation {
                self.lastRefreshFinished = self.now()
                self.refreshTask = nil
                self.busy = false
            }
        }
        refreshTask = task
        return task
    }
    private func selected(_ repositories: [Repository]) -> [Repository] {
        let configured = Set(configuration.repositories)
        let excluded = Set(configuration.excludedRepositories)
        let group = activeGroup.isEmpty ? nil : Set(configuration.groups[activeGroup] ?? [])
        return repositories.filter { repo in
            if !configured.contains(repo.id) || excluded.contains(repo.id) { return false }
            if let group, !group.contains(repo.id) { return false }
            let name = String(repo.id.split(separator: "/").last ?? "")
            return !configuration.exclude.contains { pattern in
                fnmatch(pattern, repo.id, 0) == 0 || fnmatch(pattern, name, 0) == 0
            }
        }
    }
    private func performRefresh(operation: Int) async {
        guard operation == generation, let history, let github, !account.isEmpty else { return }
        let id = account, config = configuration
        error = ""
        let cacheRows = (try? await history.cachedRepositories()) ?? []
        guard operation == generation, !Task.isCancelled else { return }
        cachedRepositoryRows = cacheRows
        var current = snapshot
        current.cachedOnly = config.cachedOnly
        current.coverage = Coverage(phase: "refreshing", message: "Loading cached history…")
        snapshot.coverage = current.coverage
        if snapshot.commits.isEmpty, !authorized.isEmpty {
            current = await scan(
                selected(authorized), previous: current, history: history, configuration: config, live: false)
            guard operation == generation, !Task.isCancelled else { return }
            current.cachedOnly = config.cachedOnly
            current.coverage.phase = "refreshing"
            current.coverage.message = "Cached history loaded; checking GitHub access…"
            snapshot = current
        }
        if config.cachedOnly {
            current = await scan(
                selected(authorized), previous: current, history: history, configuration: config, live: false)
            guard operation == generation, !Task.isCancelled else { return }
            current.pullRequests = []
            current.cachedOnly = true
            current.coverage.phase = current.coverage.errors.isEmpty ? "ready" : "partial"
            current.coverage.message = "Cached history. Pull-request evidence is unavailable offline."
            snapshot = current
            return
        }
        do {
            discoveryVerified = false
            guard let rows = try await verifyAccess(github, account: id, operation: operation) else { return }
            discovered = rows
            inventoryVerified = true
            discoveryVerified = true
            restrictSnapshot()
            current = snapshot
            let repos = selected(authorized)
            guard !configuration.repositories.isEmpty else {
                snapshot.coverage = Coverage(phase: "setup", message: "Choose repositories in Settings.")
                return
            }
            pendingGitDenials = []
            pendingGitAuthentication = false
            current = await scan(
                repos, previous: current, history: history, configuration: config, live: true)
            guard operation == generation, !Task.isCancelled else { return }
            if pendingGitAuthentication {
                await invalidateAuthentication(operation: operation)
                return
            }
            if !pendingGitDenials.isEmpty, operation == generation {
                authorized.removeAll { pendingGitDenials.contains($0.id) }
                try storage.invalidateGrant(id)
                try storage.saveGrant(authorized, account: id)
                current.repositories.removeAll { pendingGitDenials.contains($0.id) }
                current.commits.removeAll { pendingGitDenials.contains($0.repo) }
                current.pullRequests.removeAll { pendingGitDenials.contains($0.repo) }
            }
            guard operation == generation, !Task.isCancelled else { return }
            snapshot = current
            snapshot.coverage.message = "Loading pull-request evidence…"
            let prs = await github.fetch(
                current.repositories, budget: config.requestBudget, previous: current.pullRequests)
            guard operation == generation, !Task.isCancelled else { return }
            if prs.authenticationRequired {
                await invalidateAuthentication(operation: operation)
                return
            }
            current.repositories = prs.repositories
            current.pullRequests = prs.pullRequests
            current.coverage.errors += prs.coverage.errors
            current.coverage.probeRequests += prs.coverage.probeRequests
            current.coverage.inventoryRequests = prs.coverage.inventoryRequests
            current.coverage.fileRequests = prs.coverage.fileRequests
            current.coverage.accountRequests = prs.coverage.accountRequests
            if !prs.deniedRepositories.isEmpty {
                authorized.removeAll { prs.deniedRepositories.contains($0.id) }
                try storage.invalidateGrant(id)
                try storage.saveGrant(authorized, account: id)
            }
            current.coverage.phase = current.coverage.errors.isEmpty ? "ready" : "partial"
            current.coverage.message =
                current.coverage.errors.isEmpty
                ? "Repository evidence is current." : "Some evidence is incomplete. See Data Status."
            current.cachedOnly = false
            snapshot = current
            restrictSnapshot()
        } catch {
            guard operation == generation, !Task.isCancelled else { return }
            if error as? GitHubFailure == .auth {
                await invalidateAuthentication(operation: operation)
                return
            }
            restrictSnapshot()
            snapshot.coverage.phase = "partial"
            snapshot.coverage.message = "Refresh could not finish. Retained evidence may be stale."
            snapshot.coverage.errors.append(error.localizedDescription)
            for i in snapshot.repositories.indices {
                snapshot.repositories[i].stale = true
                snapshot.repositories[i].prStale = true
            }
            self.error = error.localizedDescription
        }
        let state = await auth.status()
        guard operation == generation, !Task.isCancelled else { return }
        authStatus = state
        if !state.signedIn { await invalidateAuthentication(operation: operation) }
    }
    private struct ScanOutcome: Sendable {
        enum Access: Sendable { case unchanged, denied, authenticationRequired }

        var repository: Repository
        var commits: [Commit]
        var access: Access = .unchanged
    }
    private func scan(
        _ repos: [Repository], previous: Snapshot, history: GitHistory, configuration: WorkspaceConfiguration,
        live: Bool
    ) async -> Snapshot {
        let scanGeneration = generation
        var result = Snapshot(generatedAt: Date(), accountID: previous.accountID, viewer: previous.viewer)
        result.pullRequests = previous.pullRequests
        result.cachedOnly = !live
        result.coverage.phase = "refreshing"
        for start in stride(from: 0, to: repos.count, by: 8) {
            if Task.isCancelled { break }
            let batch = Array(repos[start..<min(start + 8, repos.count)])
            let auth = self.auth
            let refreshHistory = self.refreshHistory
            let outcomes = await withTaskGroup(of: ScanOutcome.self, returning: [ScanOutcome].self) { group in
                for repo in batch {
                    group.addTask {
                        var current = repo
                        do {
                            if live { current = try await refreshHistory(history, repo, auth.token(validFor: GitProcess.transferTimeout)) }
                            let scanned = try await history.scan(current, configuration: configuration)
                            return ScanOutcome(repository: scanned.0, commits: scanned.1)
                        } catch {
                            current.stale = true
                            current.error = error.localizedDescription
                            let prior = previous.repositories.first { $0.id == repo.id }
                            current.revision = prior?.revision ?? ""
                            current.scannedAt = prior?.scannedAt
                            if let failure = error as? GitHubFailure, failure.isAccessDenied {
                                current.error = "Access denied: " + current.error
                                return ScanOutcome(
                                    repository: current, commits: [],
                                    access: failure == .auth ? .authenticationRequired : .denied)
                            }
                            if live, !Task.isCancelled,
                                let cached = try? await history.scan(repo, configuration: configuration)
                            {
                                current.revision = cached.0.revision
                                current.scannedAt = cached.0.scannedAt
                                return ScanOutcome(repository: current, commits: cached.1)
                            }
                            return ScanOutcome(
                                repository: current, commits: previous.commits.filter { $0.repo == repo.id })
                        }
                    }
                }
                var values: [ScanOutcome] = []
                for await value in group { values.append(value) }
                return values
            }
            for outcome in outcomes.sorted(by: { $0.repository.id < $1.repository.id }) {
                let repo = outcome.repository
                if scanGeneration == generation {
                    switch outcome.access {
                    case .unchanged: break
                    case .denied: pendingGitDenials.insert(repo.id)
                    case .authenticationRequired:
                        pendingGitAuthentication = true
                        pendingGitDenials.insert(repo.id)
                    }
                }
                result.repositories.append(repo)
                result.commits += outcome.commits
                if !repo.error.isEmpty { result.coverage.errors.append("\(repo.id): \(repo.error)") }
            }
            if scanGeneration == generation, pendingGitAuthentication { break }
        }
        let retained = Set(result.repositories.map(\.id))
        result.pullRequests.removeAll { !retained.contains($0.repo) }
        return result
    }
    private func invalidateAuthentication(operation: Int) async {
        guard operation == generation else { return }
        let id = account
        generation += 1
        let invalidation = generation
        refreshTask?.cancel()
        refreshTask = nil
        discoveryTask?.cancel()
        discoveryTask = nil
        busy = false
        authorized = []
        discovered = []
        cachedRepositoryRows = []
        configuration = WorkspaceConfiguration()
        snapshot = Snapshot()
        snapshot.coverage = Coverage(phase: "signin", message: GitHubFailure.auth.localizedDescription)
        account = ""
        history = nil
        github = nil
        activeGroup = ""
        pendingGitDenials = []
        pendingGitAuthentication = false
        inventoryVerified = false
        discoveryVerified = false
        checkingSignIn = false
        authStatus.signedIn = false
        authStatus.login = ""
        authStatus.accountID = ""
        authStatus.pending = false
        error = GitHubFailure.auth.localizedDescription
        if !id.isEmpty {
            do { try storage.invalidateGrant(id) } catch { self.error = error.localizedDescription }
        }
        await auth.invalidateSession()
        let state = await auth.status()
        guard invalidation == generation else { return }
        authStatus = state
        authStatus.signedIn = false
    }
    private func restrictSnapshot() {
        let allowed = Set(authorized.map(\.id))
        let collected = Set(selected(authorized).map(\.id))
        snapshot.repositories.removeAll { !collected.contains($0.id) }
        snapshot.commits.removeAll { !collected.contains($0.repo) }
        snapshot.pullRequests.removeAll { !collected.contains($0.repo) }
        discovered.removeAll { !allowed.contains($0.id) }
    }
    private func canRecordAccess(account id: String, operation: Int) -> Bool {
        account == id
            && (operation == generation || (shutdownOperation == operation && generation == operation + 1))
    }
    private func verifyAccess(_ github: GitHubService, account id: String, operation: Int) async throws
        -> [Repository]?
    {
        guard operation == generation, account == id, !Task.isCancelled else { return nil }
        let previousGrant = authorized
        let rows: [Repository]
        do {
            let state = try await auth.refreshViewer(using: github)
            guard operation == generation, !Task.isCancelled else { return nil }
            authStatus = state
            snapshot.viewer = state.login
            rows = try await github.discover()
        } catch {
            if canRecordAccess(account: id, operation: operation), let failure = error as? GitHubFailure,
                failure.isAccessDenied
            {
                if failure == .auth {
                    if operation == generation {
                        await invalidateAuthentication(operation: operation)
                    } else {
                        try storage.invalidateGrant(id)
                    }
                } else {
                    let remaining =
                        failure.installationOwner.map { owner in
                            previousGrant.filter {
                                $0.id.split(separator: "/").first.map(String.init) != owner
                            }
                        } ?? []
                    if operation == generation {
                        authorized = remaining
                        restrictSnapshot()
                    }
                    try storage.invalidateGrant(id)
                    try storage.saveGrant(remaining, account: id)
                }
            }
            throw error
        }
        guard canRecordAccess(account: id, operation: operation) else { return nil }
        if operation == generation {
            authorized = rows
            restrictSnapshot()
        }
        try storage.invalidateGrant(id)
        try storage.saveGrant(rows, account: id)
        guard operation == generation, !Task.isCancelled else { return nil }
        return rows
    }
    func discover() async {
        guard !busy, let github, !account.isEmpty else { return }
        busy = true
        error = ""
        discoveryVerified = false
        inventoryVerified = false
        let operation = generation, id = account
        let task = Task { await self.performDiscovery(github, account: id, operation: operation) }
        discoveryTask = task
        await withTaskCancellationHandler {
            await task.value
        } onCancel: {
            task.cancel()
        }
        if operation == generation {
            discoveryTask = nil
            busy = false
        }
    }
    private func performDiscovery(_ github: GitHubService, account id: String, operation: Int) async {
        do {
            guard let rows = try await verifyAccess(github, account: id, operation: operation) else { return }
            discovered = rows
            discoveryVerified = true
        } catch {
            guard operation == generation, !Task.isCancelled else { return }
            if error as? GitHubFailure == .auth {
                await invalidateAuthentication(operation: operation)
                return
            }
            self.error = error.localizedDescription
        }
    }
    @discardableResult
    func saveConfiguration(_ config: WorkspaceConfiguration) async -> Bool {
        guard !busy, !account.isEmpty, authStatus.signedIn else {
            error =
                busy
                ? "Wait for the current operation to finish before applying settings."
                : "Sign in before applying settings."
            return false
        }
        let operation = generation
        do {
            try storage.validate(config)
            guard !config.repositories.isEmpty else {
                throw BigboardError.message("Select at least one repository to track.")
            }
            if Set(config.repositories) != Set(configuration.repositories) {
                if !discoveryVerified {
                    await discover()
                    guard operation == generation, !Task.isCancelled else { return false }
                    guard discoveryVerified else {
                        if error.isEmpty {
                            error = "Repository access could not be verified. Try applying again."
                        }
                        return false
                    }
                }
                let unavailable = Set(config.repositories).subtracting(discovered.map(\.id)).sorted()
                guard unavailable.isEmpty else {
                    throw BigboardError.message(
                        "These selected repositories are not in the current access list: \(unavailable.joined(separator: ", ")). Refresh the list or remove them from your selection."
                    )
                }
            }
            try storage.saveConfiguration(config, account: account)
            configuration = config
            error = ""
            reconcileActiveGroup()
            restrictSnapshot()
            _ = beginRefresh()
            return true
        } catch {
            if operation == generation { self.error = error.localizedDescription }
            return false
        }
    }
    func saveIdentities(_ rules: [String: IdentityRule]) async {
        guard !busy, let history else { return }
        do {
            var next = configuration
            next.identities = rules
            try storage.saveConfiguration(next, account: account)
            configuration = next
            busy = true
            let operation = generation
            let old = snapshot
            let rescanned = await scan(
                old.repositories, previous: old, history: history, configuration: next, live: false)
            guard operation == generation else { return }
            snapshot.commits = rescanned.commits
            snapshot.coverage.errors += rescanned.coverage.errors
            busy = false
        } catch {
            self.error = error.localizedDescription
            busy = false
        }
    }
    private func reconcileActiveGroup() {
        if !activeGroup.isEmpty, configuration.groups[activeGroup] == nil { activeGroup = "" }
    }
    func selectGroup(_ name: String) async {
        guard !busy, name.isEmpty || configuration.groups[name] != nil else { return }
        activeGroup = name
        restrictSnapshot()
        await refresh()
    }
    func removeCache(_ id: String) async {
        guard !busy, inventoryVerified, !authorized.contains(where: { $0.id == id }), let history else {
            error = "Refresh live access before removing an unlisted cache."
            return
        }
        let operation = generation
        do {
            try await history.remove(id)
            let rows = (try? await history.cachedRepositories()) ?? []
            guard operation == generation else { return }
            cachedRepositoryRows = rows
        } catch { if operation == generation { self.error = error.localizedDescription } }
    }
    func cachedRepositories() async -> [Repository] {
        let operation = generation
        let rows = (try? await history?.cachedRepositories()) ?? []
        return operation == generation ? rows : []
    }
    func signIn() async {
        guard !checkingSignIn, !signingOut, !authStatus.pending, signInAttempt == nil else { return }
        let attempt = UUID()
        signInAttempt = attempt
        authStatus.pending = true
        defer { if signInAttempt == attempt { signInAttempt = nil } }
        let operation = generation
        error = ""
        do {
            let url = try await auth.start()
            let state = await auth.status()
            guard operation == generation, signInAttempt == attempt else { return }
            authStatus = state
            let callback = try await webAuthentication.authenticate(url: url, callbackScheme: auth.callbackScheme)
            guard operation == generation, signInAttempt == attempt else { return }
            try await auth.complete(callback)
            guard operation == generation, signInAttempt == attempt else { return }
            await finishSignIn()
        } catch {
            guard operation == generation, signInAttempt == attempt else { return }
            await auth.cancel()
            let state = await auth.status()
            guard operation == generation, signInAttempt == attempt else { return }
            if !(error is CancellationError) { self.error = error.localizedDescription }
            authStatus = state
        }
    }
    func cancelSignIn() async {
        let operation = generation
        signInAttempt = nil
        webAuthentication.cancel()
        await auth.cancel()
        let state = await auth.status()
        if operation == generation, signInAttempt == nil { authStatus = state }
    }
    @discardableResult
    func signOut() async -> Bool {
        guard !signingOut else { return false }
        signingOut = true
        if signInAttempt != nil { await cancelSignIn() }
        let interruptedRefresh = refreshTask != nil || snapshot.coverage.phase == "refreshing"
        generation += 1
        refreshTask?.cancel()
        refreshTask = nil
        discoveryTask?.cancel()
        discoveryTask = nil
        let operation = generation
        busy = true
        defer {
            signingOut = false
            if operation == generation { busy = false }
        }
        do { try await auth.signOut() } catch {
            let state = await auth.status()
            guard operation == generation else { return false }
            authStatus = state
            let retained =
                state.signedIn ? "You are still signed in." : "Your saved sign-in could not be removed."
            self.error =
                "Could not sign out. \(retained) \(error.localizedDescription) Try signing out again."
            if interruptedRefresh {
                snapshot.coverage.phase = "partial"
                snapshot.coverage.message = "Refresh canceled. Retained evidence may be stale."
                for index in snapshot.repositories.indices {
                    snapshot.repositories[index].stale = true
                    snapshot.repositories[index].prStale = true
                }
            }
            return false
        }
        guard operation == generation else { return false }
        snapshot = Snapshot()
        configuration = WorkspaceConfiguration()
        discovered = []
        authorized = []
        cachedRepositoryRows = []
        account = ""
        history = nil
        github = nil
        activeGroup = ""
        pendingGitDenials = []
        pendingGitAuthentication = false
        inventoryVerified = false
        discoveryVerified = false
        checkingSignIn = false
        error = ""
        authStatus.signedIn = false
        authStatus.login = ""
        authStatus.accountID = ""
        authStatus.pending = false
        authStatus.error = ""
        return true
    }
    func finishSignIn() async {
        var operation = generation
        do {
            generation += 1
            operation = generation
            refreshTask?.cancel()
            refreshTask = nil
            discoveryTask?.cancel()
            discoveryTask = nil
            busy = false
            account = ""
            history = nil
            github = nil
            snapshot = Snapshot()
            cachedRepositoryRows = []
            let state = await auth.status()
            guard operation == generation else { return }
            authStatus = state
            try connect()
            operation = generation
            try storage.invalidateGrant(account)
            authorized = []
            await refresh()
        } catch {
            let state = await auth.status()
            guard operation == generation else { return }
            self.error = error.localizedDescription
            authStatus = state
        }
    }
    func manageAccess() async {
        let operation = generation
        if let url = await auth.installationURL(), operation == generation { NSWorkspace.shared.open(url) }
    }
    func openPath(_ scope: ActivityScope) async throws -> URL {
        guard let history, let repo = snapshot.repositories.first(where: { $0.id == scope.repo }) else {
            throw BigboardError.message("Select a loaded repository.")
        }
        let operation = generation
        let url = try await history.pathURL(repo: repo, path: scope.path)
        guard operation == generation else { throw CancellationError() }
        return url
    }
    func shutdown() async {
        guard shutdownOperation == nil else { return }
        shuttingDown = true
        shutdownOperation = generation
        generation += 1
        let refresh = refreshTask, discovery = discoveryTask
        if signInAttempt != nil { await cancelSignIn() }
        refresh?.cancel()
        discovery?.cancel()
        await refresh?.value
        await discovery?.value
        shutdownOperation = nil
    }
    func exportJSON() throws -> Data {
        guard !snapshot.repositories.isEmpty else {
            throw BigboardError.message("Load repository history before exporting.")
        }
        return try ActivityExport.json(snapshot.commits, configuration: configuration)
    }
}
