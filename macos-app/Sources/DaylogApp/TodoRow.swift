import SwiftUI
import DaylogCore

/// The main row is one accessible button; proposal controls and links stay independent.
struct TodoRow: View {
    let entry: Entry
    let needsTriage: Bool
    let busy: Bool
    let onAction: (String) async -> Bool
    @State private var submitting = false
    @State private var hovering = false
    @State private var completionOverride: Bool?
    @State private var accepted = false
    @State private var declined = false

    private var isCompleted: Bool { completionOverride ?? (entry.done == true) }
    private var isProposal: Bool { needsTriage && !accepted && !isCompleted }
    private var toggleCommand: String { isCompleted ? "reopen" : "done" }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if isProposal {
                rowContent
                if !declined {
                    HStack(spacing: 6) {
                        Spacer()
                        Button("Decline") { perform("decline") }
                        Button("Accept") { perform("accept") }
                            .buttonStyle(.borderedProminent)
                    }
                    .controlSize(.small)
                    .disabled(busy || submitting)
                    .padding(.horizontal, 14).padding(.bottom, 12)
                }
            } else {
                Button { perform(toggleCommand) } label: { rowContent }
                    .buttonStyle(.plain)
                    .disabled(busy || submitting || declined)
                    .accessibilityLabel("\(isCompleted ? "Reopen" : "Complete") \(entry.tldr)")
                    .help(isCompleted ? "Mark as not done" : "Mark done")
            }
            if let url = entry.referenceURL {
                Link(destination: url) {
                    Label("Referenced PR", systemImage: "arrow.up.right.square")
                }
                .font(.caption).buttonStyle(.plain).foregroundStyle(Color.accentColor)
                .padding(.leading, 44).padding(.bottom, 12)
            }
        }
        .background(hovering && !busy ? Color.primary.opacity(0.035) : Color.clear)
        .onHover { hovering = $0 }
        .onChange(of: entry.done) { _ in completionOverride = nil }
    }

    private var rowContent: some View {
        HStack(alignment: .top, spacing: 10) {
            Group {
                if submitting {
                    ProgressView().controlSize(.small)
                } else if isCompleted {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 16, weight: .medium))
                        .symbolRenderingMode(.palette)
                        .foregroundStyle(.white, .green)
                } else if isProposal {
                    Image(systemName: declined ? "xmark.circle" : "circle.dashed")
                        .foregroundStyle(.orange)
                        .help("Accept this proposal before completing it")
                } else {
                    Image(systemName: "circle")
                        .font(.system(size: 16, weight: .regular))
                        .foregroundStyle(hovering && !busy ? Color.accentColor : Color.secondary)
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
                    Text(declined ? "Declined" : "Suggested by \(entry.source.split(separator: ":").last.map(String.init) ?? entry.source)")
                        .font(.caption).foregroundStyle(.secondary)
                } else if isCompleted {
                    Text(completionDetails)
                        .font(.caption2).foregroundStyle(.secondary)
                        .lineLimit(1).truncationMode(.tail)
                        .help(completionDetails)
                } else if let filed = olderFilingDay {
                    Text("Added \(filed)").font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .padding(.horizontal, 14).padding(.vertical, 12)
        .contentShape(Rectangle())
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
        guard !busy, !submitting, !declined else { return }
        if isProposal {
            guard command == "accept" || command == "decline" else { return }
        } else {
            guard command == toggleCommand else { return }
        }
        submitting = true
        Task { @MainActor in
            if await onAction(command) {
                // Reflect a saved write even if the follow-up read failed, in either direction.
                if command == "done" || command == "reopen" { completionOverride = command == "done" }
                accepted = accepted || command == "accept"
                declined = command == "decline"
            }
            submitting = false
        }
    }
}
