---
name: daylog
description: Give Athena a brief work handover—what changed or was learned, checks actually performed, and important limitations. Athena turns private reports into useful journal entries.
---

# Give Athena a work handover

After a task or meaningful segment of work, give a brief handover, not a polished headline or a certification. A few natural sentences are usually enough:

- What changed, was attempted, or was learned? Include useful context or a decision's rationale when it helps explain the result.
- What checks did you actually perform, and what did they show?
- What remains uncertain, untested, or unfinished?

Mention these when relevant; there is no mandatory form or requirement to run extra checks just to log. Do not paste transcripts or raw tool output. Distinguish what you observed from what you inferred, and proposed, attempted, implemented, tested, and deployed work from each other.

```sh
daylog add --type work --ref '#142' "Implemented name-based campaign collections so renames no longer break membership. Added rename/deletion recovery tests; those pass locally. Haven't exercised the UI yet. Older briefs remain discoverable through the fallback."
```

- Report concrete observations, findings, decisions, changes, and useful references. Partial results can be useful; describe their limitations rather than treating the task as either completely done or not worth reporting.
- Use `work` for the requested task, `sidequest` for incidental work, or `note` for a factual observation. All agent narrative types enter the same private queue.
- Do not decide journal relevance or final wording. Athena can rewrite, combine, update, skip, or hold reports. Unnecessary reports are acceptable; do not narrate every tool call.
- Reports can be up to 16 KiB; the journal's short-headline limit does not apply to your handover. Keep useful context instead of compressing away limitations. No proof attachments or independent audit are required; never invent checks or claim verification you did not perform.
- Add refs for relevant issues/artifacts. PR/CI status has a separate poller snapshot; describe underlying work rather than using PR movement as a work outcome.
- `queued <candidate-id>` means durably captured, NOT published. A failed command means the report was not acknowledged. If you have a stable request identity, supply `--idempotency-key`; reuse it only for identical content.
- Explicit `todo` is a proposal for the human, not a narrative report or your own task tracker. File one only when deliberately proposing an action. Never adopt, decline, or complete obligations for the human.
- Source identity comes from the harness's `DAYLOG_SOURCE=agent:<name>` environment. Never override it to `human:*`, inspect worker mode to change routing, or write directly to daylog data files.
