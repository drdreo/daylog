import SwiftUI
import DaylogCore

/// Compact by default. The model chooses content, never layout or executable UI.
struct JournalEntryCard: View {
    let entry: Entry
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Button {
                    withAnimation(.easeInOut(duration: 0.15)) { expanded.toggle() }
                } label: {
                    Text(entry.tldr)
                        .font(.system(size: 13, weight: .regular))
                        .lineLimit(expanded ? nil : 1)
                        .truncationMode(.tail)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(entry.tldr)
                .accessibilityValue(expanded ? "Expanded" : "Collapsed")
                .accessibilityHint(expanded ? "Hide details" : "Show full entry and details")
                .help(expanded ? "Collapse entry" : entry.tldr)

                Text(entry.clock).font(.caption2.monospacedDigit()).foregroundStyle(.secondary)
            }

            if !entry.references.isEmpty || !(entry.tags ?? []).isEmpty {
                // Separate controls: opening a reference must not toggle expansion.
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 6) {
                        ForEach(entry.references) { reference in
                            if let url = reference.url {
                                Link(destination: url) {
                                    Label(reference.label, systemImage: "arrow.up.right")
                                        .font(.caption2)
                                        .padding(.horizontal, 6).padding(.vertical, 3)
                                        .background(.blue.opacity(0.08), in: Capsule())
                                }
                                .help(reference.id)
                            } else {
                                chip(reference.label).help(reference.id)
                            }
                        }
                        ForEach(entry.tags ?? [], id: \.self) { tag in chip(tag) }
                    }
                }
                .frame(height: 22)
            }

            if expanded {
                VStack(alignment: .leading, spacing: 8) {
                    if let details = entry.details, !details.isEmpty {
                        Text(details)
                            .font(.callout)
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    HStack(spacing: 6) {
                        Text(entry.type)
                        Text("·")
                        Text(entry.source)
                    }
                    .font(.caption2).foregroundStyle(.secondary)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 12).padding(.vertical, 3)
    }

    private func chip(_ text: String) -> some View {
        Text(text).font(.caption2).foregroundStyle(.secondary)
            .padding(.horizontal, 6).padding(.vertical, 3)
            .background(.primary.opacity(0.05), in: Capsule())
    }
}
