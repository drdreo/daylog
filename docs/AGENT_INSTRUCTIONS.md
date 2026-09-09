# Canonical agent reporting instructions

The canonical skill is [`skills/daylog/SKILL.md`](../skills/daylog/SKILL.md).
Install that same instruction set for every harness. No mode-dependent prompts or separate publishers.

For harnesses without skill discovery, use this block:

```markdown
## Work reporting (daylog)

Give a brief work handover, not a polished headline or a certification:

    daylog add --type <work|sidequest|note> --ref '#142' "factual report"

In a few natural sentences, describe what changed, was attempted, or was learned;
checks you actually performed and their results; and important uncertainty or
unfinished work. Include useful context or rationale, not raw tool output. There
is no mandatory form, proof attachment, or requirement to run extra checks to log.
Distinguish observations from inferences and proposed, attempted, implemented,
tested, and deployed work. Partial results can be useful when honestly described.

Athena handles relevance, faithful wording, grouping, duplicates, and holds. Do not
self-filter for journal materiality or compress away limitations to fit a headline.
Reports can be up to 16 KiB; the journal's short-headline limit does not apply.
Unnecessary reports are acceptable private inputs; do not narrate every tool call.
Include useful refs; PR/CI current state stays in its separate snapshot. Never
invent checks or claim verification you did not perform.

The harness sets DAYLOG_SOURCE=agent:<name>. Never impersonate a human or write
data files. `queued <candidate-id>` confirms capture, not publication. Persistence
failure is an error. Supply --idempotency-key only when you have a stable request
identity, and reuse it only for identical content.

An explicit todo is a deliberate proposal for the human, not a work report or your
own task tracker. Do not adopt, decline, or complete obligations for the human.
```
