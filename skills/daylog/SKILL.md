---
name: daylog
description: Record completed agent work and actionable discoveries with the daylog CLI. Use once after completing any task, including failed or blocked attempts; do not use for ordinary progress updates or questions.
metadata:
  source: github.com/drdreo/daylog
---

# Log completed work

After completing a task, run this from the repository or working directory where the work happened:

```sh
daylog add --type <work|sidequest> "one-line TLDR, ≤280 chars"
```

Use `work` for the task the user asked for. Use `sidequest` for concrete work outside that request; when unsure, use `sidequest`.

If you discover an actionable task that you are not doing, file it for the human to review:

```sh
daylog add --type todo "concrete action, for human review"
```

- Add `--ref '#142'` or a Linear/Jira ID for each related PR or issue. Repeat `--ref` when needed.
- Log failed or blocked attempts too; state what was attempted and why it stopped.
- File todos only for the human. Do not act on, track, or later close a todo you filed unless the user explicitly asks.
- Log concrete outcomes, not reasoning, observations, progress updates, or message-by-message activity.
- Write one entry per completed task. Do not split a single outcome across entries.
- Before logging, verify `DAYLOG_SOURCE` starts with `agent:`. If it does not, report the missing harness configuration and skip the entry rather than guessing an identity.
- Never write directly to daylog's data files. Do not use `--source`; producer identity belongs in the agent harness.
- If `daylog` is unavailable or rejects the entry, report that briefly instead of writing elsewhere. You may shorten an overlong TLDR and retry once.
