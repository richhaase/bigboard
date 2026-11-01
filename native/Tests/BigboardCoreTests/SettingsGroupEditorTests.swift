import Foundation
import Testing
@testable import Bigboard

struct SettingsGroupEditorTests {
    @Test func renameReplacesOriginalInDraftAndPreservesOtherGroups() {
        var saved = WorkspaceConfiguration()
        saved.groups = ["Focus": ["owner/app", "owner/library"], "Other": ["other/repo"]]
        var draft = saved
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: saved.groups["Focus"]!)
        editor.name = " Product "

        let didSave = editor.save(to: &draft.groups)
        #expect(didSave)

        #expect(draft.groups == ["Product": ["owner/app", "owner/library"], "Other": ["other/repo"]])
        #expect(saved.groups == ["Focus": ["owner/app", "owner/library"], "Other": ["other/repo"]])
        #expect(!editor.isEditing && !editor.canSave && editor.error.isEmpty)
        #expect(editor.name.isEmpty && editor.repositories.isEmpty)
    }
    @Test func occupiedNameRetainsDraftAndEditForCorrection() {
        var groups = ["Focus": ["owner/app"], "Existing": ["other/repo"]]
        let before = groups
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: groups["Focus"]!)
        editor.name = " Existing "
        editor.repositories = "owner/library, owner/app"

        let rejectedSave = editor.save(to: &groups)
        #expect(!rejectedSave)

        #expect(groups == before)
        #expect(editor.original == "Focus")
        #expect(editor.name == " Existing " && editor.repositories == "owner/library, owner/app")
        #expect(editor.error.contains("Existing"))
        editor.name = "Product"
        #expect(editor.error.isEmpty)
        let didSave = editor.save(to: &groups)
        #expect(didSave)
        #expect(groups == ["Product": ["owner/app", "owner/library"], "Existing": ["other/repo"]])
    }
    @Test func editingMembersWithoutRenamingReplacesOnlyThatGroup() {
        var groups = ["Focus": ["owner/app"], "Other": ["other/repo"]]
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: groups["Focus"]!)
        editor.repositories = " Owner/Library, OWNER/Service\nowner/library "

        let didSave = editor.save(to: &groups)
        #expect(didSave)

        #expect(groups == ["Focus": ["owner/library", "owner/service"], "Other": ["other/repo"]])
        #expect(!editor.isEditing)
    }
    @Test(arguments: [false, true]) func newModeAddsOrUpdatesMatchingName(updateExisting: Bool) {
        var groups = ["Other": ["other/repo"]]
        if updateExisting { groups["Focus"] = ["owner/old"] }
        var editor = SettingsGroupEditor()
        editor.name = " Focus "
        editor.repositories = " Owner/App, OWNER/Library\nowner/app "

        let didSave = editor.save(to: &groups)
        #expect(didSave)

        #expect(groups == ["Focus": ["owner/app", "owner/library"], "Other": ["other/repo"]])
        #expect(!editor.isEditing && !editor.canSave)
    }
    @Test func cancelEditClearsErrorAndIdentityWithoutChangingDraft() {
        var groups = ["Focus": ["owner/app"], "Other": ["other/repo"]]
        let before = groups
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: groups["Focus"]!)
        editor.name = "Other"
        editor.repositories = "owner/library"
        let rejectedSave = editor.save(to: &groups)
        #expect(!rejectedSave)

        editor.reset()

        #expect(!editor.isEditing && !editor.canSave && editor.error.isEmpty)
        #expect(editor.name.isEmpty && editor.repositories.isEmpty)
        #expect(groups == before)
        editor.name = "Product"
        editor.repositories = "owner/library"
        let didSave = editor.save(to: &groups)
        #expect(didSave)
        #expect(groups == ["Focus": ["owner/app"], "Other": ["other/repo"], "Product": ["owner/library"]])
    }
    @Test func removingEditedGroupResetsEditorButRemovingAnotherDoesNot() {
        var groups = ["Focus": ["owner/app"], "Other": ["other/repo"]]
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: groups["Focus"]!)
        editor.name = "Product"

        editor.remove(name: "Other", from: &groups)
        #expect(editor.isEditing && editor.name == "Product")
        #expect(groups == ["Focus": ["owner/app"]])

        editor.remove(name: "Focus", from: &groups)
        #expect(!editor.isEditing && !editor.canSave && editor.error.isEmpty)
        #expect(editor.name.isEmpty && editor.repositories.isEmpty)
        #expect(groups.isEmpty)
    }
    @Test func editingAnotherGroupReplacesOriginalIdentityAndClearsError() {
        var groups = ["Focus": ["owner/app"], "Other": ["other/repo"]]
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: groups["Focus"]!)
        editor.name = "Other"
        let rejectedSave = editor.save(to: &groups)
        #expect(!rejectedSave)

        editor.edit(name: "Other", repositories: groups["Other"]!)

        #expect(editor.original == "Other" && editor.error.isEmpty)
        #expect(editor.name == "Other" && editor.repositories == "other/repo")
        editor.name = "Product"
        let didSave = editor.save(to: &groups)
        #expect(didSave)
        #expect(groups == ["Focus": ["owner/app"], "Product": ["other/repo"]])
    }
    @Test func removedOriginalCannotBeRecreatedBySavingStaleEdit() {
        var groups = ["Other": ["other/repo"]]
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: ["owner/app"])
        editor.name = "Product"

        let rejectedSave = editor.save(to: &groups)
        #expect(!rejectedSave)

        #expect(groups == ["Other": ["other/repo"]])
        #expect(editor.original == "Focus" && editor.name == "Product")
        #expect(editor.error.contains("removed"))
    }
    @Test @MainActor func editedGroupAppliesAndReopensWithoutOriginalOrChangingOtherAccount() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let storage = WorkspaceStorage(supportRoot: root.appendingPathComponent("support"), cacheRoot: root.appendingPathComponent("cache"))
        var original = WorkspaceConfiguration()
        original.repositories = ["owner/app", "owner/library", "other/repo"]
        original.groups = ["Focus": ["owner/app"], "Other": ["owner/library"]]
        original.cachedOnly = true
        try storage.saveConfiguration(original, account: "123")
        try storage.saveConfiguration(original, account: "456")
        try storage.saveGrant(original.repositories.map { Repository(id: $0) }, account: "123")
        let now = Date()
        let credentials = AuthCredentials(accessToken: "fixture-access", refreshToken: "fixture-refresh", expiresAt: now.addingTimeInterval(3600), refreshExpiresAt: now.addingTimeInterval(86400), login: "fixture", accountID: "123", clientID: "fixture-client")
        let encoder = JSONEncoder(); encoder.dateEncodingStrategy = .iso8601
        let transport = GroupEditorTransport()
        let auth = AuthManager(configuration: AuthConfiguration(clientID: "fixture-client", clientSecret: "fixture-secret", appSlug: "fixture-app"), store: GroupEditorCredentials(value: try encoder.encode(credentials)), transport: transport)
        let workspace = WorkspaceModel(storage: storage, authentication: auth, transport: transport)
        await workspace.start()
        #expect(workspace.authStatus.signedIn)
        var draft = workspace.configuration
        var editor = SettingsGroupEditor()
        editor.edit(name: "Focus", repositories: draft.groups["Focus"]!)
        editor.name = "Product"
        editor.repositories = " Owner/Library, OWNER/App\nowner/library "
        let didSave = editor.save(to: &draft.groups)
        #expect(didSave)
        draft = SettingsRepositories.applying(Set(original.repositories), available: [], to: draft)
        #expect(workspace.configuration == original)
        #expect(try storage.loadConfiguration("123") == original)

        let applied = await workspace.saveConfiguration(draft)
        for _ in 0..<100 {
            if !workspace.busy { break }
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(!workspace.busy)
        await workspace.selectGroup("Product")
        #expect(workspace.activeGroup == "Product")
        #expect(Set(workspace.snapshot.repositories.map(\.id)) == ["owner/app", "owner/library"])
        await workspace.shutdown()

        #expect(applied)
        #expect(workspace.configuration.groups == ["Product": ["owner/app", "owner/library"], "Other": ["owner/library"]])
        let reopened = WorkspaceStorage(supportRoot: storage.supportRoot, cacheRoot: storage.cacheRoot)
        #expect(try reopened.loadConfiguration("123") == draft)
        #expect(try reopened.loadConfiguration("456") == original)
        #expect(await transport.count == 0)
    }
}

private struct GroupEditorCredentials: CredentialStore {
    var value: Data
    func load() throws -> Data? { value }
    func save(_ data: Data) throws { throw AuthFailure.storage }
    func delete() throws { throw AuthFailure.storage }
}
private actor GroupEditorTransport: GitHubTransport {
    private(set) var count = 0
    func send(_ request: URLRequest, maximumBytes: Int) async throws -> GitHubHTTPResponse {
        count += 1
        throw GitHubFailure.unavailable
    }
}
