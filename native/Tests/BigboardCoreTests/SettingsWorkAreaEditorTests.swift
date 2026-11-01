import Testing
@testable import Bigboard

struct SettingsWorkAreaEditorTests {
    @Test func renameReplacesOriginalInDraftAndPreservesOtherAreas() {
        let original = WorkArea(name: "Sources", paths: ["src/"])
        let sibling = WorkArea(name: "Tests", paths: ["tests"])
        var saved = WorkspaceConfiguration()
        saved.workAreas = ["owner/repo": [original, sibling], "other/repo": [original]]
        var draft = saved
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.name = " Engine "

        let didSave = editor.save(to: &draft.workAreas)
        #expect(didSave)

        #expect(draft.workAreas["owner/repo"] == [WorkArea(name: "Engine", paths: ["src/"]), sibling])
        #expect(draft.workAreas["other/repo"] == [original])
        #expect(saved.workAreas["owner/repo"] == [original, sibling])
        #expect(!editor.isEditing)
        #expect(editor.repository.isEmpty && editor.name.isEmpty && editor.paths.isEmpty)
    }
    @Test(arguments: [false, true]) func moveReplacesOriginalAndPreservesRepositoryContents(keepSourceSibling: Bool) {
        let original = WorkArea(name: "Sources", paths: ["src"])
        let sibling = WorkArea(name: "Tests", paths: ["tests"])
        let destination = WorkArea(name: "Docs", paths: ["docs"])
        var workAreas = ["owner/repo": keepSourceSibling ? [original, sibling] : [original], "other/repo": [destination], "third/repo": [sibling]]
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.repository = " other/repo "
        editor.name = "Engine"
        editor.paths = "src/core, tools"

        let didSave = editor.save(to: &workAreas)
        #expect(didSave)

        #expect(workAreas["owner/repo"] == (keepSourceSibling ? [sibling] : nil))
        #expect(workAreas["other/repo"] == [destination, WorkArea(name: "Engine", paths: ["src/core", "tools"])])
        #expect(workAreas["third/repo"] == [sibling])
    }
    @Test func movingWithoutRenamingCreatesDestinationRepository() {
        let original = WorkArea(name: "Sources", paths: ["src"])
        var workAreas = ["owner/repo": [original]]
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.repository = "other/repo"

        let didSave = editor.save(to: &workAreas)
        #expect(didSave)

        #expect(workAreas == ["other/repo": [original]])
    }
    @Test func editingPathsWithoutChangingIdentityPreservesPosition() {
        let original = WorkArea(name: "Sources", paths: ["src"])
        let sibling = WorkArea(name: "Tests", paths: ["tests"])
        var workAreas = ["owner/repo": [original, sibling]]
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.paths = "src/core"

        let didSave = editor.save(to: &workAreas)
        #expect(didSave)

        #expect(workAreas["owner/repo"] == [WorkArea(name: "Sources", paths: ["src/core"]), sibling])
    }
    @Test(arguments: ["owner/repo", "other/repo"], [["src/comma,name", "src/line\nbreak", "src/trailing ", " leading", "src/"], ["   "]]) func renameOrMovePreservesUneditedLiteralPaths(destinationRepository: String, literalPaths: [String]) {
        let original = WorkArea(name: "Sources", paths: literalPaths)
        var workAreas = ["owner/repo": [original]]
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.repository = destinationRepository
        editor.name = "Engine"

        let didSave = editor.save(to: &workAreas)
        #expect(didSave)

        #expect(workAreas == [destinationRepository: [WorkArea(name: "Engine", paths: original.paths)]])
    }
    @Test func intentionalUnicodePathEditPreservesEditedBytes() {
        let original = WorkArea(name: "Sources", paths: ["src/caf\u{00e9}"])
        let editedPath = "src/cafe\u{0301}"
        var workAreas = ["owner/repo": [original]]
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.paths = editedPath

        let didSave = editor.save(to: &workAreas)
        #expect(didSave)

        let savedPath = workAreas["owner/repo"]?.first?.paths.first ?? ""
        #expect(savedPath.utf8.elementsEqual(editedPath.utf8))
    }
    @Test(arguments: ["owner/repo", "other/repo"]) func occupiedDestinationRetainsDraftAndEditForCorrection(destinationRepository: String) {
        let original = WorkArea(name: "Sources", paths: ["src"])
        let occupied = WorkArea(name: "Existing", paths: ["docs"])
        var workAreas = ["owner/repo": [original, occupied], "other/repo": [occupied]]
        let before = workAreas
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.repository = destinationRepository
        editor.name = " Existing "
        editor.paths = "src/core"

        let savedCollision = editor.save(to: &workAreas)
        #expect(!savedCollision)

        #expect(workAreas == before)
        #expect(editor.isEditing)
        #expect(editor.repository == destinationRepository && editor.name == " Existing " && editor.paths == "src/core")
        #expect(editor.error.contains("Existing") && editor.error.contains(destinationRepository))
        editor.name = "Engine"
        #expect(editor.error.isEmpty)
        let didSave = editor.save(to: &workAreas)
        #expect(didSave)
        #expect(!(workAreas["owner/repo"] ?? []).contains { $0.name == original.name })
        #expect(workAreas[destinationRepository]?.contains(WorkArea(name: "Engine", paths: ["src/core"])) == true)
        #expect(workAreas["owner/repo"]?.contains(occupied) == true)
        #expect(workAreas["other/repo"]?.contains(occupied) == true)
    }
    @Test(arguments: [false, true]) func newModeAddsOrUpdatesMatchingIdentity(updateExisting: Bool) {
        let original = WorkArea(name: "Sources", paths: ["src"])
        let sibling = WorkArea(name: "Tests", paths: ["tests"])
        var workAreas = ["owner/repo": updateExisting ? [original, sibling] : [sibling], "other/repo": [original]]
        var editor = SettingsWorkAreaEditor()
        editor.repository = "owner/repo"
        editor.name = "Sources"
        editor.paths = "src/core, tools"

        let didSave = editor.save(to: &workAreas)
        #expect(didSave)

        let updated = WorkArea(name: "Sources", paths: ["src/core", "tools"])
        #expect(workAreas["owner/repo"] == (updateExisting ? [updated, sibling] : [sibling, updated]))
        #expect(workAreas["other/repo"] == [original])
        #expect(!editor.isEditing && !editor.canSave)
    }
    @Test func cancelEditLeavesDraftUnchangedAndResetsIdentity() {
        let original = WorkArea(name: "Sources", paths: ["src"])
        var workAreas = ["owner/repo": [original]]
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.name = "Renamed"

        editor.reset()

        #expect(!editor.isEditing && !editor.canSave && editor.error.isEmpty)
        #expect(editor.repository.isEmpty && editor.name.isEmpty && editor.paths.isEmpty)
        #expect(workAreas == ["owner/repo": [original]])
        editor.repository = "owner/repo"
        editor.name = "Docs"
        editor.paths = "docs"
        let didSave = editor.save(to: &workAreas)
        #expect(didSave)
        #expect(workAreas["owner/repo"] == [original, WorkArea(name: "Docs", paths: ["docs"])])
    }
    @Test func removingEditedAreaResetsEditorButRemovingAnotherDoesNot() {
        let original = WorkArea(name: "Sources", paths: ["src"])
        let sibling = WorkArea(name: "Tests", paths: ["tests"])
        var workAreas = ["owner/repo": [original, sibling]]
        var editor = SettingsWorkAreaEditor()
        editor.edit(repository: "owner/repo", area: original)
        editor.name = "Renamed"

        editor.remove(repository: "owner/repo", name: sibling.name, from: &workAreas)
        #expect(editor.isEditing && editor.name == "Renamed")
        #expect(workAreas["owner/repo"] == [original])

        editor.remove(repository: "owner/repo", name: original.name, from: &workAreas)
        #expect(!editor.isEditing && !editor.canSave)
        #expect(workAreas.isEmpty)
    }
}
