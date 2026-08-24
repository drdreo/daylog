# Canonical agent instruction block

Paste the block below verbatim into each agent's global config —
`~/.claude/CLAUDE.md` for Claude Code, `~/.codex/AGENTS.md` for Codex,
pi's equivalent.

Agent identity is deliberately **not** in the prompt: each harness's launch
wrapper sets `DAYLOG_SOURCE=agent:<name>` once (e.g. in a shell alias or
wrapper script), so `source` is correct by construction rather than by
agent self-report.

For skill-aware Codex and Claude Code installations, the same workflow is
packaged in [`../skills/daylog/SKILL.md`](../skills/daylog/SKILL.md). Run
`./install.sh --skills` to install it in both global skill directories.

---

```markdown
## Work logging (daylog)

When a task leaves something behind — code committed, pushed or merged; a
PR opened or landed; a bug diagnosed or fixed; research or a review that
reached a conclusion; infrastructure, schema or data changed — run:

    daylog add --type <work|sidequest> "one-line TLDR, ≤280 chars"

If you notice something actionable that you are NOT doing, file it for
the human to review:

    daylog add --type todo "concrete action, for human review"

- Add `--ref '#142'` (or a Linear/Jira id) for any PR or issue involved.
- `work` = the task you were asked to do; `sidequest` = anything you did
  that wasn't the original ask. When unsure, use `sidequest`.
- Do NOT log: questions answered, code explained or read, trivial edits,
  progress updates, or work whose only artifact is the conversation. When
  it is borderline, do not log — a log full of noise stops being read.
- A substantial attempt that failed is worth logging ("attempted X,
  blocked by Y"); a trivial one is not.
- Todos go to the human's review queue. Do not act on them, track them,
  or file them for yourself — filing one ends your involvement with it.
  Triage (`accept`/`decline`) is the human's alone.
- One entry per completed task. Never write to daylog's data files directly.
```
