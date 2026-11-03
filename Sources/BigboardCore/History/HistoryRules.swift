import Foundation
import Darwin

struct AttributeRule: Sendable {
    var pattern: String
    var generated: Bool
}
enum AttributeDecision: Sendable { case generated, included, unspecified }
enum HistoryRules {
    static let maximumAttributeBytes = 4 * 1024 * 1024
    static let maximumAttributeRules = 20_000
    static let maximumAttributeWork = 250_000_000
    static let ignoredDirectories: Set<String> = [
        "vendor", "node_modules", "dist", "build", ".next", "target", ".yarn", ".venv", "__pycache__", "Pods",
        "Carthage",
    ]
    static let ignoredGlobs = [
        "*.min.js", "*.min.css", "*.map", "*.snap", "*.lock", "*.pb.go", "*_pb2.py", "package-lock.json",
        "yarn.lock", "pnpm-lock.yaml", "go.sum", "Cargo.lock", "composer.lock", "Gemfile.lock", "poetry.lock",
    ]
    static let bulkExtensions: Set<String> = [
        "json", "ndjson", "jsonl", "yaml", "yml", "csv", "tsv", "xml", "po", "pot", "svg",
    ]
    static func glob(_ pattern: String, _ value: String) -> Bool {
        fnmatch(pattern, value, FNM_PATHNAME) == 0
    }
    static func attributes(
        _ contents: String, byteLimit: Int = maximumAttributeBytes, ruleLimit: Int = maximumAttributeRules,
        checkpoint: @Sendable () throws -> Void = {}
    ) throws -> [AttributeRule] {
        try checkpoint()
        guard contents.utf8.count <= byteLimit else {
            throw BigboardError.message("Repository attributes exceeded the byte limit.")
        }
        var rules: [AttributeRule] = []
        for line in contents.split(separator: "\n") {
            try checkpoint()
            let fields = line.split(whereSeparator: \.isWhitespace).map(String.init)
            guard fields.count > 1, let pattern = fields.first, !pattern.hasPrefix("#"),
                !pattern.hasPrefix("!"), !pattern.hasSuffix("/")
            else { continue }
            var generated: Bool?
            for raw in fields.dropFirst() {
                try checkpoint()
                if raw.hasPrefix("!") { continue }
                let pieces = raw.split(separator: "=", maxSplits: 1).map(String.init)
                var name = pieces[0], value = true
                if name.hasPrefix("-") {
                    name.removeFirst()
                    value = false
                } else if pieces.count == 2 {
                    guard ["true", "false", "0", "1"].contains(pieces[1]) else { continue }
                    value = pieces[1] == "true" || pieces[1] == "1"
                }
                if ["linguist-generated", "linguist-vendored"].contains(name), generated == nil || value {
                    generated = value
                }
            }
            if let generated {
                guard rules.count < ruleLimit else {
                    throw BigboardError.message("Repository attributes exceeded the rule limit.")
                }
                rules.append(AttributeRule(pattern: pattern, generated: generated))
            }
        }
        return rules
    }
    static func pathMatches(_ pattern: String, _ path: String) throws -> Bool {
        var remainingWork = maximumAttributeWork
        return try pathMatches(pattern, path, remainingWork: &remainingWork, checkpoint: {})
    }
    private static func pathMatches(
        _ pattern: String, _ path: String, remainingWork: inout Int, checkpoint: @Sendable () throws -> Void
    ) throws -> Bool {
        try checkpoint()
        let patternBytes = pattern.utf8.count, pathBytes = path.utf8.count
        guard patternBytes <= 64 * 1024, pathBytes <= 64 * 1024 else {
            throw BigboardError.message("Repository attribute matching exceeded the safety limit.")
        }
        let pathComponents = path.split(separator: "/")
        let patternComponents = pattern.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            .split(separator: "/")
        let work =
            pattern.contains("/")
            ? (patternBytes + 1) * (pathComponents.count + 1) + (pathBytes + 1) * patternComponents.count
            : patternBytes + pathBytes + 1
        guard work <= 1_000_000 else {
            throw BigboardError.message("Repository attribute matching exceeded the safety limit.")
        }
        guard work <= remainingWork else {
            throw BigboardError.message("Repository attribute matching exceeded the scan work limit.")
        }
        remainingWork -= work
        let p = patternComponents.map(String.init)
        let s = pathComponents.map(String.init)
        guard p.allSatisfy({ $0.utf8.count <= 4096 }), s.allSatisfy({ $0.utf8.count <= 4096 }) else {
            throw BigboardError.message("Repository attribute matching exceeded the safety limit.")
        }
        if !pattern.contains("/") { return glob(pattern, s.last ?? "") }
        var reachable = [Bool](repeating: false, count: s.count + 1)
        reachable[0] = true
        for component in p {
            try checkpoint()
            if component == "**" {
                for j in s.indices { reachable[j + 1] = reachable[j + 1] || reachable[j] }
            } else {
                for j in s.indices.reversed() { reachable[j + 1] = reachable[j] && glob(component, s[j]) }
                reachable[0] = false
            }
        }
        return reachable[s.count]
    }
    static func attributeDecision(
        _ path: String, attributes: [AttributeRule], remainingWork: inout Int,
        checkpoint: @Sendable () throws -> Void
    ) throws -> AttributeDecision {
        for attribute in attributes.reversed() {
            try checkpoint()
            if try pathMatches(attribute.pattern, path, remainingWork: &remainingWork, checkpoint: checkpoint)
            {
                return attribute.generated ? .generated : .included
            }
        }
        return .unspecified
    }
    static func exclusion(_ file: FileChange, allFiles: Bool, attribute: AttributeDecision) -> String {
        if allFiles { return "" }
        if attribute == .generated { return "attribute" }
        if attribute == .included { return "" }
        if file.path.split(separator: "/").contains(where: { ignoredDirectories.contains(String($0)) })
            || ignoredGlobs.contains(where: { glob($0, String(file.path.split(separator: "/").last ?? "")) })
        {
            return "path"
        }
        if !file.binary && file.added + file.removed >= 2000
            && bulkExtensions.contains((file.path as NSString).pathExtension.lowercased())
        {
            return "bulk"
        }
        return ""
    }
    static func normalizedEmail(_ raw: String) -> String {
        var value = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if let start = value.lastIndex(of: "<"), let end = value.lastIndex(of: ">"), start < end {
            value = String(value[value.index(after: start)..<end])
        }
        return value.trimmingCharacters(in: CharacterSet(charactersIn: "<> "))
    }
    static func isAI(_ raw: String, extra: [String]) -> Bool {
        let email = normalizedEmail(raw)
        if extra.contains(where: {
            let e = $0.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            return !e.isEmpty && (e.hasPrefix("@") ? email.hasSuffix(e) : email == e)
        }) {
            return true
        }
        if [
            "noreply@anthropic.com", "noreply@openai.com", "hi@cursor.com", "hi@cursor.sh",
            "cursoragent@cursor.com", "copilot@github.com", "devin@cognition.ai", "noreply@aider.chat",
            "bot@codium.ai",
        ]
        .contains(email) {
            return true
        }
        if email.hasSuffix("@users.noreply.github.com") {
            var local = String(email.split(separator: "@")[0].split(separator: "+").last ?? "")
            if local.hasSuffix("[bot]") { local = String(local.dropLast(5)) }
            return [
                "copilot", "copilot-swe-agent", "claude", "devin-ai-integration", "google-labs-jules",
                "cursoragent",
            ]
            .contains(local)
        }
        return false
    }
    static func isBot(name: String, email: String, extra: [String]) -> Bool {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(),
            email = normalizedEmail(email)
        if extra.contains(where: {
            let e = $0.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            return !e.isEmpty
                && (e.hasPrefix("@") ? email.hasSuffix(e) : e.contains("@") ? email == e : name == e)
        }) {
            return true
        }
        if name.contains("[bot]") || email.split(separator: "@").first?.contains("[bot]") == true {
            return true
        }
        let base = name.hasSuffix(" bot") ? String(name.dropLast(4)) : name
        return [
            "dependabot", "dependabot-preview", "renovate", "github-actions", "snyk-bot", "greenkeeper",
            "imgbot", "mergify", "allcontributors", "pre-commit-ci", "codecov",
        ]
        .contains(base)
    }
}
