import Foundation
import Testing

@Suite(.serialized)
struct PublisherConfigurationTests {
    private let keys = ["BIGBOARD_GITHUB_CLIENT_ID", "BIGBOARD_GITHUB_CLIENT_SECRET", "BIGBOARD_GITHUB_APP_SLUG"]
    private var settings: [String: String] {
        ["BIGBOARD_GITHUB_CLIENT_ID": "test-client", "BIGBOARD_GITHUB_CLIENT_SECRET": "test-secret", "BIGBOARD_GITHUB_APP_SLUG": "test-app"]
    }

    @Test func localSettingsAndEnvironmentPrecedence() throws {
        let result = try configure(saved: settings, environment: ["BIGBOARD_GITHUB_CLIENT_ID": "override-client"])
        #expect(result.status == 0)
        #expect(result.plist["BIGBOARD_GITHUB_CLIENT_ID"] as? String == "override-client")
        #expect(result.plist["BIGBOARD_GITHUB_CLIENT_SECRET"] as? String == "test-secret")
        #expect(result.plist["CFBundleIdentifier"] as? String == "test.bundle")
    }

    @Test func missingOptionalSettingsRemovePreviouslyEmbeddedValues() throws {
        let result = try configure(saved: nil)
        #expect(result.status == 0)
        for key in keys {
            #expect(result.plist[key] == nil)
        }
    }

    @Test func releaseRequiresAllSettings() throws {
        let result = try configure(saved: nil, environment: ["BIGBOARD_REQUIRE_AUTH": "YES"])
        #expect(result.status != 0)
        #expect(result.output.contains("BIGBOARD_GITHUB_CLIENT_ID"))
    }

    @Test func emptyEnvironmentValueOverridesSavedValue() throws {
        let result = try configure(saved: settings, environment: ["BIGBOARD_GITHUB_CLIENT_ID": ""])
        #expect(result.status != 0)
    }

    @Test func invalidFileAndValuesAreRejectedWithoutDisclosure() throws {
        let malformed = try configure(saved: ["BIGBOARD_GITHUB_CLIENT_SECRET": 42])
        #expect(malformed.status != 0)
        var invalid = settings
        invalid["BIGBOARD_GITHUB_CLIENT_SECRET"] = "private value with spaces"
        let result = try configure(saved: invalid)
        #expect(result.status != 0)
        #expect(!result.output.contains("private value with spaces"))
    }

    @Test func customConfigurationPathAndLiteralCharacters() throws {
        var values = settings
        let literal = #"value$()\"&<>"#
        values["BIGBOARD_GITHUB_CLIENT_SECRET"] = literal
        let result = try configure(saved: values, customPath: true)
        #expect(result.status == 0)
        #expect(result.plist["BIGBOARD_GITHUB_CLIENT_SECRET"] as? String == literal)
    }

    private func configure(
        saved: [String: Any]?,
        environment: [String: String] = [:],
        customPath: Bool = false
    ) throws -> (status: Int32, plist: [String: Any], output: String) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: root) }

        let settingsURL = root.appendingPathComponent(customPath ? "custom.json" : "publisher.local.json")
        if let saved {
            try JSONSerialization.data(withJSONObject: saved).write(to: settingsURL)
        }
        let plistURL = root.appendingPathComponent("Info.plist")
        var initial: [String: Any] = settings
        initial["CFBundleIdentifier"] = "test.bundle"
        try PropertyListSerialization.data(fromPropertyList: initial, format: .xml, options: 0)
            .write(to: plistURL)

        let repository = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/xcrun")
        process.arguments = ["swift", repository.appendingPathComponent("scripts/configure-publisher.swift").path, plistURL.path]
        process.environment = [
            "PATH": "/usr/bin:/bin",
            "DEVELOPER_DIR": ProcessInfo.processInfo.environment["DEVELOPER_DIR"] ?? "/Applications/Xcode.app/Contents/Developer",
            "SRCROOT": root.path,
        ].merging(environment) { _, new in new }
        if customPath {
            process.environment?["BIGBOARD_PUBLISHER_CONFIG"] = settingsURL.path
        }
        let output = Pipe()
        process.standardOutput = output
        process.standardError = output
        try process.run()
        let bytes = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        let plist = try #require(
            PropertyListSerialization.propertyList(from: Data(contentsOf: plistURL), format: nil) as? [String: Any]
        )
        return (process.terminationStatus, plist, String(decoding: bytes, as: UTF8.self))
    }
}
