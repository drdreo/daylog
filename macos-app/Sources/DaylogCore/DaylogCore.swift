import Foundation
import Darwin

public struct JournalDay: Decodable, Sendable {
    public let version: Int
    public let date: String
    public let entries: [Entry]
    public let open_todos: [Entry]
    public let needs_triage: [Entry]
    public let prs: [PullRequest]
    public let prs_fetched_at: String?

    // The CLI already scopes entries (including completions) to the selected day.
    public var completedTodos: [Entry] { entries.filter { $0.type == "todo" && $0.done == true }.reversed() }
    public var journalEntries: [Entry] { entries.filter { !($0.type == "todo" && $0.done == true) } }

    public static func decode(_ data: Data) throws -> JournalDay {
        let day = try JSONDecoder().decode(Self.self, from: data)
        guard day.version == 2 else {
            throw ClientError("Unsupported view version \(day.version). Update Daylog and the app together.")
        }
        return day
    }
}

public struct Entry: Decodable, Identifiable, Sendable {
    public let id: String
    public let type: String
    public let tldr: String
    public let details: String?
    public let tags: [String]?
    public let source: String
    public let display_at: String
    public let filed_at: String?
    public let done: Bool?
    public let refs: [String]

    // Preserve the captured offset rather than silently converting to this Mac's zone.
    public var clock: String { String(display_at.dropFirst(11).prefix(5)) }
    public var filedLabel: String? {
        guard let filed = filed_at else { return nil }
        // Format captured calendar components, not an instant converted to this Mac's zone.
        // Fractional seconds and the offset do not affect the displayed filing minute.
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss"
        guard let date = formatter.date(from: String(filed.prefix(19))) else { return nil }
        formatter.dateFormat = "d.M.yyyy HH:mm"
        return formatter.string(from: date)
    }
    public var references: [JournalReference] {
        var seen = Set<String>()
        return refs.filter { seen.insert($0).inserted }.compactMap(JournalReference.init)
    }

    public var referenceURL: URL? {
        for ref in refs {
            guard ref.range(of: #"^gh:pr:[a-zA-Z0-9.-]+/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+#[1-9][0-9]*$"#,
                            options: .regularExpression) != nil else { continue }
            return URL(string: "https://" + ref.dropFirst(6).replacingOccurrences(of: "#", with: "/pull/"))
        }
        return nil
    }
}

/// Athena selects supplied references; Swift owns safe destinations and rendering.
public struct JournalReference: Identifiable, Sendable {
    public let id: String
    public let label: String
    public let url: URL?

    public init?(_ ref: String) {
        id = ref
        if ref.range(of: #"^gh:pr:[a-zA-Z0-9.-]+/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+#[1-9][0-9]*$"#,
                     options: .regularExpression) != nil {
            label = String(ref.split(separator: "/").last!)
            url = URL(string: "https://" + ref.dropFirst(6).replacingOccurrences(of: "#", with: "/pull/"))
        } else if ref.range(of: #"^linear:[A-Z][A-Z0-9]*-[1-9][0-9]*$"#, options: .regularExpression) != nil {
            label = String(ref.dropFirst(7))
            url = URL(string: "https://linear.app/issue/" + label)
        } else if ref.range(of: #"^jira:[A-Z][A-Z0-9]*-[1-9][0-9]*$"#, options: .regularExpression) != nil {
            label = String(ref.dropFirst(5))
            url = nil // A Jira issue ID does not tell us the organization's host.
        } else {
            return nil
        }
    }
}

public struct PullRequest: Decodable, Identifiable, Sendable {
    public let repo: String
    public let number: Int
    public let title: String
    public let url: String
    public let checks: String
    public let review: String
    public let draft: Bool
    public var id: String { url }
    public var safeURL: URL? {
        guard let value = URL(string: url), value.scheme == "https", value.host != nil else { return nil }
        return value
    }
    // Snapshot readiness only; GitHub may still enforce conflicts or other branch rules.
    public var isMergeReady: Bool { !draft && review == "approved" && checks == "passing" }
    public var checksBadge: PRStatusBadge { .checks(checks) }
    public var reviewBadge: PRStatusBadge { .review(review) }
}

/// Keep CI and review independent: an approved PR can still have failing checks.
public struct PRStatusBadge: Equatable, Sendable {
    public enum Tone: Sendable { case positive, negative, pending, neutral }
    public let label: String
    public let symbol: String
    public let tone: Tone

    public static func checks(_ value: String) -> Self {
        switch value {
        case "passing": return Self(label: "Checks passed", symbol: "checkmark.circle.fill", tone: .positive)
        case "failing": return Self(label: "Checks failed", symbol: "xmark.circle.fill", tone: .negative)
        case "pending": return Self(label: "Checks pending", symbol: "clock", tone: .pending)
        case "none": return Self(label: "No checks", symbol: "minus.circle", tone: .neutral)
        default: return Self(label: "Checks unknown", symbol: "questionmark.circle", tone: .neutral)
        }
    }

    public static func review(_ value: String) -> Self {
        switch value {
        case "approved": return Self(label: "Approved", symbol: "checkmark.seal.fill", tone: .positive)
        case "changes_requested": return Self(label: "Changes requested", symbol: "xmark.bubble.fill", tone: .negative)
        case "review_required": return Self(label: "Awaiting review", symbol: "clock", tone: .pending)
        case "none": return Self(label: "No review", symbol: "minus.bubble", tone: .neutral)
        default: return Self(label: "Review unknown", symbol: "questionmark.bubble", tone: .neutral)
        }
    }
}

public struct ClientError: LocalizedError, Sendable {
    public let message: String
    public init(_ message: String) { self.message = message }
    public var errorDescription: String? { message }
}

/// Thin CLI boundary: no shell, no direct store access, and no worker/model calls.
public struct DaylogClient: Sendable {
    public let binary: String
    public let dataDirectory: String
    public init(binary: String, dataDirectory: String) {
        self.binary = binary
        self.dataDirectory = dataDirectory
    }

    public func run(_ arguments: [String], timeout: TimeInterval = 60) async throws -> Data {
        try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                continuation.resume(with: Result { try runSynchronously(arguments, timeout: timeout) })
            }
        }
    }

    // Keep Foundation Process lifecycle on one worker thread, never the UI executor.
    private func runSynchronously(_ arguments: [String], timeout: TimeInterval) throws -> Data {
            let fm = FileManager.default
            let executable = (binary as NSString).expandingTildeInPath
            guard executable.hasPrefix("/"), fm.isExecutableFile(atPath: executable) else {
                throw ClientError("Daylog CLI not found at \(executable). Set its absolute path in Connection.")
            }
            let directory = (dataDirectory as NSString).expandingTildeInPath
            guard directory.isEmpty || directory.hasPrefix("/") else {
                throw ClientError("Data directory must be an absolute path (or empty for the CLI default).")
            }
            // Private temporary files avoid pipe-buffer deadlocks for large journals/errors.
            let temp = fm.temporaryDirectory.appendingPathComponent("daylog-app-\(UUID().uuidString)")
            try fm.createDirectory(at: temp, withIntermediateDirectories: false,
                                   attributes: [.posixPermissions: 0o700])
            defer { try? fm.removeItem(at: temp) }
            let out = temp.appendingPathComponent("stdout")
            let err = temp.appendingPathComponent("stderr")
            fm.createFile(atPath: out.path, contents: nil, attributes: [.posixPermissions: 0o600])
            fm.createFile(atPath: err.path, contents: nil, attributes: [.posixPermissions: 0o600])
            let output = try FileHandle(forWritingTo: out)
            let errors = try FileHandle(forWritingTo: err)
            defer { try? output.close(); try? errors.close() }
            let process = Process()
            process.executableURL = URL(fileURLWithPath: executable)
            process.arguments = (directory.isEmpty ? [] : ["--data-dir", directory]) + arguments
            process.currentDirectoryURL = fm.homeDirectoryForCurrentUser
            var env = ProcessInfo.processInfo.environment.filter {
                !$0.key.hasPrefix("DAYLOG_") && !$0.key.hasPrefix("PI_") && !$0.key.hasPrefix("HERDR_")
            }
            env["DAYLOG_SOURCE"] = "human:widget"
            env["PATH"] = "\(fm.homeDirectoryForCurrentUser.path)/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
            process.environment = env
            process.standardInput = FileHandle.nullDevice
            process.standardOutput = output
            process.standardError = errors
            try process.run()
            let deadline = Date().addingTimeInterval(timeout)
            while process.isRunning && Date() < deadline {
                Thread.sleep(forTimeInterval: 0.05)
            }
            if process.isRunning {
                process.terminate()
                Thread.sleep(forTimeInterval: 0.1)
                if process.isRunning { kill(process.processIdentifier, SIGKILL) }
                process.waitUntilExit()
                throw ClientError("Daylog timed out. Refresh to check state before retrying an action.")
            }
            process.waitUntilExit()
            guard process.terminationStatus == 0 else {
                let message = String(decoding: try Data(contentsOf: err), as: UTF8.self)
                throw ClientError(message.isEmpty ? "Daylog exited with status \(process.terminationStatus)." : message)
            }
            return try Data(contentsOf: out)
    }
}
