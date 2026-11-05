import Foundation
import Testing
@testable import Bigboard

@Test func settingsRepositoriesSortByLatestPushWithoutDroppingUnavailableChoices() {
    let rows = [Repository(id: "owner/alpha", pushedAt: Date(timeIntervalSince1970: 100)), Repository(id: "owner/zeta", pushedAt: Date(timeIntervalSince1970: 200)), Repository(id: "owner/unknown")]
    let recent = SettingsRepositories.rows(available: rows, saved: ["other/saved"], sort: .recent, query: "")
    #expect(recent.map(\.id) == ["owner/zeta", "owner/alpha", "other/saved", "owner/unknown"])
    #expect(recent.first?.pushedAt == Date(timeIntervalSince1970: 200))
    #expect(SettingsRepositories.rows(available: rows, saved: ["other/saved"], sort: .name, query: "").map(\.id) == ["other/saved", "owner/alpha", "owner/unknown", "owner/zeta"])
    #expect(SettingsRepositories.rows(available: rows, saved: ["other/saved"], sort: .recent, query: "ALPHA").map(\.id) == ["owner/alpha"])
}
