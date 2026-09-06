import SwiftUI
import DaylogCore

/// A native SwiftUI month grid, rather than AppKit's compact legacy date control.
struct JournalCalendarView: View {
    let selectedDate: Date
    let client: DaylogClient
    let busy: Bool
    let onSelect: (Date) -> Void
    @State private var month: JournalCalendarMonth
    @State private var counts: [String: Int] = [:]
    @State private var loading = true
    @State private var error: String?

    init(selectedDate: Date, client: DaylogClient, busy: Bool, onSelect: @escaping (Date) -> Void) {
        self.selectedDate = selectedDate
        self.client = client
        self.busy = busy
        self.onSelect = onSelect
        _month = State(initialValue: JournalCalendarMonth(containing: selectedDate))
    }

    private var monthTitle: String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_GB")
        formatter.calendar = month.calendar
        formatter.timeZone = month.calendar.timeZone
        formatter.dateFormat = "MMMM yyyy"
        return formatter.string(from: month.start)
    }

    var body: some View {
        VStack(spacing: 14) {
            HStack {
                Text("Journal").font(.caption).foregroundStyle(.secondary)
                if loading {
                    ProgressView().controlSize(.mini)
                        .accessibilityLabel("Loading journal days")
                }
                Spacer()
                Button("Today") { onSelect(Date()) }
                    .buttonStyle(.borderless).tint(.accentColor)
                    .disabled(busy)
            }
            HStack {
                Text(monthTitle).font(.system(size: 18, weight: .semibold))
                Spacer()
                monthButton("chevron.left", label: "Previous month", offset: -1)
                monthButton("chevron.right", label: "Next month", offset: 1)
                    .disabled(month.key >= JournalCalendarMonth(containing: Date()).key)
            }
            VStack(spacing: 6) {
                HStack(spacing: 0) {
                    ForEach(Array(["M", "T", "W", "T", "F", "S", "S"].enumerated()), id: \.offset) { _, label in
                        Text(label).font(.system(size: 11, weight: .medium))
                            .foregroundStyle(.tertiary).frame(maxWidth: .infinity)
                    }
                }.accessibilityHidden(true)
                let cells = month.cells
                LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 0), count: 7), spacing: 3) {
                    ForEach(cells.indices, id: \.self) { index in
                        if let date = cells[index] {
                            CalendarDayButton(
                                date: date,
                                number: month.calendar.component(.day, from: date),
                                selected: month.calendar.isDate(date, inSameDayAs: selectedDate),
                                today: month.calendar.isDateInToday(date),
                                count: counts[month.dayKey(date)] ?? 0
                            ) { onSelect(date) }
                            .disabled(busy || date > month.calendar.startOfDay(for: Date()))
                        } else {
                            Color.clear.frame(height: 44).accessibilityHidden(true)
                        }
                    }
                }
            }
            if let error = error {
                Divider()
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Label("Entry markers unavailable", systemImage: "exclamationmark.circle")
                        Spacer()
                        Button("Retry") { Task { await loadMonth() } }.buttonStyle(.borderless)
                    }
                    Text(error).font(.caption2).lineLimit(3).help(error)
                }
                .font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(20)
        .frame(width: 336)
        .task(id: month.key) { await loadMonth() }
    }

    private func monthButton(_ symbol: String, label: String, offset: Int) -> some View {
        Button { month = month.moved(by: offset) } label: {
            Image(systemName: symbol).font(.system(size: 11, weight: .semibold))
                .frame(width: 24, height: 24).contentShape(Circle())
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(label).help(label)
    }

    @MainActor
    private func loadMonth() async {
        let key = month.key
        counts = [:]
        error = nil
        loading = true
        do {
            try Task.checkCancellation()
            let data = try await client.run(["days", key, "--json"])
            try Task.checkCancellation()
            let summary = try JournalMonthSummary.decode(data, expectedMonth: key)
            guard month.key == key else { return }
            counts = Dictionary(uniqueKeysWithValues: summary.days.map { ($0.date, $0.count) })
            loading = false
        } catch {
            guard !Task.isCancelled, month.key == key else { return }
            self.error = error.localizedDescription
            loading = false
        }
    }
}

private struct CalendarDayButton: View {
    let date: Date
    let number: Int
    let selected: Bool
    let today: Bool
    let count: Int
    let action: () -> Void
    @Environment(\.isEnabled) private var enabled
    @State private var hovering = false

    private var description: String {
        date.formatted(.dateTime.weekday(.wide).day().month(.wide).year())
            + (today ? ", today" : "")
            + (count > 0 ? ", \(count) journal \(count == 1 ? "entry" : "entries")" : "")
    }

    var body: some View {
        Button(action: action) {
            VStack(spacing: 3) {
                Text(String(number))
                    .font(.system(size: 14, weight: selected || today ? .semibold : .regular))
                    .foregroundStyle(selected ? Color.white : today ? Color.red : Color.primary)
                    .frame(width: 32, height: 32)
                    .background {
                        Circle().fill(selected ? Color.accentColor : hovering && enabled ? Color.primary.opacity(0.07) : Color.clear)
                    }
                    .overlay {
                        if today && !selected { Circle().strokeBorder(Color.red.opacity(0.45), lineWidth: 1) }
                    }
                Circle().fill(Color.accentColor).frame(width: 4, height: 4)
                    .opacity(count > 0 ? 1 : 0)
            }
            .frame(maxWidth: .infinity).frame(height: 44)
            .contentShape(Rectangle())
            .opacity(enabled ? 1 : 0.25)
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .accessibilityLabel(description)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .help(description)
    }
}
