import Foundation

final class GitLogParser: @unchecked Sendable {
    private var buffer = Data()
    private var offset = 0
    private var metadata: [String] = []
    private var pendingFile: FileChange?
    private var awaitingPreviousPath = false
    private let repo: String
    private let configuration: WorkspaceConfiguration
    private let attributes: [AttributeRule]
    private var remainingAttributeWork: Int
    private var attributeDecisions: [Data: AttributeDecision] = [:]
    private var cachedPathBytes = 0
    private var failure: (any Error)?
    private var identityMap: [String: (String, String)] = [:]
    private var skipping = false
    private(set) var records: [Commit] = []
    private(set) var skipped = 0
    init(
        repo: String, configuration: WorkspaceConfiguration, attributes: [AttributeRule] = [],
        matchingWorkLimit: Int = HistoryRules.maximumAttributeWork
    ) {
        self.repo = repo
        self.configuration = configuration
        self.attributes = attributes
        remainingAttributeWork = max(0, matchingWorkLimit)
        for (email, rule) in configuration.identities {
            for alias in [email] + rule.aliases {
                identityMap[alias.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()] = (
                    email.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(),
                    rule.name.trimmingCharacters(in: .whitespacesAndNewlines)
                )
            }
        }
    }
    func feed(_ data: Data, checkpoint: @Sendable () throws -> Void = {}) throws {
        if let failure { throw failure }
        do {
            try checkpoint()
            guard configuration.allFiles || attributes.count <= HistoryRules.maximumAttributeRules else {
                throw BigboardError.message("Repository attributes exceeded the rule limit.")
            }
            buffer.append(data)
            while let end = buffer[offset...].firstIndex(of: 0) {
                try checkpoint()
                guard end - offset <= 4 * 1024 * 1024 else {
                    throw BigboardError.message("Git history contains an oversized field.")
                }
                let token = String(decoding: buffer[offset..<end], as: UTF8.self)
                offset = end + 1
                try consume(token, checkpoint: checkpoint)
            }
            guard buffer.count - offset <= 4 * 1024 * 1024 else {
                throw BigboardError.message("Git history contains an oversized field.")
            }
            if offset > 1024 * 1024 {
                buffer.removeSubrange(0..<offset)
                offset = 0
            }
            try checkpoint()
        } catch {
            failure = error
            throw error
        }
    }
    func finish() throws -> [Commit] {
        if let failure { throw failure }
        guard buffer.count == offset, metadata.isEmpty, pendingFile == nil else {
            throw BigboardError.message("Git history ended before a complete record was received.")
        }
        return records
    }
    private func consume(_ raw: String, checkpoint: @Sendable () throws -> Void) throws {
        if !metadata.isEmpty {
            metadata.append(raw)
            if metadata.count == 6 {
                guard let seconds = Int64(metadata[3]) else {
                    skipped += 1
                    skipping = true
                    metadata = []
                    return
                }
                skipping = false
                var name = metadata[1].trimmingCharacters(in: .whitespacesAndNewlines),
                    email = metadata[2].trimmingCharacters(in: .whitespacesAndNewlines)
                if let mapped = identityMap[email.lowercased()] {
                    email = mapped.0
                    if !mapped.1.isEmpty { name = mapped.1 }
                }
                let ai =
                    HistoryRules.isAI(email, extra: configuration.aiIdentities)
                    || metadata[4].split(separator: "\u{1f}")
                        .contains { HistoryRules.isAI(String($0), extra: configuration.aiIdentities) }
                let commit = Commit(
                    id: metadata[0], repo: repo, author: name, email: email,
                    date: Date(timeIntervalSince1970: Double(seconds)), subject: metadata[5], ai: ai,
                    bot: HistoryRules.isBot(name: name, email: email, extra: configuration.botIdentities))
                records.append(commit)
                metadata = []
            }
            return
        }
        if var file = pendingFile {
            if awaitingPreviousPath {
                file.previousPath = raw
                pendingFile = file
                awaitingPreviousPath = false
            } else {
                file.path = raw
                pendingFile = nil
                try append(file, checkpoint: checkpoint)
            }
            return
        }
        let token = String(raw.drop(while: { $0 == "\n" }))
        if token.isEmpty { return }
        if !token.contains("\t") {
            guard GitHistory.validOID(token) else {
                throw BigboardError.message("Git history contains an invalid commit identifier.")
            }
            metadata = [token]
            return
        }
        let pieces = token.split(separator: "\t", maxSplits: 2, omittingEmptySubsequences: false)
            .map(String.init)
        guard pieces.count == 3, skipping || !records.isEmpty else {
            throw BigboardError.message("Git history contains an invalid file-change record.")
        }
        var file = FileChange(path: pieces[2])
        if pieces[0] == "-" || pieces[1] == "-" {
            file.binary = true
        } else {
            guard let added = Int(pieces[0]), let removed = Int(pieces[1]), added >= 0, removed >= 0 else {
                throw BigboardError.message("Git history contains invalid line counts.")
            }
            file.added = added
            file.removed = removed
        }
        if file.path.isEmpty {
            pendingFile = file
            awaitingPreviousPath = true
        } else {
            try append(file, checkpoint: checkpoint)
        }
    }
    private func append(_ original: FileChange, checkpoint: @Sendable () throws -> Void) throws {
        if skipping { return }
        guard !records.isEmpty, !original.path.isEmpty else {
            throw BigboardError.message("Git history contains an empty path.")
        }
        var file = original
        var attribute = AttributeDecision.unspecified
        if !configuration.allFiles, !attributes.isEmpty {
            let bytes = file.path.utf8.count
            guard bytes <= 64 * 1024 else {
                throw BigboardError.message("Repository attribute matching exceeded the safety limit.")
            }
            let key = Data(file.path.utf8)
            if let cached = attributeDecisions[key] {
                attribute = cached
            } else {
                attribute = try HistoryRules.attributeDecision(
                    file.path, attributes: attributes, remainingWork: &remainingAttributeWork,
                    checkpoint: checkpoint)
                if attributeDecisions.count < 16_384, bytes <= 4 * 1024 * 1024 - cachedPathBytes {
                    attributeDecisions[key] = attribute
                    cachedPathBytes += bytes
                }
            }
        }
        file.excludedReason = HistoryRules.exclusion(
            file, allFiles: configuration.allFiles, attribute: attribute)
        let index = records.count - 1
        if file.excludedReason.isEmpty {
            records[index].added += file.added
            records[index].removed += file.removed
        }
        records[index].files.append(file)
    }
}
