import Foundation

enum ConfigurationError: LocalizedError {
    case invalidFile
    case invalidValue(String)

    var errorDescription: String? {
        switch self {
        case .invalidFile:
            "Cannot read publisher settings JSON. Check its path and string fields."
        case .invalidValue(let key):
            "Missing or malformed publisher setting: \(key)"
        }
    }
}

func configurePublisher() throws {
    let environment = ProcessInfo.processInfo.environment
    let sourceRoot = environment["SRCROOT"] ?? FileManager.default.currentDirectoryPath
    let configurationPath = environment["BIGBOARD_PUBLISHER_CONFIG"]
        ?? "\(sourceRoot)/publisher.local.json"
    var saved: [String: Any] = [:]
    if FileManager.default.fileExists(atPath: configurationPath) {
        do {
            let data = try Data(contentsOf: URL(fileURLWithPath: configurationPath))
            guard let values = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                throw ConfigurationError.invalidFile
            }
            saved = values
        } catch {
            throw ConfigurationError.invalidFile
        }
    }

    let keys = ["BIGBOARD_GITHUB_CLIENT_ID", "BIGBOARD_GITHUB_CLIENT_SECRET", "BIGBOARD_GITHUB_APP_SLUG"]
    var values: [String: String] = [:]
    for key in keys {
        if let value = saved[key], !(value is String) {
            throw ConfigurationError.invalidFile
        }
        values[key] = environment[key] ?? saved[key] as? String ?? ""
    }
    if environment["BIGBOARD_REQUIRE_AUTH"] == "YES" || values.values.contains(where: { !$0.isEmpty }) {
        for key in keys {
            let value = values[key] ?? ""
            let valid: Bool
            if key == "BIGBOARD_GITHUB_APP_SLUG" {
                valid = !value.isEmpty && value.utf8.count <= 100
                    && value.utf8.allSatisfy { (97...122).contains($0) || (48...57).contains($0) || $0 == 45 }
            } else {
                valid = !value.isEmpty && value.utf8.count <= 4096
                    && value.utf8.allSatisfy { (33...126).contains($0) }
            }
            guard valid else { throw ConfigurationError.invalidValue(key) }
        }
    }

    guard CommandLine.arguments.count == 2 else {
        throw ConfigurationError.invalidFile
    }
    let plistURL = URL(fileURLWithPath: CommandLine.arguments[1])
    let data = try Data(contentsOf: plistURL)
    guard var plist = try PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any] else {
        throw ConfigurationError.invalidFile
    }
    for key in keys {
        plist[key] = values[key].flatMap { $0.isEmpty ? nil : $0 }
    }
    let updated = try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0)
    try updated.write(to: plistURL, options: .atomic)
}

do {
    try configurePublisher()
} catch {
    FileHandle.standardError.write(Data("Publisher configuration failed: \(error.localizedDescription)\n".utf8))
    exit(1)
}
