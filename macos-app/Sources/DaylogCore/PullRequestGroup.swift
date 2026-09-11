import Foundation

/// Branch relationships among fetched PRs only, not a claim of complete stack membership.
public struct PullRequestGroup: Identifiable, Sendable {
    public struct Row: Identifiable, Sendable {
        public let pr: PullRequest
        public let parentNumber: Int?
        public var id: String { pr.id }
    }
    public let rows: [Row]
    public var id: String { rows[0].id }
    public var isStack: Bool { rows.count > 1 }

    public static func group(_ prs: [PullRequest]) -> [Self] {
        // Require an unambiguous repository + branch match. Missing metadata from
        // old snapshots (or deleted fork repositories) must never invent links.
        var parents: [Int: Int] = [:]
        for (child, pr) in prs.enumerated() {
            guard let base = pr.base_branch, !base.isEmpty,
                  let host = pr.safeURL?.host?.lowercased() else { continue }
            let candidates = prs.indices.filter { parent in
                parent != child && prs[parent].repo.lowercased() == pr.repo.lowercased()
                    && prs[parent].safeURL?.host?.lowercased() == host
                    && prs[parent].head_repo?.lowercased() == pr.repo.lowercased()
                    && prs[parent].head_branch == base
            }
            if candidates.count == 1 { parents[child] = candidates[0] }
        }
        // Drop cyclic relationships rather than presenting a fictitious merge order.
        let original = parents
        for start in prs.indices {
            var path: [Int] = []
            var current = start
            while let parent = original[current] {
                if let cycle = path.firstIndex(of: current) {
                    for index in path[cycle...] { parents.removeValue(forKey: index) }
                    break
                }
                path.append(current)
                current = parent
            }
        }
        func root(of index: Int) -> Int {
            var result = index
            while let parent = parents[result] { result = parent }
            return result
        }
        // Keep group order anchored at its first appearance in the snapshot.
        var roots: [Int] = []
        var seen = Set<Int>()
        for index in prs.indices {
            let root = root(of: index)
            if seen.insert(root).inserted { roots.append(root) }
        }
        return roots.map { root in
            var rows: [Row] = []
            func append(_ index: Int) {
                rows.append(Row(pr: prs[index], parentNumber: parents[index].map { prs[$0].number }))
                for child in prs.indices where parents[child] == index { append(child) }
            }
            append(root)
            return Self(rows: rows)
        }
    }
}
