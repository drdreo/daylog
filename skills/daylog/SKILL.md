---
name: daylog
description: Record substantive completed work with the daylog CLI — a meaningful implementation, diagnosis, decision, or system change. Use at most once per task, and only when the outcome still matters tomorrow. Never for PR or CI status, routine failures or retries, questions answered, files read, trivial edits, or progress updates.
metadata:
  source: github.com/drdreo/daylog
---

# Log substantive work

This log is read once a day, by one human catching up on what happened.
Every entry spends their attention, so the test is not "did I finish
something" but "would its absence lose something worth knowing".

## What clears the bar

Log a task that left something behind:

- code, documentation, or product behavior materially changed
- a bug reproduced, diagnosed, or fixed
- research, an investigation, or a review that reached a conclusion
- configuration, infrastructure, schema, or data changed
- a key milestone that materially changes what the human needs to know

## What does not

- answering a question, explaining code, comparing options
- reading, searching, or navigating a codebase
- a trivial edit: a typo, a rename, a one-line tweak with no consequence
- progress updates, or a second entry for work already logged
- pull-request lifecycle or status: opened, pushed, review requested,
  approved, merged, closed, or conflicted
- CI and check status: pending, passing, failing, cancelled, retried, or fixed
- a failed command, test, check, or attempt that produced no durable diagnosis
  or decision
- anything whose only artifact is the conversation itself

When it is borderline, do not log. A missing entry costs nothing; a log
full of noise stops being read.

The GitHub poller owns PR lifecycle, review state, links, and check results.
Do not duplicate those in an agent entry. A failure or blocker is not itself
an outcome; log only a durable diagnosis or decision produced by investigating
it, and name that result rather than the failed PR, check, or command.

## How to log

Run this from the repository or working directory where the work happened:

```sh
daylog add --type <work|sidequest> "one-line TLDR, ≤280 chars"
```

Use `work` for the task the user asked for, `sidequest` for concrete work
outside that request; when unsure, use `sidequest`.

Add `--ref '#142'` or a Linear/Jira ID for each related PR or issue,
repeating `--ref` as needed. State the outcome, not the reasoning: what is
true now that was not true before. A reference can connect the underlying
work to a PR, but does not make the PR's status loggable.

## Filing a todo

If you find an actionable task you are not doing, and it clears the same
bar, file it for the human to review:

```sh
daylog add --type todo "concrete action, for human review"
```

File todos only for the human. Do not act on, track, or close a todo you
filed unless the user explicitly asks. The triage verbs (`accept`,
`decline`) are the human's alone — the CLI rejects them from an `agent:`
source.

## Rules

- One entry per completed task. Do not split one outcome across entries.
- Before logging, verify `DAYLOG_SOURCE` starts with `agent:`. If it does
  not, report the missing harness configuration and skip the entry rather
  than guessing an identity.
- Never write directly to daylog's data files, and never pass `--source`:
  producer identity belongs in the agent harness.
- The store override is `$DAYLOG_DIR` — there is no `DAYLOG_DATA`. When
  testing against a scratch store, set it in the same command as the write:
  shell state does not persist between tool calls, and a lost export sends
  test events into the real log.
- If `daylog` is unavailable or rejects the entry, report that briefly
  instead of writing elsewhere. You may shorten an overlong TLDR and retry
  once.
