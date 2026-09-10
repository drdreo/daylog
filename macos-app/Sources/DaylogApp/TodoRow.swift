import SwiftUI
import DaylogCore

/// Todos are tasks, not journal entries: checkbox first, task text second.
struct TodoRow: View {
    let entry: Entry
    let needsTriage: Bool
    let busy: Bool
    let onAction: (String) async -> Bool
    @State private var submitting = false
    @State private var hovering = false
    @State private var completed = false
    @State private var accepted = false
    @State private var declined = false

    private var isCompleted: Bool { completed || entry.done == true }
    private var isProposal: Bool { needsTriage && !accepted && !isCompleted }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Group {
                if submitting {
                    ProgressView().controlSize(.small)
                } else if isCompleted {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 16, weight: .medium))
                        .symbolRenderingMode(.palette)
                        .foregroundStyle(.white, .green)
                        .accessibilityLabel("Completed")
                } else if isProposal {
                    Image(systemName: declined ? "xmark.circle" : "circle.dashed")
                        .foregroundStyle(.orange)
                        .help("Accept this proposal before completing it")
                } else {
                    Button { perform("done") } label: {
                        Image(systemName: "circle")
                            .font(.system(size: 16, weight: .regular))
                            .foregroundStyle(hovering && !busy ? Color.accentColor : Color.secondary)
                            .frame(width: 24, height: 24)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Complete \(entry.tldr)")
                    .help("Mark done")
                    .disabled(busy)
                }
            }
            .frame(width: 20, height: 20)

            VStack(alignment: .leading, spacing: 6) {
                Text(entry.tldr)
                    .font(.system(size: 14))
                    .foregroundStyle(isCompleted || declined ? .secondary : .primary)
                    .strikethrough(isCompleted || declined)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)

                if isProposal {
                    HStack(spacing: 6) {
                        Text(declined ? "Declined" : "Suggested by \(entry.source.split(separator: ":").last.map(String.init) ?? entry.source)")
                            .font(.caption).foregroundStyle(.secondary)
                        Spacer(minLength: 0)
                        if !declined {
                            Button("Decline") { perform("decline") }
                            Button("Accept") { perform("accept") }
                                .buttonStyle(.borderedProminent)
                        }
                    }
                    .controlSize(.small)
                    .disabled(busy || submitting)
                } else if isCompleted {
                    Text(completionDetails)
                        .font(.caption2).foregroundStyle(.secondary)
                        .lineLimit(1).truncationMode(.tail)
                        .help(completionDetails)
                } else if let filed = olderFilingDay {
                    Text("Added \(filed)").font(.caption).foregroundStyle(.secondary)
                }

                if let url = entry.referenceURL {
                    Link(destination: url) {
                        Label("Referenced PR", systemImage: "arrow.up.right.square")
                    }
                    .font(.caption).buttonStyle(.plain).foregroundStyle(Color.accentColor)
                }
            }
        }
        .padding(.horizontal, 14).padding(.vertical, 12)
        .background(hovering ? Color.primary.opacity(0.035) : Color.clear)
        .onHover { hovering = $0 }
    }

    private var completionDetails: String {
        let done = entry.done == true ? "Done \(entry.clock)" : "Completed"
        return done + (entry.filedLabel.map { " · Filed \($0)" } ?? "")
    }

    private var olderFilingDay: String? {
        let day = String((entry.filed_at ?? entry.display_at).prefix(10))
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.dateFormat = "yyyy-MM-dd"
        guard let date = formatter.date(from: day), !Calendar.current.isDateInToday(date) else { return nil }
        return date.formatted(.dateTime.month(.abbreviated).day().year())
    }

    private func perform(_ command: String) {
        guard !busy, !submitting, !isCompleted, !declined else { return }
        submitting = true
        Task { @MainActor in
            if await onAction(command) {
                // Keep a successful write reflected even if the follow-up read failed.
                completed = command == "done"
                accepted = accepted || command == "accept"
                declined = command == "decline"
            }
            submitting = false
        }
    }
}
