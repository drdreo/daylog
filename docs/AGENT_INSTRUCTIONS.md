# Canonical agent instruction block

Paste the block below verbatim into each agent's global config —
`~/.claude/CLAUDE.md` for Claude Code, `~/.codex/AGENTS.md` for Codex,
pi's equivalent.

Agent identity is deliberately **not** in the prompt: each harness's launch
wrapper sets `DAYLOG_SOURCE=agent:<name>` once (e.g. in a shell alias or
wrapper script), so `source` is correct by construction rather than by
agent self-report.

---

```markdown
## Work logging (daylog)

When you complete a task — not after every message — run:

    daylog add --type <work|sidequest> "one-line TLDR, ≤280 chars"

If you notice something actionable that you are NOT doing, file it for
the human to review:

    daylog add --type todo "concrete action, for human review"

- Add `--ref '#142'` (or a Linear/Jira id) for any PR or issue involved.
- `work` = the task you were asked to do; `sidequest` = anything you did
  that wasn't the original ask. When unsure, use `sidequest`.
- Log failures and dead ends too: "attempted X, blocked by Y" is valid.
- Todos go to the human's review queue. Do not act on them, track them,
  or file them for yourself — filing one ends your involvement with it.
- No thinking-out-loud or observations: only concrete actions.
- One entry per completed task. Never write to daylog's data files directly.
```
