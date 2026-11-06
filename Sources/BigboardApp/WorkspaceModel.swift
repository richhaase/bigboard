import Foundation
import Observation

@MainActor @Observable
final class WorkspaceModel {
    var snapshot = Snapshot()
    var configuration = WorkspaceConfiguration()
    var authStatus = AuthStatus()
    var checkingSignIn = true
    var busy = false
    var error = ""
    var discovered: [Repository] = []
    var activeGroup = ""
    var storageDescription: String {
        "Settings: \(storage.configurationFile.path)\nHistory: \(storage.cacheRoot.path)"
    }
    private let storage: WorkspaceStorage
    private let auth: GitHubCLIAuth
    private let transport: any GitHubTransport
    private let now: @Sendable () -> Date
    private var lastRefreshFinished: Date?
    private var shuttingDown = false
    private let refreshHistory: @Sendable (GitHistory, Repository) async throws -> Repository
    private var github: GitHubService?
    private var history: GitHistory?
    private var account = ""
    private var generation = 0
    private var refreshTask: Task<Void, Never>?
    private var discoveryTask: Task<Void, Never>?
    private var started = false
    init(
        storage: WorkspaceStorage = .system(), authentication: GitHubCLIAuth? = nil,
        transport: any GitHubTransport = URLSessionGitHubTransport(),
        now: @escaping @Sendable () -> Date = Date.init,
        refreshHistory: @escaping @Sendable (GitHistory, Repository) async throws -> Repository = {
            try await $0.refresh($1)
        }
    ) {
        self.storage = storage
        self.transport = transport
        self.now = now
        self.refreshHistory = refreshHistory
        self.auth = authentication ?? GitHubCLIAuth(storage: storage, transport: transport, now: now)
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
            throw BigboardError.message("Connect GitHub CLI first.")
        }
        if account == authStatus.accountID, history != nil { return }
        generation += 1
        account = authStatus.accountID
        lastRefreshFinished = nil
        configuration = try storage.loadConfiguration()
        discovered = []
        activeGroup = ""
        history = GitHistory(root: storage.historyRoot)
        let manager = auth
        github = GitHubService(token: { try await manager.token() }, transport: transport)
        snapshot = Snapshot(accountID: account, viewer: authStatus.login)
    }
    func refresh() async {
        await beginRefresh()?.value
    }
    func nextAutomaticRefresh(visible: Bool, modalOpen: Bool) -> Date? {
        guard visible, !modalOpen, !shuttingDown, !checkingSignIn, !busy,
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
        snapshot.coverage.message = "Refreshing…"
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
        configuration.collected(repositories, group: activeGroup)
    }
    private func performRefresh(operation: Int) async {
        guard operation == generation, let history, let github, !account.isEmpty else { return }
        let id = account, config = configuration
        error = ""
        guard !config.sourceRoots.isEmpty else {
            snapshot.coverage = Coverage(phase: "setup", message: "Choose the folders that hold your Git clones.")
            return
        }
        snapshot.coverage = Coverage(phase: "refreshing", message: "Finding clones…")
        guard await findClones(operation: operation, roots: config.sourceRoots) else { return }
        var current = snapshot
        current.cachedOnly = config.cachedOnly
        current.coverage = Coverage(phase: "refreshing", message: "Loading cached history…")
        snapshot.coverage = current.coverage
        if config.cachedOnly || snapshot.commits.isEmpty {
            current = await scan(
                selected(discovered), previous: current, history: history, configuration: config, live: false)
            guard operation == generation, !Task.isCancelled else { return }
            current.cachedOnly = config.cachedOnly
            if config.cachedOnly {
                current.pullRequests = []
                current.coverage.phase = current.coverage.errors.isEmpty ? "ready" : "partial"
                current.coverage.message = "Cached history. Pull-request evidence is unavailable offline."
                snapshot = current
                return
            }
            current.coverage.phase = "refreshing"
            current.coverage.message = "Cached history loaded; checking GitHub access…"
            snapshot = current
        }
        do {
            guard try await verifyViewer(github, account: id, operation: operation) else { return }
            current = snapshot
            let repos = selected(discovered)
            guard !configuration.repositories.isEmpty else {
                snapshot.coverage = Coverage(phase: "setup", message: "Choose repositories in Settings.")
                return
            }
            current = await scan(
                repos, previous: current, history: history, configuration: config, live: true)
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
            current.coverage.inventoryRequests = prs.coverage.inventoryRequests
            current.coverage.fileRequests = prs.coverage.fileRequests
            current.coverage.phase = current.coverage.errors.isEmpty ? "ready" : "partial"
            current.coverage.message =
                current.coverage.errors.isEmpty
                ? "Repository evidence is current." : "Some evidence is incomplete. See Data Status."
            current.cachedOnly = false
            snapshot = current
            restrictSnapshot()
        } catch {
            guard operation == generation, !Task.isCancelled else { return }
            if error as? GitHubFailure == .auth || error is GitHubCLIFailure {
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
    }
    private struct ScanOutcome: Sendable {
        var repository: Repository
        var commits: [Commit]
    }
    private func scan(
        _ repos: [Repository], previous: Snapshot, history: GitHistory, configuration: WorkspaceConfiguration,
        live: Bool
    ) async -> Snapshot {
        var result = Snapshot(generatedAt: Date(), accountID: previous.accountID, viewer: previous.viewer)
        result.pullRequests = previous.pullRequests
        result.cachedOnly = !live
        result.coverage.phase = "refreshing"
        for start in stride(from: 0, to: repos.count, by: 8) {
            if Task.isCancelled { break }
            let batch = Array(repos[start..<min(start + 8, repos.count)])
            let refreshHistory = self.refreshHistory
            let outcomes = await withTaskGroup(of: ScanOutcome.self, returning: [ScanOutcome].self) { group in
                for repo in batch {
                    group.addTask {
                        var current = repo
                        do {
                            if live { current = try await refreshHistory(history, repo) }
                            let scanned = try await history.scan(current, configuration: configuration)
                            return ScanOutcome(repository: scanned.0, commits: scanned.1)
                        } catch {
                            current.stale = true
                            current.error = error.localizedDescription
                            let prior = previous.repositories.first { $0.id == repo.id }
                            current.revision = prior?.revision ?? ""
                            current.scannedAt = prior?.scannedAt
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
                var repo = outcome.repository
                if let prior = previous.repositories.first(where: { $0.id == repo.id }) {
                    repo.prCheckedAt = prior.prCheckedAt
                    repo.prInventoryComplete = prior.prInventoryComplete
                    repo.prFilesComplete = prior.prFilesComplete
                    repo.prReviewersComplete = prior.prReviewersComplete
                    repo.prStale = prior.prStale
                }
                result.repositories.append(repo)
                result.commits += outcome.commits
                if !repo.error.isEmpty { result.coverage.errors.append("\(repo.id): \(repo.error)") }
            }
        }
        let retained = Set(result.repositories.map(\.id))
        result.pullRequests.removeAll { !retained.contains($0.repo) }
        return result
    }
    private func invalidateAuthentication(operation: Int) async {
        guard operation == generation else { return }
        generation += 1
        let invalidation = generation
        refreshTask?.cancel()
        refreshTask = nil
        discoveryTask?.cancel()
        discoveryTask = nil
        busy = false
        discovered = []
        configuration = WorkspaceConfiguration()
        snapshot = Snapshot()
        snapshot.coverage = Coverage(phase: "signin", message: GitHubFailure.auth.localizedDescription)
        account = ""
        history = nil
        github = nil
        activeGroup = ""
        checkingSignIn = false
        authStatus.signedIn = false
        authStatus.login = ""
        authStatus.accountID = ""
        error = GitHubFailure.auth.localizedDescription
        await auth.invalidate()
        guard invalidation == generation else { return }
        authStatus.error = error
    }
    private func restrictSnapshot() {
        let collected = Set(selected(discovered).map(\.id))
        snapshot.repositories.removeAll { !collected.contains($0.id) }
        snapshot.commits.removeAll { !collected.contains($0.repo) }
        snapshot.pullRequests.removeAll { !collected.contains($0.repo) }
    }
    private func findClones(operation: Int, roots: [String]) async -> Bool {
        let rows = await CloneDiscovery.find(in: CloneDiscovery.roots(roots))
        guard operation == generation, !Task.isCancelled else { return false }
        discovered = rows
        restrictSnapshot()
        return true
    }
    private func verifyViewer(_ github: GitHubService, account id: String, operation: Int) async throws -> Bool {
        guard operation == generation, account == id, !Task.isCancelled else { return false }
        let state = try await auth.verify(using: github, account: id)
        guard operation == generation, !Task.isCancelled else { return false }
        authStatus = state
        snapshot.viewer = state.login
        return true
    }
    func discover(roots: [String]? = nil) async {
        guard !busy, !account.isEmpty else { return }
        busy = true
        error = ""
        let operation = generation
        let folders = roots ?? configuration.sourceRoots
        let task = Task { _ = await self.findClones(operation: operation, roots: folders) }
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
    @discardableResult
    func addSourceFolders(_ folders: [String]) async -> Bool {
        guard !busy, !account.isEmpty, authStatus.signedIn else { return false }
        var config = configuration
        for folder in folders where !config.sourceRoots.contains(folder) { config.sourceRoots.append(folder) }
        guard config.sourceRoots != configuration.sourceRoots else { return true }
        let operation = generation
        do {
            try storage.saveConfiguration(config)
            configuration = config
            error = ""
        } catch {
            self.error = error.localizedDescription
            return false
        }
        await discover(roots: config.sourceRoots)
        guard operation == generation, !Task.isCancelled else { return false }
        if !config.repositories.isEmpty { _ = beginRefresh() }
        return true
    }
    @discardableResult
    func saveConfiguration(_ config: WorkspaceConfiguration) async -> Bool {
        guard !busy, !account.isEmpty, authStatus.signedIn else {
            error =
                busy
                ? "Wait for the current operation to finish before applying settings."
                : "Connect GitHub CLI before applying settings."
            return false
        }
        let operation = generation
        do {
            try storage.validate(config)
            guard !config.repositories.isEmpty else {
                throw BigboardError.message("Select at least one repository to track.")
            }
            let foldersChanged = config.sourceRoots != configuration.sourceRoots
            if foldersChanged || Set(config.repositories) != Set(configuration.repositories) {
                if foldersChanged || discovered.isEmpty {
                    await discover(roots: config.sourceRoots)
                    guard operation == generation, !Task.isCancelled else { return false }
                }
                let unavailable = Set(config.repositories).subtracting(discovered.map(\.id)).sorted()
                guard unavailable.isEmpty else {
                    throw BigboardError.message(
                        "No local clone was found for: \(unavailable.joined(separator: ", ")). Add their folder under Source folders, refresh the list, or remove them from your selection."
                    )
                }
            }
            try storage.saveConfiguration(config)
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
            try storage.saveConfiguration(next)
            configuration = next
            busy = true
            let operation = generation
            let old = snapshot
            let task = Task {
                let rescanned = await self.scan(
                    old.repositories, previous: old, history: history, configuration: next, live: false)
                guard operation == self.generation else { return }
                var errors: [String] = []
                for error in old.coverage.errors + rescanned.coverage.errors where !errors.contains(error) {
                    errors.append(error)
                }
                self.snapshot.repositories = rescanned.repositories
                self.snapshot.commits = rescanned.commits
                self.snapshot.coverage.errors = errors
                self.refreshTask = nil
                self.busy = false
            }
            refreshTask = task
            await task.value
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
    func checkConnection() async {
        guard !checkingSignIn else { return }
        checkingSignIn = true
        let operation = generation
        error = ""
        await auth.invalidate()
        let state = await auth.status()
        guard operation == generation else { return }
        checkingSignIn = false
        authStatus = state
        if !state.error.isEmpty { error = state.error }
        guard state.signedIn else {
            snapshot.coverage.phase = "signin"
            return
        }
        if state.accountID == account, history != nil {
            await refresh()
        } else {
            await finishSignIn()
        }
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
            let state = await auth.status()
            guard operation == generation else { return }
            authStatus = state
            try connect()
            operation = generation
            await refresh()
        } catch {
            let state = await auth.status()
            guard operation == generation else { return }
            self.error = error.localizedDescription
            authStatus = state
        }
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
        guard !shuttingDown else { return }
        shuttingDown = true
        generation += 1
        let refresh = refreshTask, discovery = discoveryTask
        refresh?.cancel()
        discovery?.cancel()
        await refresh?.value
        await discovery?.value
    }
}
