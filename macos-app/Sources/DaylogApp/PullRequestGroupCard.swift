import SwiftUI
import DaylogCore

struct PullRequestGroupCard: View {
    let group: PullRequestGroup

    var body: some View {
        if group.isStack {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label("PR stack", systemImage: "square.3.layers.3d")
                        .foregroundStyle(Color.secondary)
                    Spacer()
                    Text("\(group.rows.count) PRs · base first").foregroundStyle(.secondary)
                }
                .font(.caption.weight(.medium))
                .help("Inferred from base/head branches among fetched open PRs. Other stack members may not be in this snapshot.")
                VStack(spacing: 0) {
                    ForEach(group.rows) { row in
                        HStack(spacing: 8) {
                            VStack(spacing: 0) {
                                Rectangle().fill(Color.secondary.opacity(row.id == group.rows.first?.id ? 0 : 0.3))
                                Circle().fill(Color.secondary).frame(width: 7, height: 7)
                                Rectangle().fill(Color.secondary.opacity(row.id == group.rows.last?.id ? 0 : 0.3))
                            }.frame(width: 2)
                            VStack(alignment: .leading, spacing: 4) {
                                if let parent = row.parentNumber {
                                    Text("Based on #" + String(parent))
                                        .font(.caption2).foregroundStyle(.secondary)
                                }
                                PullRequestCard(pr: row.pr)
                            }.padding(.vertical, 3)
                        }
                        .fixedSize(horizontal: false, vertical: true)
                    }
                }.padding(.leading, 4)
            }
            .padding(10)
            .background(Color.secondary.opacity(0.035), in: RoundedRectangle(cornerRadius: 14))
            .overlay {
                RoundedRectangle(cornerRadius: 14).strokeBorder(Color.secondary.opacity(0.18))
            }
        } else if let row = group.rows.first {
            PullRequestCard(pr: row.pr)
        }
    }
}
