import Foundation
import Darwin

struct HistoryCacheLimits: Sendable {
    var cacheBytes = 16 * 1024 * 1024
}

struct HistoryScanCache: Codable {
    var schema: Int
    var fingerprint: String
    var scannedAt: Date
    var records: [Commit]
    static func read(
        _ url: URL, limits: HistoryCacheLimits = HistoryCacheLimits(), checkpoint: () throws -> Void = {}
    ) throws -> HistoryScanCache {
        try checkpoint()
        let descriptor = open(url.path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK)
        guard descriptor >= 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
        let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
        defer { try? handle.close() }
        var info = stat()
        let maximum = max(0, limits.cacheBytes)
        guard fstat(descriptor, &info) == 0, info.st_mode & S_IFMT == S_IFREG, info.st_size >= 0,
            info.st_size <= Int64(maximum)
        else { throw BigboardError.message("Git history cache exceeded the file safety limit.") }
        var data = Data()
        while true {
            try checkpoint()
            let count = min(64 * 1024, maximum - data.count) + 1
            guard let chunk = try handle.read(upToCount: count), !chunk.isEmpty else { break }
            guard chunk.count <= maximum - data.count else {
                throw BigboardError.message("Git history cache exceeded the file safety limit.")
            }
            data.append(chunk)
        }
        try checkpoint()
        let cache = try JSONDecoder().decode(HistoryScanCache.self, from: data)
        try checkpoint()
        return cache
    }
    func encoded(limits: HistoryCacheLimits = HistoryCacheLimits(), checkpoint: () throws -> Void = {}) throws
        -> Data?
    {
        try checkpoint()
        var remaining = max(0, limits.cacheBytes)
        func reserve(_ count: Int) -> Bool {
            guard count <= remaining else { return false }
            remaining -= count
            return true
        }
        func reserveString(_ value: String) -> Bool {
            let bytes = value.utf8.count
            guard bytes <= remaining / 6 else { return false }
            return reserve(bytes * 6)
        }
        guard reserve(256), reserveString(fingerprint) else { return nil }
        for record in records {
            try checkpoint()
            guard reserve(512) else { return nil }
            for value in [record.id, record.repo, record.author, record.email, record.subject] {
                guard reserveString(value) else { return nil }
            }
            for file in record.files {
                try checkpoint()
                guard reserve(192) else { return nil }
                for value in [file.path, file.previousPath, file.excludedReason] {
                    guard reserveString(value) else { return nil }
                }
            }
        }
        try checkpoint()
        let data = try JSONEncoder().encode(self)
        try checkpoint()
        guard data.count <= limits.cacheBytes else { return nil }
        return data
    }
    @discardableResult func write(
        to url: URL, limits: HistoryCacheLimits = HistoryCacheLimits(), checkpoint: () throws -> Void = {}
    ) throws -> Bool {
        guard let data = try encoded(limits: limits, checkpoint: checkpoint) else { return false }
        try FileManager.default.createDirectory(
            at: url.deletingLastPathComponent(), withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        try checkpoint()
        try data.write(to: url, options: .atomic)
        return true
    }
}
