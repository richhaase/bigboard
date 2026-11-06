import Foundation
import Testing
@testable import Bigboard

struct SettingsPendingInputTests {
    @Test func groupEditorReportsUnsavedInput() {
        var editor = SettingsGroupEditor()
        #expect(!editor.hasPendingInput)
        editor.name = "  "
        #expect(!editor.hasPendingInput)
        editor.repositories = "owner/app"
        #expect(editor.hasPendingInput)
        editor.name = "Focus"
        var groups: [String: [String]] = [:]
        let saved = editor.save(to: &groups)
        #expect(saved)
        #expect(groups == ["Focus": ["owner/app"]])
        #expect(!editor.hasPendingInput)
        editor.edit(name: "Focus", repositories: ["owner/app"])
        #expect(editor.hasPendingInput)
    }
    @Test func workAreaEditorReportsUnsavedInput() {
        var editor = SettingsWorkAreaEditor()
        #expect(!editor.hasPendingInput)
        editor.paths = "src"
        #expect(editor.hasPendingInput)
        var areas: [String: [WorkArea]] = [:]
        let rejected = editor.save(to: &areas)
        #expect(!rejected)
        #expect(!editor.error.isEmpty)
        editor.repository = "owner/app"
        editor.name = "UI"
        let accepted = editor.save(to: &areas)
        #expect(accepted)
        #expect(!editor.hasPendingInput)
    }
}
