import SwiftUI
import DaylogCore

struct PullRequestCard: View {
    let pr: PullRequest
    @State private var hovering = false

    var body: some View {
        Group {
            if let url = pr.safeURL {
                Link(destination: url) { content }
                    .buttonStyle(.plain)
                    .help("\(pr.title)\nOpen \(pr.repo)#\(pr.number) in your browser")
            } else {
                content
            }
        }
        .onHover { hovering = $0 && pr.safeURL != nil }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: "arrow.triangle.pull")
                    .foregroundStyle(pr.draft ? Color.secondary : Color.green)
                // String interpolation avoids localized grouping in PR numbers (94 448).
                Text(pr.repo + " #" + String(pr.number))
                    .lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 4)
                if pr.draft {
                    Text("Draft").font(.caption2.weight(.medium))
                        .padding(.horizontal, 7).padding(.vertical, 3)
                        .background(.primary.opacity(0.06), in: Capsule())
                }
                if pr.safeURL != nil {
                    Image(systemName: "arrow.up.right")
                        .foregroundStyle(hovering ? Color.accentColor : Color.secondary)
                }
            }
            .font(.caption).foregroundStyle(.secondary)

            Text(pr.title)
                .font(.system(.body, weight: .medium))
                .foregroundStyle(.primary)
                .lineLimit(1)
                .truncationMode(.tail)
                .frame(maxWidth: .infinity, alignment: .leading)

            HStack(spacing: 6) {
                badge(pr.checksBadge, label: "Checks")
                if pr.review == "approved" || pr.review == "changes_requested" {
                    badge(pr.reviewBadge)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .background(hovering ? Color.accentColor.opacity(0.08) : Color.primary.opacity(0.035),
                    in: RoundedRectangle(cornerRadius: 12))
        .overlay {
            RoundedRectangle(cornerRadius: 12)
                .strokeBorder(hovering ? Color.accentColor.opacity(0.35) : Color.primary.opacity(0.06))
        }
        .contentShape(Rectangle())
        .animation(.easeOut(duration: 0.12), value: hovering)
    }

    private func badge(_ status: PRStatusBadge, label: String? = nil) -> some View {
        let color: Color = {
            switch status.tone {
            case .positive: return .green
            case .negative: return .red
            case .pending: return .orange
            case .neutral: return .secondary
            }
        }()
        return Label(label ?? status.label, systemImage: status.symbol)
            .accessibilityLabel(status.label)
            .help(status.label)
            .font(.caption.weight(.medium))
            .foregroundStyle(color)
            .padding(.horizontal, 7).padding(.vertical, 3)
            .background(color.opacity(0.10), in: Capsule())
    }
}
