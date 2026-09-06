---
name: daylog
description: Report factual work, findings, decisions, changes, and incomplete results to the private daylog queue. Athena, daylog's gatekeeping subsystem, decides journal relevance and grouping.
---

# Daylog reporting

Report what happened after a task or meaningful segment of work:

```sh
daylog add --type work --ref '#142' "Implemented refresh locking; the new regression test passed locally. Deployment has not been attempted."
```

- Report facts, findings, decisions, changes, and useful references. Distinguish proposed, attempted, implemented, tested, and deployed results honestly. Describe incomplete results as incomplete.
- Use `work` for the requested task, `sidequest` for incidental work, or `note` for a factual observation. All agent narrative types enter the same private queue.
- Do not decide journal relevance or final wording. Athena can rewrite, combine, update, skip, or hold reports. Unnecessary reports are acceptable; do not narrate every tool call.
- Reports can be up to 16 KiB; they need not be publication-ready one-liners. Never claim independent verification you did not perform.
- Add refs for relevant issues/artifacts. PR/CI status has a separate poller snapshot; describe underlying work rather than using PR movement as a work outcome.
- `queued <candidate-id>` means durably captured, NOT published. A failed command means the report was not acknowledged. If you have a stable request identity, supply `--idempotency-key`; reuse it only for identical content.
- Explicit `todo` is a proposal for the human, not a narrative report or your own task tracker. File one only when deliberately proposing an action. Never adopt, decline, or complete obligations for the human.
- Source identity comes from the harness's `DAYLOG_SOURCE=agent:<name>` environment. Never override it to `human:*`, inspect worker mode to change routing, or write directly to daylog data files.
