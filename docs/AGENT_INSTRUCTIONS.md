# Canonical agent reporting instructions

The canonical skill is [`skills/daylog/SKILL.md`](../skills/daylog/SKILL.md).
Install that same instruction set for every harness. No mode-dependent prompts or separate publishers.

For harnesses without skill discovery, use this block:

```markdown
## Work reporting (daylog)

Report facts, findings, decisions, changes, and incomplete results with:

    daylog add --type <work|sidequest|note> --ref '#142' "factual report"

Distinguish proposed, attempted, implemented, tested, and deployed work honestly.
Do not apply a journal materiality rubric or write publication-ready copy: Athena,
daylog's gatekeeping subsystem, handles relevance, wording, grouping, duplicates, and holds. Unnecessary
reports are acceptable private inputs; do not narrate every tool call. Reports are
bounded at 16 KiB. Include useful refs; PR/CI current state stays in its separate
snapshot. Never claim verification you did not perform.

The harness sets DAYLOG_SOURCE=agent:<name>. Never impersonate a human or write
data files. `queued <candidate-id>` confirms capture, not publication. Persistence
failure is an error. Supply --idempotency-key only when you have a stable request
identity, and reuse it only for identical content.

An explicit todo is a deliberate proposal for the human, not a work report or your
own task tracker. Do not adopt, decline, or complete obligations for the human.
```
