import AppKit
import SwiftUI
import DaylogCore

@MainActor
final class JournalModel: ObservableObject {
    @Published var day: JournalDay?
    @Published var selectedDate = Calendar.current.startOfDay(for: Date())
    @Published var followsToday = true
    @Published var busy = false
    @Published var error: String?
    @Published var updatedAt: Date?

    var client: DaylogClient {
        let defaults = UserDefaults.standard
        return DaylogClient(
            binary: defaults.string(forKey: "cliPath") ?? NSHomeDirectory() + "/.local/bin/daylog",
            dataDirectory: defaults.string(forKey: "dataDirectory") ?? "")
    }
    var dateArgument: String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter.string(from: selectedDate)
    }
    var mergeReadyPRCount: Int {
        day?.prs.filter(\.isMergeReady).count ?? 0
    }
    private func load() async throws {
        if followsToday { selectedDate = Calendar.current.startOfDay(for: Date()) }
        day = try JournalDay.decode(await client.run(["today", dateArgument, "--json"]))
        updatedAt = Date()
    }
    func refresh() async {
        guard !busy else { return }
        busy = true
        defer { busy = false }
        do { try await load(); error = nil }
        catch { self.error = error.localizedDescription }
    }
    func selectDate(_ date: Date) async {
        let calendar = Calendar.current
        let date = calendar.startOfDay(for: date)
        guard !busy, date <= calendar.startOfDay(for: Date()) else { return }
        selectedDate = date
        followsToday = calendar.isDateInToday(date)
        day = nil
        await refresh()
    }
    func move(_ offset: Int) async {
        guard let date = Calendar.current.date(byAdding: .day, value: offset, to: selectedDate) else { return }
        await selectDate(date)
    }
    func today() async {
        await selectDate(Date())
    }
    func act(_ arguments: [String]) async -> Bool {
        guard !busy else { return false }
        busy = true
        defer { busy = false }
        do { _ = try await client.run(arguments) }
        catch { self.error = error.localizedDescription; return false }
        // Mutation succeeded even if its follow-up read fails: don't invite duplicate notes.
        do { try await load(); error = nil }
        catch { self.error = "Action saved, but refresh failed: \(error.localizedDescription)" }
        return true
    }
}

@main
struct DaylogApp: App {
    @StateObject private var model = JournalModel()

    var body: some Scene {
        MenuBarExtra {
            JournalPanel(model: model)
        } label: {
            Image(systemName: model.error == nil ? "note.text" : "exclamationmark.triangle")
            if model.mergeReadyPRCount > 0 { Text("\(model.mergeReadyPRCount)") }
        }
        .menuBarExtraStyle(.window)
    }
}

struct JournalPanel: View {
    @ObservedObject var model: JournalModel
    @State private var note = ""
    @State private var noteType = "note"
    @State private var showConnection = false
    @State private var showCalendar = false
    @State private var refreshingGitHub = false
    @State private var cliPath = UserDefaults.standard.string(forKey: "cliPath") ?? NSHomeDirectory() + "/.local/bin/daylog"
    @State private var dataDirectory = UserDefaults.standard.string(forKey: "dataDirectory") ?? ""
    private let timer = Timer.publish(every: 60, on: .main, in: .common).autoconnect()

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Image(systemName: "note.text").font(.title2).foregroundStyle(.orange)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Daylog").font(.title2.bold())
                    Text("Your work, remembered.").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                if model.busy { ProgressView().controlSize(.small) }
                Button { Task { await model.refresh() } } label: { Image(systemName: "arrow.clockwise") }
                    .help("Refresh").disabled(model.busy)
                Button { showConnection.toggle() } label: { Image(systemName: "gearshape") }
                    .help("Connection settings")
            }
            .buttonStyle(.borderless)
            .padding(20)

            if showConnection {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Connection").font(.headline)
                    TextField("Absolute CLI path", text: $cliPath)
                    TextField("Data directory (empty = CLI default)", text: $dataDirectory)
                    Text("No store is created or migrated. No curation runs from this app.")
                        .font(.caption).foregroundStyle(.secondary)
                    HStack {
                        Button("Apply & refresh") {
                            UserDefaults.standard.set(cliPath, forKey: "cliPath")
                            UserDefaults.standard.set(dataDirectory, forKey: "dataDirectory")
                            showConnection = false
                            Task { model.day = nil; model.updatedAt = nil; await model.refresh() }
                        }
                        Spacer()
                        Button("Quit Daylog") { NSApplication.shared.terminate(nil) }
                    }
                }
                .textFieldStyle(.roundedBorder).padding(.horizontal, 20).padding(.bottom, 12)
                .disabled(model.busy)
            }

            if let error = model.error {
                Text(error).font(.caption).foregroundStyle(.red)
                    .textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                    .padding(12).background(.red.opacity(0.07))
            }
            Divider()
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    HStack {
                        Button { Task { await model.move(-1) } } label: { Image(systemName: "chevron.left") }
                        Button { showCalendar.toggle() } label: {
                            HStack(spacing: 8) {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(model.followsToday ? "Today" : model.selectedDate.formatted(.dateTime.weekday(.wide)))
                                        .font(.headline)
                                    Text(model.selectedDate.formatted(.dateTime.month(.wide).day().year()))
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                                Image(systemName: "calendar")
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .help("Choose a journal day")
                        .popover(isPresented: $showCalendar, arrowEdge: .bottom) {
                            calendarPicker
                        }
                        Spacer()
                        if !model.followsToday { Button("Today") { Task { await model.today() } } }
                        Button { Task { await model.move(1) } } label: { Image(systemName: "chevron.right") }
                            .disabled(Calendar.current.isDateInToday(model.selectedDate))
                    }.disabled(model.busy)

                    if let day = model.day {
                        let todos = day.open_todos + day.completedTodos
                        HStack {
                            Text("TO-DOS").font(.caption.bold()).tracking(1)
                            Spacer()
                            Text("\(day.open_todos.count) open · \(day.completedTodos.count) done")
                                .font(.caption.monospacedDigit())
                        }.foregroundStyle(.secondary)
                        if todos.isEmpty {
                            Label("All clear. Nothing to do.", systemImage: "checkmark.circle")
                                .foregroundStyle(.secondary)
                        } else {
                            VStack(spacing: 0) {
                                ForEach(todos) { entry in
                                    if entry.id == day.completedTodos.first?.id {
                                        if entry.id != todos.first?.id { Divider() }
                                        Text("Completed · \(model.followsToday ? "today" : model.selectedDate.formatted(.dateTime.day().month(.defaultDigits).year().locale(Locale(identifier: "de_DE"))))")
                                            .textCase(.uppercase)
                                            .font(.caption.weight(.semibold)).tracking(0.5)
                                            .foregroundStyle(.secondary)
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                            .padding(.horizontal, 14).padding(.top, 10).padding(.bottom, 2)
                                    } else if entry.id != todos.first?.id {
                                        Divider().padding(.leading, 44)
                                    }
                                    TodoRow(entry: entry,
                                            needsTriage: day.needs_triage.contains { $0.id == entry.id },
                                            busy: model.busy) { command in
                                        await model.act([command, entry.id, "--source", "human:widget"])
                                    }
                                }
                            }
                            .background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 12))
                            .clipShape(RoundedRectangle(cornerRadius: 12))
                        }
                        Divider()
                        sectionTitle("JOURNAL", count: day.journalEntries.count)
                        if day.journalEntries.isEmpty {
                            Text("Nothing logged for this day.").foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 12)
                        }
                        VStack(spacing: 6) {
                            ForEach(day.journalEntries.reversed()) { entry in
                                entryCard(entry)
                            }
                        }
                        Divider()
                        sectionTitle("OPEN PRS · SNAPSHOT", count: day.prs.count)
                        Button {
                            guard !model.busy, !refreshingGitHub else { return }
                            refreshingGitHub = true
                            Task {
                                _ = await model.act(["poll", "gh"])
                                refreshingGitHub = false
                            }
                        } label: {
                            Group {
                                if refreshingGitHub {
                                    HStack(spacing: 6) {
                                        ProgressView().controlSize(.mini)
                                        Text("Refreshing GitHub PRs…")
                                    }
                                } else if let fetched = day.prs_fetched_at {
                                    snapshotTimestamp(fetched)
                                } else {
                                    Label("Fetch GitHub PRs", systemImage: "arrow.clockwise")
                                }
                            }
                            .font(.caption2).foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .disabled(model.busy || refreshingGitHub)
                        .help("Click to refresh PRs from GitHub")
                        .accessibilityLabel("Refresh GitHub pull requests")
                        if day.prs.isEmpty { Text("No open PRs in the snapshot.").foregroundStyle(.secondary) }
                        VStack(spacing: 6) {
                            ForEach(day.prs) { pr in
                                PullRequestCard(pr: pr)
                            }
                        }
                    } else if model.busy {
                        Text("Loading your journal…").foregroundStyle(.secondary)
                    } else {
                        Text("Connect to your Daylog CLI using the gear button above.").foregroundStyle(.secondary)
                    }
                }.padding(20)
            }
            Divider()
            VStack(spacing: 10) {
                HStack {
                    Picker("Type", selection: $noteType) {
                        Text("Note").tag("note")
                        Text("Todo").tag("todo")
                    }.labelsHidden().frame(width: 85)
                    TextField("Add something for today…", text: $note).textFieldStyle(.roundedBorder)
                        .onSubmit { saveNote() }
                    Button { saveNote() } label: { Image(systemName: "plus") }
                        .disabled(note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }.disabled(model.busy || model.day == nil)
                HStack {
                    if let updated = model.updatedAt {
                        Text("Updated \(updated.formatted(date: .omitted, time: .shortened))").font(.caption2)
                    }
                    Spacer()
                }.font(.caption).foregroundStyle(.secondary).buttonStyle(.borderless)
            }.padding(16)
        }
        .frame(width: 480, height: 680)
        .background(.regularMaterial)
        .task { await model.refresh() }
        .onReceive(timer) { _ in Task { await model.refresh() } }
    }

    private var calendarPicker: some View {
        JournalCalendarView(selectedDate: model.selectedDate, client: model.client, busy: model.busy) { date in
            showCalendar = false
            Task { await model.selectDate(date) }
        }
    }

    private func snapshotTimestamp(_ value: String) -> some View {
        let formatter = ISO8601DateFormatter()
        let plain = formatter.date(from: value)
        formatter.formatOptions.insert(.withFractionalSeconds)
        let date = plain ?? formatter.date(from: value)
        return Group {
            if let date = date {
                HStack(spacing: 4) {
                    Image(systemName: "arrow.clockwise")
                    Text("Fetched")
                    Text(date, style: .relative)
                    Text("ago")
                    if Date().timeIntervalSince(date) > 7200 { Text("· Stale").foregroundStyle(.orange) }
                }
                .help(date.formatted(date: .complete, time: .shortened))
            } else {
                Text("Snapshot fetch time unavailable")
            }
        }.font(.caption2).foregroundStyle(.secondary)
    }

    private func sectionTitle(_ title: String, count: Int) -> some View {
        HStack {
            Text(title).font(.caption.bold()).tracking(1)
            Spacer()
            Text("\(count)").font(.caption.monospacedDigit())
        }.foregroundStyle(.secondary)
    }

    private func entryCard(_ entry: Entry) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(entry.clock).monospacedDigit()
                Text(entry.type == "todo" && entry.done == true ? "completed" : entry.type)
                Spacer()
                Text(entry.source)
            }.font(.caption).foregroundStyle(.secondary)
            HStack(alignment: .top, spacing: 8) {
                if entry.type == "todo" && entry.done == true {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                }
                Text(entry.tldr)
                    .strikethrough(entry.type == "todo" && entry.done == true)
                    .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
            }
            if entry.type == "todo", entry.done == true, let filed = entry.filedLabel {
                Text("Filed \(filed)").font(.caption2).foregroundStyle(.secondary)
            }
            if let url = entry.referenceURL {
                Link("Open referenced PR ↗", destination: url).font(.caption)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 12).padding(.vertical, 8)
        .background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 10))
    }
    private func saveNote() {
        let text = note.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !model.busy, model.day != nil else { return }
        Task {
            if await model.act(["add", "--type", noteType, "--source", "human:widget", "--", text]) { note = "" }
        }
    }
}
