# Native capture contracts (adapter v1)

Adapters feed the same private queue as `add`. They never publish or request continuation. Native stdin may include additional documented harness fields; only the allowlisted fields in `internal/capture/adapters.Hook` are extracted. Daylog-owned candidate/receipt/plan/config JSON is strict, unlike extensible native payloads.

## Verified references

- **pi 0.85.1:** installed `docs/extensions.md`, `docs/session-format.md`, `docs/json.md`, and runtime types. `agent_settled` follows retries/compaction/follow-ups; `agent_end` is not equivalent. Native session v3 preserves node IDs and parent links.
- **Claude Code 2.1.224:** [official hooks reference](https://code.claude.com/docs/en/hooks). Stop/SubagentStop provide `last_assistant_message`; transcript writes may lag. `prompt_id` is available in the supported release. StopFailure text is an API failure, never a completed outcome.
- **Codex 0.153.2:** [release-tag hook schemas](https://github.com/openai/codex/tree/rust-v0.153.2/codex-rs/hooks/schema/generated) (`stop.command.input.schema.json`, `stop.command.output.schema.json`, subagent/interrupt equivalents) and [tagged protocol](https://github.com/openai/codex/blob/rust-v0.153.2/codex-rs/protocol/src/protocol.rs). Stop/SubagentStop require valid neutral JSON; Interrupt cannot restart a turn.

The installed versions and pi model catalog were inspected without inference. Sanitized hand-authored fixtures model these contracts; no private transcript corpus was read or uploaded for implementation. The fixtures are not a claim that every live native history shape is supported.

## Hook path

`daylog --data-dir ABSOLUTE capture --adapter HARNESS --harness-version VERSION`

Success emits only `{}` and exit 0. Failure emits an explicit nonzero error on stderr, never exit-2 continuation feedback. There is no silent success on persistence failure. Native stdout never contains `queued` or model output.

Installation adds SessionStart and supported terminal hooks without replacing unrelated handlers. The pi extension is `pi/daylog.ts`, installed with a private adjacent settings file holding absolute daylog/store paths. It registers no tools and sends no prompts. `DAYLOG_INTERNAL=1` prevents recursion. All hooks validate original project approval before capturing git context or excerpts; terminal hooks do not open caller-supplied transcript paths. The git context probe has a shared 750 ms deadline.

Source environments must identify working agents. The pi extension sets `DAYLOG_SOURCE=agent:pi` and tracks durable user-node IDs in `DAYLOG_TURN_ID`; native pi shell tools already supply `PI_SESSION_ID`. Claude/Codex callers can supply `DAYLOG_SESSION_ID`, `DAYLOG_TURN_ID`, `DAYLOG_TASK_ID` and `DAYLOG_PARENT_SESSION_ID` through launch/tool environments when known. Report/hook exact episode linkage is strongest when those identifiers align; unknown identity is not guessed.

## Reconciliation

Only configured native directories are traversed; symlink entries are not followed. Every file additionally needs an approved original project. Prefix boundaries use path components and existing symlinks are canonicalized before approval. Scan bounds: 10,000 traversed paths, 64 rotating files per run, 2 MiB/128 candidates per file, and a 1 MiB native-line cap. Narrow scopes rather than scanning all historical sessions.

- **pi:** parses session v3 header and assistant text nodes. Tool-call continuations, reasoning, tool output, compaction summaries and branch summaries are not recaptured as outcomes. Parent-to-request links distinguish branches; copied nodes use cwd/node/occurrence/revision keys. An ancestry cap is explicit rather than dropping links.
- **Claude:** extracts version-tagged user/assistant JSONL records with session/cwd metadata. Hook/native claims with identical final text and terminal state share native delivery identity; changed final text is new evidence. Not every native message carries prompt metadata, so exact report/native task association can remain unknown.
- **Codex:** extracts supported `session_meta`, `turn_context` and `event_msg` terminal records (`task_complete`, `turn_aborted`). Intermediate reasoning/tool payloads are excluded. Exact session/turn/revision keys align terminal hooks and recovery. Reference-based history is not recursively followed into arbitrary files.

Offsets advance only after successful persistence. If the cursor write is interrupted, replay reuses native identity. An incomplete tail is retained and diagnosed for a later flush. A changed header or truncated file stops at the preserved cursor and requires inspection, not an automatic reset. SessionStart snapshots and native metadata preserve available original context after deleted worktrees; workers never run git in their own cwd to fill gaps.

## Limits and privacy

Assistant excerpts are claims, not independent execution evidence. Redaction removes obvious sensitive lines and caps excerpts; it is not a secret scanner guarantee. `.env`, credential files, hidden reasoning, images and raw tool streams are not evidence sources. Both report text and human/preference wording are filtered before editorial submission. Native evidence lives separately and can be pruned after processing; original explicit report text is retained privately.

Known gaps include ephemeral/hard-crashed sessions without saved evidence, unsupported versions, Claude transcripts without supported metadata, Codex logical history references, unknown custom child lineage, and changed/truncated cursors requiring intervention. A session is never treated as a unique task. Repeated claims are never treated as independent verification.

**Live harness capture has not been exercised in this implementation pass.** Go fixture tests and a mock-pi Node test verify normalization, neutral responses, overlap, branched/copied histories, partial tails, deleted cwd handling, scope checks and extension behavior. Approve a narrow synthetic/sanitized scope and inspect `status`, `explain` and shadow decisions before operational installation/cutover.
