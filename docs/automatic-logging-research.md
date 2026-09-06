# Automatic daylog: reliable capture, selective publication

- **Status:** research proposal; not implemented or enabled.
- **Research date:** 2026-09-06
- **Repository inspected:** `979cd7b`
- **Method:** local implementation/configuration audit, scratch-store experiments, three independent GPT-5.6 Sol/xhigh researchers (comparables, harness interfaces, skeptical architecture), and primary-source verification.

**Follow-up decision:** the [gatekeeper architecture and implementation plan](gatekeeper-plan.md) selects a report-first queue with a one-shot pi/Luna editor. It supersedes this report's initial hook-first build order, queue-storage options, model suggestion, and shadow migration sketch. This document preserves the research and rationale.

## Recommendation in one paragraph

Keep daylog's Go CLI, append-only outcome ledger, PR snapshots, and widgets. Replace skill-dependent automatic logging with **small deterministic harness adapters + recovery scanning + a separate outcome editor**. Adapters capture evidence, not log entries. The editor can explicitly publish nothing, combine related work, or hold an incomplete outcome. Only selected, evidenced outcomes reach the ledger. Give the human lightweight correction controls and train the policy on their examples. Start in shadow mode; do not enable automatic publication until its output is demonstrably worth reading.

> Put capture reliability in code, relevance in one editor, and personal taste in examples.

## 1. What we have

### The foundations are worth keeping

- One small, local write interface with source identity, Git context, typed refs, and a validated 280-character TLDR: [`cmd/add.go`](../cmd/add.go), [`cmd/root.go`](../cmd/root.go), [`internal/event/event.go`](../internal/event/event.go).
- Immutable JSONL events and a deterministic derived view: [`internal/store/store.go`](../internal/store/store.go), [`internal/view/fold.go`](../internal/view/fold.go).
- Correct separation of **what happened** from **what needs me now**. Open todos and live PR state are separate collections; closed todos enter the log on their closure day.
- Three thin widgets consuming `daylog today --json`. They do not need to understand transcript formats or models.
- The GitHub poller is now snapshot-only. Its tests protect against reintroducing PR/CI narration.
- A thoughtfully restrictive [skill](../skills/daylog/SKILL.md). Recent commits deliberately raised the materiality bar (`0518c86`), excluded CI/PR status (`3d5fd9a`), and removed GitHub narrative transitions (`21491a5`). Automation should preserve those decisions.

### What is missing

| Finding | Consequence |
| --- | --- |
| The model must notice the skill, decide relevance, remember to invoke the CLI, and choose the right granularity | Capture and editorial judgment fail together; absence is unobservable |
| `add` validates shape, not relevance | Valid prose can still be status noise or a duplicate |
| There is no session/request/task identity or idempotency key | “Once per task” is unenforceable; retries and multiple agents create distinct ULIDs |
| The architecture mentions a Stop-hook reminder, but the repo installs no such hook | Documented intent is not an operational backstop |
| Context is captured in the writing process's current cwd | An asynchronous worker would log its own repo/branch unless capture-time context is preserved |
| `reclassify` only changes type; triage only handles todos | No supported way to fix a generated TLDR, dismiss a work false positive, or merge duplicates |
| `ts` is append time | Delayed processing can put yesterday's work into today's log |
| `O_APPEND` is the concurrency strategy | It does not make check-then-append idempotent or provide crash durability; there is no `fsync` |
| Malformed JSONL lines are silently skipped, including middle lines | Corruption can look like a quiet day |
| Source is an environment/flag convention | Useful attribution and accident prevention, not authentication against another process running as the same user |

A scratch-store experiment confirmed that the CLI accepts **two identical** agent work entries saying `Read README; CI passed`, plus an agent `note` saying `Routine retry completed`. All three render normally. This is not a claim that arbitrary string filtering would fix relevance; it demonstrates where enforcement currently stops. The experiment used `DAYLOG_DIR` on every invocation and did not write test entries into the real ledger.

### Local installation findings

- `DAYLOG_SOURCE` is configured for Claude, Codex, and pi; this session correctly resolves to `agent:pi`.
- Both installed skills match the repository skill byte-for-byte.
- The inspected Claude and Codex hook configurations contain `SessionStart` hooks, but no daylog hook. No daylog extension was listed in pi settings.
- The installed binary's build provenance references `370e324` with dirty modifications, not checkout HEAD. **The exact installed-source delta is unknown**; do not infer old poller behavior from the revision alone. Add version/build reporting to future diagnostics and make upgrades deliberate.
- The existing local store contained 59 events across 12 day-files: 22 work, 11 todo, 11 done, 7 historical transition, 5 triage, and 3 note. No malformed lines or exact repeated narrative texts were found. These counts do **not** establish recall or semantic precision; there is no labeled denominator of work that should have been captured.

Baseline validation passed: `go test ./...`, `go vet ./...`, and `go test -race ./...`. There are no current CLI/store tests; a passing race run does not verify cross-process write safety.

## 2. Define the product before the hooks

Daylog should answer:

1. **What is meaningfully different because of today's work?**
2. **What did we establish or decide that changes what I do next?**
3. **What needs me?** — still owned by explicit todos and live external snapshots.

It should not answer “what commands ran?” or become an agent-memory database, timesheet, productivity score, or second issue tracker.

The journal unit is a **durable task outcome**, not a tool call, model turn, session, commit, or PR.

Two policy ambiguities need deliberate resolution:

- The skill permits research/diagnoses but rejects anything whose only artifact is a conversation. Proposed interpretation: a reproducible diagnosis or an adopted, consequential decision can qualify without a code edit, if its conclusion and evidence can be preserved. An explanation, an unadopted recommendation, or options discussed without a conclusion do not qualify merely because the answer sounds substantial.
- “A missing entry costs nothing” is a useful tie-break for borderline relevance, but wrong for capture health. Missing trivia is acceptable; silently losing a week's substantive work is not. **Abstain editorially, report operational failure.**

### Proposed selection rubric

Publish only when all four tests pass:

1. **Outcome:** something materially changed, was established, or was explicitly decided—not merely attempted.
2. **Evidence:** the wording is supported by an artifact, a concrete verified diagnosis, or an adopted decision. “The agent says done” is not independent verification of every claim.
3. **Relevance:** absence would lose context the human is likely to need tomorrow.
4. **Novelty:** this adds something not already represented by a published outcome.

No numerical file-count, elapsed-time, token-count, or diff-size threshold substitutes for these tests. A one-line security fix can matter more than 3,000 generated lines. Read-only research can qualify; running tests successfully by itself does not.

| Example | Decision |
| --- | --- |
| Read and explained an unfamiliar module | Skip |
| Renamed a local variable without behavioral consequence | Skip |
| Fixed an authorization check with a one-line patch | Publish the behavior change |
| Opened a PR; pushed; CI passed; checks were retried | Skip; PR snapshot owns status |
| Investigated a failing check and established an actual root cause with evidence | Publish the diagnosis, not the failure |
| Compared Redis and SQLite without a decision | Skip |
| Adopted SQLite after a measured experiment; saved the rationale | Publish the decision and consequence |
| Agent proposes a fix, but has not applied or validated it | Hold; do not claim it was fixed |
| Three research agents contribute to one design conclusion | One parent outcome, not three reports plus a summary |
| Another agent reviews the same fix and finds nothing consequential | No second outcome |
| Review identifies a separate material vulnerability | A distinct outcome can qualify |
| Task continued the next day and reached a new material milestone | A new dated delta can qualify; do not replay yesterday |

**Do not impose a hard daily quota.** A busy day may contain many genuinely relevant outcomes. Use grouping and a compact digest to manage reading effort, not silent deletion after the fifth entry.

## 3. What others built, and what to borrow

The following are inspected implementations or explicitly labeled documentation claims, not promises of measured reliability.

| Project | Useful pattern | Why not simply use it as daylog? |
| --- | --- | --- |
| [Engineering Notebook][notebook] | Scans Claude/Codex transcripts, groups by date/project, LLM emits `SKIP`, persists skipped groups | Very close to the diary use case, but coarse project/day identity, a different UI/stack, and weak handling of changed inputs in the inspected ingestion/grouping paths |
| [Entire CLI][entire] | Hook/checkpoint state machines, strong Git provenance, outcome-first `dispatch` digest | Primarily code/session attribution and recovery; Git checkpoints are not task boundaries, and transcript-in-Git storage is a poor privacy default here |
| [claude-mem][claude-mem] | Durable pending work, deterministic exclusions, observer vocabulary, explicit no-output path | Primarily future-agent memory; per-tool observations, context injection, vector storage, and broader service machinery are unnecessary for this journal |
| [claude-memory-compiler][compiler] | Terminal/pre-compaction capture; changed-input hashing for knowledge compilation | Inspected code uses a 60-second last-session dedup window and can append “Nothing worth saving” to a daily file—exactly the noise daylog should avoid |
| [cass][cass] | Harness connectors, native-log ingestion, provenance, watching plus periodic reconciliation | An evidence/search substrate, not a materiality filter or human journal; no need to adopt its entire index |
| [Agent Trace mirror/RFC][trace] | Model/tool/session/VCS provenance and content-hash vocabulary | File/line attribution, not outcome selection. The inspected mirror is not proof of a universally adopted current standard |

### The most instructive failure: forgetting that a skip happened

Engineering Notebook documented that about 44 trivial session groups kept being sent to the model on every run, adding about 12 minutes to a recurring job. The fix persisted skipped groups so they disappeared from visible views but were not reconsidered endlessly ([upstream design][notebook-skip]). These are the project's reported numbers, not a benchmark reproduced here.

There is a second lesson in its inspected code: ingestion skips an already-known path/session unless forced, and grouping excludes existing `(date, project)` entries. That can leave newly appended session content or later same-day work unconsidered by those paths. Do not copy a permanent “this session/day is done” cache.

**Persist the editorial disposition against the exact evidence revision and policy version.** New evidence invalidates the old decision; an identical retry does not. Keep operational skips outside the human ledger instead of hiding sentinel journal rows in every consumer.

### Dedup is often weaker than the README sounds

The inspected claude-mem implementation deduplicates identical generated title/narrative content within a memory session. Paraphrases or different sessions can survive. Entire can group related checkpoints in a digest, but that is not the same as durable task-level deduplication.

Daylog needs both **deterministic retry idempotency** and **semantic outcome coalescing**. Neither one replaces the other.

## 4. Actual agent integration surfaces

Installed versions checked: Claude Code `2.1.224`, Codex `0.153.2`, pi `0.85.1`. These are compatibility research findings, not adapters tested end-to-end in daylog. Live docs can describe features newer than these releases; implement against fixtures and installed APIs.

| Harness | Primary capture boundary | Native correlation | Important caveat |
| --- | --- | --- | --- |
| Claude Code | `Stop`; `SubagentStop`; `StopFailure` for terminal API failures | Session ID, prompt ID on supporting releases, child agent ID, tool-use IDs | `Stop` is not user-task completion, and other hooks may continue the turn. The final assistant text in the payload can be ahead of the asynchronously written transcript |
| Codex | `Stop`; `SubagentStop`; `Interrupt` | Session/thread ID, turn ID, child agent ID, tool-use IDs | Hooks are stable/enabled in the inspected installation. No equivalent `StopFailure` hook; recovery must cover unrecovered errors. Hook definitions require trust review |
| pi | **`agent_settled`** | Session UUID plus durable leaf entry ID; message/tool-call IDs | `turn_end` fires per model/tool cycle; even `agent_end` can precede retries/queued continuation. `agent_settled` means no automatic continuation remains, not that a human task necessarily succeeded |

### Practical consequences

- **Use synchronous fast enqueue, asynchronous analysis.** A hook should perform bounded local persistence, return the harness's valid neutral response, and never call a model or request continuation. Codex `Stop` expects its JSON output contract; do not pipe the current `daylog add` success text into hook stdout.
- `SessionEnd` is a cleanup/final-flush opportunity, not the primary trigger. Current Codex can delay it until an idle, unattached thread has been inactive for 30 minutes and caps its handler at three seconds. Hard crashes can skip graceful teardown entirely.
- Background hooks are not durable queues. Both ecosystems have teardown/cancellation caveats; first persist the envelope, then let daylog own processing.
- Codex is **not notify-only** anymore. Keep `notify` as an optional fallback for older versions, with an explicitly lower capability level.
- Claude/Codex transcript schemas are not stable hook contracts. Version parsers; prefer documented Codex app-server history APIs where practical, with rollout parsing as a tested fallback.
- pi session JSONL is a tree. `/tree`, forks/clones, copied ancestry, and compaction summaries must not count old evidence twice. Key by native nodes/cursors, not just `turnIndex` or line number. Installed pi docs and the release-tag implementation differ around newer `retainedTail` compactions; fixtures must follow the actual runtime.
- pi has no universal native subagent lineage API. Extensions may spawn ephemeral child processes. Use an explicit inherited parent/origin convention when available; preserve unknown lineage as unknown.
- A session with `--no-session` cannot be recovered from a transcript that was never saved. Capture its minimal envelope while alive; report this limitation honestly.
- Install globally for the personal workflow, preserving existing configurations and their trust requirements. Do not bypass hook trust or overwrite other hooks to make setup appear automatic.

**Agent-agnostic means a stable core with small versioned adapters, not zero agent-specific code.** A generic process wrapper can support additional CLIs, but does not replace within-session boundaries or cover desktop launches automatically.

## 5. Proposed architecture

```text
Claude hooks ─┐
Codex hooks ──┼──► daylog capture ──► durable local evidence queue
pi extension ┘            ▲                      │
                    recovery scan                ▼
                                            outcome editor
                                      skip / hold / publish / relate
                                                  │
                                       validated, idempotent writer
                                                  │
                                    existing events/YYYY-MM-DD.jsonl
                                                  │
                                 existing fold + widgets + daily digest
```

These commands are proposed interfaces, not commands available today:

```sh
daylog capture --adapter pi           # adapter envelope on stdin
daylog reconcile                     # recover unseen native evidence
daylog curate --once --shadow        # dispositions only; no journal writes
daylog doctor                        # installation, capture, queue, parser health
daylog explain <entry-or-candidate>   # local evidence + selection reason
```

### A. Capture evidence without creating journal noise

A versioned envelope should contain:

- adapter/harness version, native event kind, capture time;
- native session, prompt/turn/leaf and tool IDs where available;
- parent/root linkage **only when known**, and internal-worker origin;
- terminal state: settled, failed, interrupted, or unknown;
- original cwd, repository identity including Git host, worktree identity, captured branch and HEAD;
- final assistant text and a bounded slice of relevant user intent/tool evidence;
- evidence references, content hashes/cursors, original timestamps, and completeness flags.

Capture the context before the worktree disappears or the worker runs elsewhere. A daemon must never call the existing `add` path and accidentally attribute all work to its own cwd. Preserve the CLI-owned write boundary, but add a validated ingestion path for capture-time context rather than accepting model-invented context.

Prefer a small machine-local SQLite queue under `capture/`, with unique native evidence keys and transactional cursor/decision updates. This is operational storage, **not a replacement for the JSONL journal**. An atomic-file spool is a valid smaller first implementation if claims, retries, and locking remain explicit.

Pending capture records are not disposable caches until handled. Document this distinction from the existing `state/` snapshots. Do not put them into `events/`, `open_todos`, or `needs_triage`.

### B. Make capture and editorial state separate

Operational states: `pending`, `processing` with a recoverable lease, `processed`, `error`, `unsupported`. Editorial dispositions on a processed evidence revision: `skip`, `hold`, `publish`, or `relate` to an existing outcome.

Persist reason codes, evidence revision, parser/policy/model versions, and published event IDs. A changed evidence revision can reopen a skipped or held candidate; an identical event must not cause another model call.

`daylog doctor` should distinguish:

- adapter installed vs actually observed;
- last hook receipt and reconciliation progress;
- intentional skip vs missing/incomplete evidence;
- queue age, model failures and expired leases;
- unsupported harness formats and known coverage gaps.

A healthy empty day is different from a broken collector. Surface operational problems separately, never as work entries. A heartbeat alone cannot prove coverage; reconcile against known native sessions where possible.

### C. Group evidence into task episodes

Use several identities instead of pretending one solves everything:

1. **Evidence ID:** adapter + native session + event/node/turn identity. A changing record also has a content revision.
2. **Episode ID:** a contiguous user-request effort and its continuations; not every model turn and not an entire long-lived session.
3. **Work-item ID:** an internal durable identity linking episodes that clearly contribute to the same outcome.
4. **Artifact IDs:** commit OIDs, patch/evidence hashes, referenced documents or external changes.

Strong linkage: known parent-child request lineage, an explicit task token, shared exact evidence, or confirmed continuation. Issue/PR refs, repository, branch, and semantic similarity help find candidates but are **not unique task keys**. One issue can produce multiple important outcomes; one branch can host unrelated work.

Let the editor propose uncertain relationships, not silently fuse unrelated sessions. Shared-worktree changes are not proof of which agent authored them. Prefer separate worktrees for attribution; where agents overlap, keep the outcome if supported but mark contributor attribution uncertain rather than inventing an owner.

Coalesce repeated stops, test retries, PR lifecycle, and child summaries into one outcome. A later meaningful diagnosis or fix may update the same day's outcome, while a genuinely new milestone on a later day gets its own dated delta. Do not rewrite yesterday as though today's fix had already existed.

Keep a short configurable quiet period to absorb follow-up messages, but do not wait for the whole process to exit. A timer is a debounce, not proof of task completion.

### D. Use one bounded editor, not an autonomous second coding agent

Start with one capable model and a strict structured-output contract. Given the low volume and personal focus, tuning evidence and policy is more useful initially than a cheap-model/expensive-model cascade. GPT-5.6 Sol is an option, not a dependency; the runner should be replaceable, including by a local model.

Inputs:

- a compact evidence slice with source IDs and completeness flags;
- the known user request and concrete results;
- related already-published outcomes;
- a small set of human-labeled keep/skip/merge examples;
- the versioned materiality rubric.

Outputs: zero or more candidate outcomes, proposed relations, evidence citations, and disposition/reason. No tools, shell, browsing, auto-repair, or write access. The Go layer validates schema, allowed types, length, cited evidence IDs, and legal relation targets. Source identity, timestamps, paths, and arbitrary command execution are not controlled by generated text.

Do not treat a model's `confidence: 0.95` as calibrated probability. Publish only in categories that perform well on human-labeled examples. Missing evidence or unresolved factual contradictions should produce `hold`/`skip`, not an invented successful outcome.

A deterministic prefilter may discard known internal/duplicate/status-only records, but **“no edits” is not a hard skip**: it would lose diagnosis and research, both explicit daylog goals. Preserve the small verified tool facts needed to support conclusions; stripping all tool evidence leaves only agent claims.

### E. Make publication safe to retry and possible to correct

There are two distinct guarantees:

- The same processing result retried must not append twice.
- Different evidence describing the same outcome should normally render once.

Add CLI-owned `AppendOnce` semantics with a publication key derived from the adjudicated outcome/revision, plus evidence/artifact references in a namespaced metadata block. Lock across the check and append; all writers participating in the same store protocol must honor the lock.

For the local JSONL/queue boundary:

1. Under the store lock, look for an already-published key in the ledger or its rebuildable index.
2. Append the validated event and sync it according to the durability contract.
3. Record/acknowledge the queue receipt only after the append succeeds.
4. On replay, recover from the event's publication key if a crash happened between append and acknowledgement.

A unique key in SQLite alone cannot transact atomically with a separate JSONL append. `O_APPEND` alone cannot protect a read/check/write sequence. Test the crash windows instead of promising global exactly-once semantics. Future multi-machine semantic duplicates still need reconciliation; Phase 3 sync is not implemented today.

Add explicit append-only correction concepts:

- **amend** generated wording/refs;
- **dismiss/restore** a false positive;
- **merge** duplicate outcomes, eventually split a mistaken merge;
- **pin** a human-approved revision so automation cannot overwrite it.

Human corrections take precedence. An intentionally dismissed outcome must not reappear on the next replay. Do not implement dismissal by reclassifying work as a todo and declining it.

The fold must consume new bookkeeping events explicitly. Its current unknown-type behavior would otherwise display corrections as extra log entries. The widgets continue to consume the folded contract; only the optional correction UI needs new controls.

### F. Preserve when the work happened

Keep existing `ts` semantics for backward compatibility and add explicit occurrence metadata for automated outcomes: observed start/end, `occurred_at`, captured offset/day, and `recorded_at` or equivalent provenance. Amend the fold to use a validated occurrence time for day placement, with a fallback to legacy `ts`.

The model must not invent a date. Delayed processing, midnight, travel/DST, missing offsets, and multi-day tasks need fixtures. Git author dates are evidence, not trustworthy capture clocks. Existing todo closure semantics should remain unchanged.

### G. Keep privacy and recursion out of the happy path

- Default to approved source directories/projects. Cloud model use requires an explicit per-scope policy; work and personal repositories may differ.
- Keep raw transcripts at their native locations where possible. Persist only the bounded evidence necessary for queued recovery and an explicit provenance trail. A reasonable trial retention is seven days for sensitive excerpts, configurable—not an established requirement. Keep compact processed receipts/hashes long enough to avoid replay duplicates.
- Store capture data with user-private permissions; existing `0755`/`0644` journal defaults are not appropriate for copied transcripts or patches.
- Exclude secrets, `.env`/credential material, images, hidden reasoning, and irrelevant command output before model submission. Redaction is best-effort, not a promise that arbitrary transcripts are safe to upload.
- Treat repository text, tool output, and transcript content as untrusted data. A constrained no-tools editor limits the consequences of prompt injection but does not make its summaries infallible.
- Never sync the raw capture queue or embed transcript contents in ordinary event metadata. Local evidence references may become unavailable after retention; show that honestly.
- Tag editor/replay processes with an inherited internal-origin marker and disable their capture integrations. Exclude the **data directory**, not the daylog source repo: developing daylog is legitimate work; summarizing daylog's summaries is not.

## 6. Personalization without another inbox

The useful feedback controls are **Keep, Too minor, Duplicate, Wrong, and Missing**. They should be occasional corrections, not required approval of every candidate.

Store a small private set of examples with rationale. Retrieve a few relevant examples into the editor prompt. Start with explicit rule changes and examples rather than fine-tuning or an embedding service. Preserve the distinction between “not worth logging” and “the facts are wrong.”

Default behavior:

- High-quality supported outcomes publish automatically after the trial.
- Borderline candidates stay out of the journal.
- Inspect a small sample of skips periodically to find important misses.
- Do **not** infer todos from every failure or dropped thread. Existing human-review proposals remain explicit.
- Keep `daylog add` for human quick capture. The skill becomes optional semantic help: supply an outcome hint, attach refs, or answer an explicit “remember this.” It should not compete with the automatic publisher.

During migration, avoid two independent publishers. In shadow mode, the current skill remains the only live writer. At auto-publication cutover for a configured adapter, replace its skill's direct-add behavior with hints or disable that automatic skill path, and retain dedup against existing entries. Harnesses without an adapter can temporarily keep their current fallback.

### Ambitious features that actually fit

After capture and precision work:

- **One-minute catch-up:** derive a handful of workstream headlines from curated outcomes, with expandable detail. This is a view, not a hard cap on the ledger.
- **“What changed since I last looked?”** Use a reading cursor over outcome revisions, not another summary event.
- **Evidence-backed resume:** follow a local entry to the originating session/worktree or saved conclusion, without injecting the whole journal into every agent.
- **Weekly decisions and discoveries:** derive a recap of consequential findings, useful for avoiding repeated investigations.

Do not append an EOD summary as another ordinary `note` every time the summarizer runs. A derived/revisioned digest avoids summaries of summaries and keeps the original outcomes canonical.

## 7. Where the independent reviewers disagreed

The skeptical architecture researcher preferred a generic process wrapper and a very restrictive first gate: clean Git start, new commits, clean end, exclusive worktree ownership, and no transcript ingestion. This is an attractive **high-precision code-only mode** and a useful privacy fallback.

It is not the recommended primary product: it would systematically omit uncommitted implementations, meaningful reviews, investigations, research, and system changes—the outcomes already included in daylog's goals. It also makes automatic coverage depend on launching everything through a wrapper.

The chosen compromise is minimal, local, versioned evidence capture with explicit privacy scopes and conservative editorial decisions. Git corroborates code outcomes; it does not define all worthwhile work. Hooks provide low latency; reconciliation provides recovery. Neither promises perfect semantic completeness.

## 8. Rollout: prove usefulness before enabling it

### Phase A — fixtures, health, and a shadow vertical slice

1. Add build/version diagnostics and capture health semantics.
2. Define the normalized envelope, evidence revisions, queue state machine, and editor contract.
3. Add pi's `agent_settled` adapter first for the current harness; use a small explicitly scoped native-session replay for recovery testing. Support settled failures as evidence, not completed work.
4. Run `curate --shadow` against sanitized examples. No automatic JSONL publication and no widget noise.
5. Collect the human's labels on roughly 30–50 varied task episodes. Include valuable misses, not only existing entries; otherwise evaluation rewards reproducing the current blind spots.

### Phase B — all three agents and production-grade publication

1. Add Claude/Codex adapters and version-specific backfill fixtures. Test missing transcript paths, app-server access, trust/config disabling, and graceful vs hard termination.
2. Add idempotent publication, occurrence-time handling, dismissal/amendment, and explicit migration of the skill-based writer.
3. Exercise duplicate hooks and crash recovery before any live auto-publication.
4. Enable only validated source scopes/outcome categories. Keep the rest in shadow or manual mode.

### Phase C — personal editor and better reading

Add cross-session coalescing refinements, human feedback examples, merge controls, a compact daily view, and optional resume links. Only then consider more harnesses or machine sync.

### Evaluation and release criteria

The gold unit is a task outcome, not a session. Label keep/skip/hold, duplicate relationships, correct facts, and intended date. Split tuning and evaluation by **task**, not by adjacent turns of the same task.

Track separately:

- **Capture coverage:** known native boundaries/evidence revisions accounted for, within supported scopes.
- **Editorial precision:** would the human keep each published outcome?
- **Missed important work:** sample skipped/held episodes and compare against known meaningful tasks.
- **Duplicate burden:** multiple visible entries for the same result.
- **Factual support:** especially distinctions between proposed, implemented, tested, and deployed.
- **Operational cost:** queue lag, hook overhead, model calls/tokens, failure rate, and feedback effort.

An initial aspiration is at least 95% “worth keeping” on a held-out set, no unsupported outcome claims in that set, and zero duplicates in deterministic replay tests. These are release goals, not measured results or statistical guarantees from a tiny sample. A perfectly empty journal has perfect apparent precision and terrible recall; evaluate both.

Required fixtures include: trivial question; one-line important fix; generated churn; read-only diagnosis; inconclusive research; adopted decision; failed attempt; interrupted but durable partial result; three subagents/one task; repeated Stop continuation; resumed/forked/copied history; new work after a cached skip; same issue with distinct outcomes; shared-worktree ambiguity; deleted worktree; duplicate delivery; crash before/after append/ack; malformed queue/model data; midnight/DST/backfill; human dismissal surviving replay; hostile instructions in evidence; and worker self-recursion.

**Smallest useful next build:** one adapter → durable queue → bounded editor → inspectable shadow decisions, with replay/idempotency fixtures. Not a rewrite, not a universal agent manager, and not a logging reminder on every turn.

## Sources and inspection boundaries

Public research used public repository/docs requests; private daylog entry text and the user's transcript corpus were not supplied to external research sites. Local ledger analysis was aggregate-only. No production hooks, settings, binary, or existing events were modified as part of this proposal.

- [Claude hooks][claude-hooks], [sessions][claude-sessions], and [v2.1.224 changelog][claude-version]. Claude's implementation is closed; findings rely on official docs/release notes, not a source audit.
- [Codex hook docs][codex-hooks], [0.153.2 hook schemas][codex-schemas], [tagged turn implementation][codex-turn], and [tagged app-server history documentation][codex-history]. Current docs and tagged source differ in details such as Stop continuation; the source indicates continuation can retain a turn ID. Do not assume Stop events are unique.
- Installed pi `docs/extensions.md`, `docs/sessions.md`, `docs/session-format.md`, `docs/compaction.md`, and runtime `dist/core/agent-session.js`; [v0.85.1 source][pi-source]. In particular, the installed runtime contains `agent_settled`.
- [Engineering Notebook summarizer][notebook-code], [ingestion][notebook-ingest], and [persist-skipped-groups design][notebook-skip], inspected at `b0a34ca`.
- Entire implementation inspected at `3dbdf8b` (nightly, not a claim about every stable release): [session/checkpoint architecture][entire-checkpoints], [dispatch generation][entire-dispatch], [privacy][entire-privacy].
- claude-mem inspected at `3939fbb`: [hook manifest][mem-hooks], [observer policy][mem-policy], [observation storage][mem-store].
- claude-memory-compiler inspected at `54eddd7`: [session flush][compiler-flush].
- cass inspected at `3848af0` (post-v0.7.1 HEAD); Agent Trace at mirror `f7577a6`. The original `cursor/agent-trace` repository was unavailable during this research; do not treat mirror claims as current upstream support.
- [`open(2)` O_APPEND caveats][open], [ULID v2.1.2 monotonicity][ulid]. Local idempotency/locking recommendations are design proposals, not guarantees provided by those primitives.

[notebook]: https://github.com/prime-radiant-inc/engineering-notebook/tree/b0a34caa4b59e466652572310fd043a9548027c2
[notebook-code]: https://github.com/prime-radiant-inc/engineering-notebook/blob/b0a34caa4b59e466652572310fd043a9548027c2/src/summarize.ts
[notebook-ingest]: https://github.com/prime-radiant-inc/engineering-notebook/blob/b0a34caa4b59e466652572310fd043a9548027c2/src/ingest.ts
[notebook-skip]: https://github.com/prime-radiant-inc/engineering-notebook/blob/b0a34caa4b59e466652572310fd043a9548027c2/docs/plans/2026-03-07-persist-skipped-groups-design.md
[entire]: https://github.com/entireio/cli/tree/3dbdf8b83c39ef7ae73b9c613612f4c51aff3f1a
[entire-checkpoints]: https://github.com/entireio/cli/blob/3dbdf8b83c39ef7ae73b9c613612f4c51aff3f1a/docs/architecture/sessions-and-checkpoints.md
[entire-dispatch]: https://github.com/entireio/cli/blob/3dbdf8b83c39ef7ae73b9c613612f4c51aff3f1a/cmd/entire/cli/dispatch/generate.go
[entire-privacy]: https://github.com/entireio/cli/blob/3dbdf8b83c39ef7ae73b9c613612f4c51aff3f1a/docs/security-and-privacy.md
[claude-mem]: https://github.com/thedotmack/claude-mem/tree/3939fbb2debe73e74a295553636e776e110df90a
[mem-hooks]: https://github.com/thedotmack/claude-mem/blob/3939fbb2debe73e74a295553636e776e110df90a/plugin/hooks/hooks.json
[mem-policy]: https://github.com/thedotmack/claude-mem/blob/3939fbb2debe73e74a295553636e776e110df90a/plugin/modes/code.json
[mem-store]: https://github.com/thedotmack/claude-mem/blob/3939fbb2debe73e74a295553636e776e110df90a/src/services/sqlite/observations/store.ts
[compiler]: https://github.com/coleam00/claude-memory-compiler/tree/54eddd709e83d3be244e9c56c9fc3a6cf375d534
[compiler-flush]: https://github.com/coleam00/claude-memory-compiler/blob/54eddd709e83d3be244e9c56c9fc3a6cf375d534/scripts/flush.py
[cass]: https://github.com/Dicklesworthstone/coding_agent_session_search/tree/3848af01a4d39bf200fbe68bf5f7ff519c24d0d1
[trace]: https://github.com/superhq-dev/agent-trace/tree/f7577a60e2f720ac83fab825fa3903e932d90bcb
[claude-hooks]: https://code.claude.com/docs/en/hooks
[claude-sessions]: https://code.claude.com/docs/en/sessions
[claude-version]: https://github.com/anthropics/claude-code/blob/v2.1.224/CHANGELOG.md
[codex-hooks]: https://developers.openai.com/codex/hooks/
[codex-schemas]: https://github.com/openai/codex/tree/rust-v0.153.2/codex-rs/hooks/schema/generated
[codex-turn]: https://github.com/openai/codex/blob/rust-v0.153.2/codex-rs/core/src/session/turn.rs
[codex-history]: https://github.com/openai/codex/blob/rust-v0.153.2/codex-rs/app-server/README.md
[pi-source]: https://github.com/earendil-works/pi/tree/v0.85.1/packages/coding-agent
[open]: https://man7.org/linux/man-pages/man2/open.2.html
[ulid]: https://github.com/oklog/ulid/blob/v2.1.2/README.md
