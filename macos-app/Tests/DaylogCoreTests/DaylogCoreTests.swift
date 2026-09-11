import Foundation
import DaylogCore

private func XCTAssertEqual<T: Equatable>(_ lhs: T, _ rhs: T) { precondition(lhs == rhs, "\(lhs) != \(rhs)") }
private func XCTAssertTrue(_ value: Bool) { precondition(value) }
private func XCTFail(_ message: String) { fatalError(message) }
private func XCTAssertThrowsError<T>(_ expression: @autoclosure () throws -> T) {
    do { _ = try expression(); fatalError("Expected an error") } catch { }
}

@main
struct DaylogCoreChecks {
    static func main() async throws {
        let tests = Self()
        try tests.testViewContractAndCapturedClock()
        tests.testRejectsUnsupportedView()
        tests.testMissingDisplayTimeIsAnError()
        try await tests.testArgumentBoundariesAndHumanIdentity()
        try await tests.testErrorsAndTimeout()
        try tests.testPRBadgesAndSafeLinks()
        try tests.testTodoGrouping()
        tests.testCalendarMonthGrid()
        try tests.testCalendarIndex()
        try tests.testMergeReadyPRs()
        try tests.testJournalPresentationAndReferences()
        try tests.testPRStacks()
        print("Passed 12 Daylog core checks")
        if CommandLine.arguments.count == 2 {
            try await tests.testRealCLI(CommandLine.arguments[1])
            print("Passed real CLI scratch-store note/todo/completion checks")
            let current = try JournalDay.decode(await DaylogClient(binary: CommandLine.arguments[1], dataDirectory: "").run(["today", "--json"]))
            print("Current store decoded: \(current.entries.count) entries, \(current.open_todos.count) todos, \(current.prs.count) PRs (read-only)")
        }
    }

    func testPRStacks() throws {
        func pr(_ number: Int, _ base: String? = nil, _ head: String? = nil,
                repo: String = "o/r", headRepo: String = "o/r", host: String = "github.com") throws -> PullRequest {
            var value: [String: Any] = ["repo": repo, "number": number, "title": "PR",
                "url": "https://\(host)/\(repo)/pull/\(number)", "draft": false,
                "checks": "passing", "review": "approved"]
            value["base_branch"] = base
            value["head_branch"] = head
            if head != nil { value["head_repo"] = headRepo }
            return try JSONDecoder().decode(PullRequest.self, from: JSONSerialization.data(withJSONObject: value))
        }
        let root = try pr(1, "main", "a")
        let child = try pr(2, "a", "b")
        let tip = try pr(3, "b", "c")
        let standalone = try pr(4)
        let groups = PullRequestGroup.group([tip, standalone, child, root])
        XCTAssertEqual(groups.map { $0.rows.map { $0.pr.number } }, [[1, 2, 3], [4]])
        XCTAssertEqual(groups[0].rows.map(\.parentNumber), [nil, 1, 2])
        XCTAssertTrue(groups[0].isStack)
        XCTAssertTrue(!groups[1].isStack)
        XCTAssertTrue(PullRequestGroup.group([]).isEmpty)
        let sibling = try pr(5, "a", "d")
        XCTAssertEqual(PullRequestGroup.group([root, child, sibling])[0].rows.map(\.parentNumber), [nil, 1, 1])
        // Same branch name is insufficient across repos, hosts, forks, or ambiguous heads.
        for other in [try pr(6, "a", "z", repo: "other/r"),
                      try pr(6, "a", "z", host: "github.example.com")] {
            XCTAssertEqual(PullRequestGroup.group([root, other]).count, 2)
        }
        XCTAssertEqual(PullRequestGroup.group([try pr(1, "main", "a", headRepo: "fork/r"), child]).count, 2)
        XCTAssertEqual(PullRequestGroup.group([root, try pr(6, "main", "a"), child]).count, 3)
        XCTAssertEqual(PullRequestGroup.group([try pr(1, "b", "a"), child]).count, 2)
        XCTAssertEqual(PullRequestGroup.group([standalone, try pr(7)]).count, 2)
    }

    func testJournalPresentationAndReferences() throws {
        let old = #"{"id":"old","type":"work","tldr":"An older entry keeps its complete wording for expansion.","source":"agent:pi","display_at":"2026-09-09T12:00:00Z","refs":[]}"#
        let legacy = try JSONDecoder().decode(Entry.self, from: Data(old.utf8))
        XCTAssertEqual(legacy.details, nil)
        XCTAssertEqual(legacy.tags, nil)
        XCTAssertEqual(legacy.tldr, "An older entry keeps its complete wording for expansion.")

        let json = #"{"id":"new","type":"work","tldr":"Narrowed the refresh race","details":"The attempted mutex fix still fails.\nCache invalidation needs investigation.","tags":["incomplete","imagegen"],"source":"agent:pi","display_at":"2026-09-09T12:00:00Z","refs":["gh:pr:github.com/owner/repo#42","linear:SCA-3825","jira:PROJ-1","gh:pr:github.com/owner/repo#42","file:///tmp/unsafe","javascript:alert(1)"]}"#
        let entry = try JSONDecoder().decode(Entry.self, from: Data(json.utf8))
        XCTAssertTrue(entry.details!.contains("still fails"))
        XCTAssertEqual(entry.tags, ["incomplete", "imagegen"])
        XCTAssertEqual(entry.references.map(\.label), ["repo#42", "SCA-3825", "PROJ-1"])
        XCTAssertEqual(entry.references[0].url?.absoluteString, "https://github.com/owner/repo/pull/42")
        XCTAssertEqual(entry.references[1].url?.absoluteString, "https://linear.app/issue/SCA-3825")
        XCTAssertEqual(entry.references[2].url, nil)
        XCTAssertEqual(JournalReference("gh:pr:github.com@evil.example/o/r#1")?.url, nil)
        XCTAssertEqual(JournalReference("linear:../escape")?.url, nil)
    }

    func testCalendarMonthGrid() {
        let zone = TimeZone(identifier: "Europe/Berlin")!
        let parser = ISO8601DateFormatter()
        func month(_ value: String) -> JournalCalendarMonth {
            JournalCalendarMonth(containing: parser.date(from: value + "T12:00:00Z")!, timeZone: zone)
        }
        let september = month("2026-09-06")
        XCTAssertEqual(september.key, "2026-09")
        XCTAssertEqual(september.cells.count, 35)
        XCTAssertEqual(september.cells.first!, nil) // Monday-first, starts Tuesday.
        XCTAssertEqual(september.cells.compactMap { $0 }.count, 30)
        XCTAssertEqual(september.dayKey(september.cells[1]!), "2026-09-01")
        XCTAssertEqual(month("2024-02-10").cells.compactMap { $0 }.count, 29)
        XCTAssertEqual(month("2021-02-10").cells.count, 28) // Exactly four weeks.
        XCTAssertEqual(month("2026-03-10").cells.count, 42) // Six weeks, DST transition.
        XCTAssertEqual(month("2026-12-31").moved(by: 1).key, "2027-01")
        XCTAssertEqual(month("2026-01-31").moved(by: -1).key, "2025-12")
        for value in ["2026-03-10", "2026-10-10"] {
            let grid = month(value)
            for (index, date) in grid.cells.compactMap({ $0 }).enumerated() {
                XCTAssertEqual(grid.calendar.component(.day, from: date), index + 1)
                XCTAssertEqual(grid.calendar.component(.hour, from: date), 0)
            }
        }
    }

    func testCalendarIndex() throws {
        let json = #"{"version":2,"month":"2026-09","days":[{"date":"2026-09-06","count":3}]}"#
        let summary = try JournalMonthSummary.decode(Data(json.utf8), expectedMonth: "2026-09")
        XCTAssertEqual(summary.days.first?.count, 3)
        for invalid in [
            json.replacingOccurrences(of: "\"version\":2", with: "\"version\":1"),
            json.replacingOccurrences(of: "\"count\":3", with: "\"count\":0"),
            json.replacingOccurrences(of: "2026-09-06", with: "2026-09-31"),
            json.replacingOccurrences(of: "2026-09-06", with: "2026-08-06"),
            #"{"version":2,"month":"2026-09","days":[{"date":"2026-09-06","count":1},{"date":"2026-09-06","count":2}]}"#
        ] {
            XCTAssertThrowsError(try JournalMonthSummary.decode(Data(invalid.utf8), expectedMonth: "2026-09"))
        }
        XCTAssertThrowsError(try JournalMonthSummary.decode(Data(json.utf8), expectedMonth: "2026-08"))
        let empty = try JournalMonthSummary.decode(Data(#"{"version":2,"month":"2026-09","days":[]}"#.utf8), expectedMonth: "2026-09")
        XCTAssertTrue(empty.days.isEmpty)
    }

    func testTodoGrouping() throws {
        func entry(_ id: String, type: String, done: Bool) -> [String: Any] {
            ["id": id, "type": type, "done": done, "tldr": id, "source": "human:widget",
             "display_at": "2026-09-06T20:56:00+02:00", "refs": [String]()]
        }
        let payload: [String: Any] = [
            "version": 2, "date": "2026-09-06",
            "entries": [entry("older", type: "todo", done: true),
                        entry("work", type: "work", done: false),
                        entry("recent", type: "todo", done: true),
                        entry("note", type: "note", done: false)],
            "open_todos": [entry("open", type: "todo", done: false)],
            "needs_triage": [], "prs": []
        ]
        let day = try JournalDay.decode(JSONSerialization.data(withJSONObject: payload))
        XCTAssertEqual(day.completedTodos.map(\.id), ["recent", "older"])
        XCTAssertEqual(day.journalEntries.map(\.id), ["work", "note"])
        XCTAssertEqual(day.open_todos.map(\.id), ["open"])
        XCTAssertEqual(day.completedTodos.count + day.journalEntries.count, day.entries.count)
    }

    func testMergeReadyPRs() throws {
        for draft in [false, true] {
            for review in ["approved", "changes_requested", "review_required", "none", "unknown"] {
                for checks in ["passing", "failing", "pending", "none", "unknown"] {
                    let json = """
                    {"repo":"owner/repo","number":42,"title":"Readiness","url":"https://github.com/owner/repo/pull/42",
                     "draft":\(draft),"review":"\(review)","checks":"\(checks)"}
                    """
                    let pr = try JSONDecoder().decode(PullRequest.self, from: Data(json.utf8))
                    XCTAssertEqual(pr.isMergeReady, !draft && review == "approved" && checks == "passing")
                }
            }
        }
    }

    func testPRBadgesAndSafeLinks() throws {
        for (state, symbol, tone) in [
            ("passing", "checkmark.circle.fill", PRStatusBadge.Tone.positive),
            ("failing", "xmark.circle.fill", .negative),
            ("pending", "clock", .pending),
            ("none", "minus.circle", .neutral),
            ("new-status", "questionmark.circle", .neutral)
        ] {
            XCTAssertEqual(PRStatusBadge.checks(state).symbol, symbol)
            XCTAssertEqual(PRStatusBadge.checks(state).tone, tone)
        }
        for (state, symbol, tone) in [
            ("approved", "checkmark.seal.fill", PRStatusBadge.Tone.positive),
            ("changes_requested", "xmark.bubble.fill", .negative),
            ("review_required", "clock", .pending),
            ("none", "minus.bubble", .neutral),
            ("new-status", "questionmark.bubble", .neutral)
        ] {
            XCTAssertEqual(PRStatusBadge.review(state).symbol, symbol)
            XCTAssertEqual(PRStatusBadge.review(state).tone, tone)
        }
        let json = #"{"repo":"owner/repo","number":94448,"title":"An approved PR with failing CI","url":"https://github.com/owner/repo/pull/94448","checks":"failing","review":"approved","draft":true}"#
        let pr = try JSONDecoder().decode(PullRequest.self, from: Data(json.utf8))
        XCTAssertEqual(pr.checksBadge.label, "Checks failed")
        XCTAssertEqual(pr.reviewBadge.label, "Approved")
        XCTAssertTrue(pr.draft)
        XCTAssertEqual(pr.safeURL?.absoluteString, "https://github.com/owner/repo/pull/94448")
        let unsafe = json.replacingOccurrences(of: "https://github.com/owner/repo/pull/94448", with: "file:///tmp/test")
        XCTAssertEqual(try JSONDecoder().decode(PullRequest.self, from: Data(unsafe.utf8)).safeURL, nil)
    }

    func testRealCLI(_ binary: String) async throws {
        let root = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: root) }
        let client = DaylogClient(binary: binary, dataDirectory: root.appendingPathComponent("store").path)
        _ = try await client.run(["init"])
        let text = "-- A native note with 'quotes' | $(literal text)"
        _ = try await client.run(["add", "--type", "note", "--source", "human:widget", "--", text])
        _ = try await client.run(["add", "--type", "todo", "--source", "human:widget", "Test the native UI"])
        var day = try JournalDay.decode(await client.run(["today", "--json"]))
        XCTAssertEqual(day.entries.first?.tldr, text)
        XCTAssertEqual(day.entries.first?.source, "human:widget")
        XCTAssertEqual(day.open_todos.count, 1)
        _ = try await client.run(["done", day.open_todos[0].id, "--source", "human:widget"])
        day = try JournalDay.decode(await client.run(["today", "--json"]))
        XCTAssertTrue(day.open_todos.isEmpty)
        XCTAssertTrue(day.entries.contains { $0.done == true && $0.filed_at != nil })
        let monthKey = String(day.date.prefix(7))
        let summary = try JournalMonthSummary.decode(await client.run(["days", monthKey, "--json"]), expectedMonth: monthKey)
        XCTAssertEqual(summary.days.first?.date, day.date)
        XCTAssertEqual(summary.days.first?.count, day.entries.count)
    }

    func testViewContractAndCapturedClock() throws {
        let day = try JournalDay.decode(Data(#"""
        {"version":2,"date":"2026-09-06","entries":[
          {"id":"a","type":"todo","tldr":"Finished","source":"human:cli",
           "display_at":"2026-09-06T09:12:00-07:00","filed_at":"2026-09-05T08:00:00-07:00",
           "done":true,"refs":["gh:pr:github.com/owner/repo#42"]}],
         "open_todos":[],"needs_triage":[],"prs":[]}
        """#.utf8))
        XCTAssertEqual(day.entries[0].clock, "09:12")
        XCTAssertEqual(day.entries[0].referenceURL?.absoluteString, "https://github.com/owner/repo/pull/42")
        XCTAssertEqual(day.entries[0].filed_at, "2026-09-05T08:00:00-07:00")
        XCTAssertEqual(day.entries[0].filedLabel, "5.9.2026 08:00")
        for timestamp in ["2026-09-06T20:56:31.816251+02:00", "2026-09-06T20:56:31Z"] {
            let json = #"{"id":"a","type":"todo","tldr":"Done","source":"human:widget","display_at":"2026-09-06T21:00:00Z","filed_at":"TIMESTAMP","refs":[]}"#
                .replacingOccurrences(of: "TIMESTAMP", with: timestamp)
            let entry = try JSONDecoder().decode(Entry.self, from: Data(json.utf8))
            XCTAssertEqual(entry.filedLabel, "6.9.2026 20:56")
        }
        XCTAssertTrue(day.open_todos.isEmpty)
    }

    func testRejectsUnsupportedView() {
        XCTAssertThrowsError(try JournalDay.decode(Data(#"{"version":1,"date":"2026-09-06","entries":[],"open_todos":[],"needs_triage":[],"prs":[]}"#.utf8)))
    }

    func testMissingDisplayTimeIsAnError() {
        XCTAssertThrowsError(try JournalDay.decode(Data(#"{"version":2,"date":"2026-09-06","entries":[{"id":"a","type":"work","tldr":"x","source":"agent:pi","refs":[]}],"open_todos":[],"needs_triage":[],"prs":[]}"#.utf8)))
    }

    func testArgumentBoundariesAndHumanIdentity() async throws {
        let root = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: root) }
        let script = root.appendingPathComponent("fake cli")
        try "#!/bin/sh\nprintf '%s\\n' \"$DAYLOG_SOURCE\" \"$@\"\n".write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        let client = DaylogClient(binary: script.path, dataDirectory: root.appendingPathComponent("store with spaces").path)
        let text = "a quote ' and $(no shell) | test"
        let result = try await client.run(["add", "--", text])
        XCTAssertEqual(String(decoding: result, as: UTF8.self),
                       "human:widget\n--data-dir\n\(root.path)/store with spaces\nadd\n--\n\(text)\n")
    }

    func testErrorsAndTimeout() async throws {
        let root = try temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: root) }
        let script = root.appendingPathComponent("fake-cli")
        try "#!/bin/sh\necho 'store unavailable' >&2\nexit 7\n".write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: script.path)
        let client = DaylogClient(binary: script.path, dataDirectory: "")
        do { _ = try await client.run([]); XCTFail("Expected CLI error") }
        catch { XCTAssertTrue(error.localizedDescription.contains("store unavailable")) }
        try "#!/bin/sh\nexec /bin/sleep 5\n".write(to: script, atomically: true, encoding: .utf8)
        do { _ = try await client.run([], timeout: 0.1); XCTFail("Expected timeout") }
        catch { XCTAssertTrue(error.localizedDescription.contains("timed out")) }
    }

    private func temporaryDirectory() throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: false)
        return url
    }
}
