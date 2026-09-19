# Athena — daylog's gatekeeping subsystem

**Contract:** store/event/view v2; Athena policy `athena-v2.5`. See the [implementation plan](docs/gatekeeper-plan.md), [usage](README.md), and [native contracts](integrations/README.md). The original direct-agent architecture has been replaced; no legacy reader, publisher, migration, or synchronization protocol is supported.

## Ownership and boundaries

**Athena names the subsystem, not the AI model.** `internal/athena` owns the curation worker, policy/decision validation, durable plans, retries, and publication coordination. It uses `internal/capture` for intake/receipts and `internal/store` for the shared append-only ledger. Its pi runner reuses an existing configured harness/model; that runner is one replaceable component within Athena. `daylog curate --once` is the action that runs Athena.

```text
agent add / approved hooks / bounded recovery
                  │
        Go capture-time context + immutable candidate
                  │
       private atomic-file spool + processing receipts
                  │
       one OS-locked worker, related bounded batches
                  │
       isolated ephemeral pi process (no tools/resources)
                  │
       strict decisions → persisted complete Go plan
                  │
       shared store lock → revision/protection check → AppendOnce
                  │
       append-only events → effective entries → daily view
                  │
           terminal / three widgets

human notes/todos/corrections ────────► same locked event writer
GitHub poller ─► disposable PR snapshot ─► separate view collection
```

Working agents report facts and uncertainty, not journal materiality. Athena treats concrete reports as the journal's source, not claims requiring independent proof. It manages relevance, faithful wording, grouping, proposed corrections and skips; holds are for material contradictions or genuinely unclear results, not absent evidence attachments. Its model has no tools, queue authority, obligation lifecycle controls, or timestamp/identity authority. Athena's Go code owns metadata, validation, decision persistence, and replay through the shared ledger. Human source strings are an accident-prevention convention, not same-user authentication.

## Store layout

```text
<data>/store.json                 explicit version/protocol marker
       config.json                optional typed machine configuration
       installation.json          owned resources/commands for reversible setup
       store.lock                 OS-backed shared ledger lock
       events/YYYY-MM-DD.jsonl    canonical immutable history
       state/gh-prs.json           disposable GitHub current state
       capture/candidates/        immutable normalized revisions
               evidence/          separately retained bounded claim excerpts
               receipts/          operational state/disposition/acknowledgments
               plans/             validated decisions, operation IDs, exact input hash
               cursors/           native offsets, ancestry, original context, health
               preferences.json   private bounded examples
               budget.json        daily invocation accounting
               worker.lock        single Athena worker, independent of enqueue
               intake.lock        short atomic intake/retry guard
```

Initialization is serialized by an adjacent `<data>.init.lock`. An unversioned nonempty directory or unsupported marker is refused without changing its contents. Initialization writes a marker before optional directories so an interrupted accepted initialization is resumable. Private files are mode 0600 and owned directories 0700 on Unix; Windows uses a protected current-user/SYSTEM DACL. Existing unrelated parent directory permissions are not changed.

Atomic replacement writes a private temporary file, checks complete write, syncs, closes, installs atomically, and applies directory durability. New Unix directories sync their parents. Windows uses `MoveFileEx(REPLACE_EXISTING|WRITE_THROUGH)` for installation. Portable locks use `gofrs/flock`/OS locking and release on process death, not a stale PID convention. **Only macOS runtime behavior has been exercised here; Windows/Linux builds are not durability validation.** Local filesystems only; no network-filesystem or distributed exactly-once guarantee.

## Contracts

`internal/event` defines explicit event/entry, repository/context, target revision, and provenance types. Narrative presentation has a `tldr` headline plus optional plain-text `details` and `tags`; model headlines are at most 100 characters, details at most 2,000 characters, and there are at most three 24-character tags. Historical TLDRs and human notes keep the 280-character limit. Swift renders collapsed headlines, expandable details, and selected typed reference chips; the model never supplies executable layout or arbitrary link destinations. Amend/merge replace presentation together so human wording changes cannot retain stale model details. Unsupported kinds/versions and invalid refs fail. Corrective events carry explicit target IDs/revisions; narrative and todo lifecycle events have required occurrence and recording timestamps. Local sequence numbers are assigned under the ledger lock, avoiding clock-skew ordering of corrections. Files partition on `recorded_at`; the folded `display_at` uses occurrence time and its captured offset, or the completion event for closed todos. `filed_at` remains available. Bookkeeping never appears as another accomplishment.

`internal/capture` separates immutable candidates from atomic receipts:

- Candidate: source, kind, report/claim, typed refs, captured occurrence/time basis, original context, native identity/revision, episode, evidence IDs, terminal/completeness markers, origin.
- Receipt: `pending|processing|processed|error`, independently `skip|hold|outcome`, input hash, evidence IDs, model/policy, plan, reason, attempts/backoff, applied IDs.
- Model output has no event/publication IDs, dates, source-authority fields, or arbitrary metadata. Go constructs those from cited inputs.

Unkeyed CLI reports remain independent. Stable request keys reject conflicting text/type/refs. Native revisions deduplicate hooks and reconciliation where identities align. New native content is a new candidate. A copied pi node uses original cwd/node/time/revision identity, so copied ancestry is not newly captured evidence. Exact explicit task identity or native session/request linkage defines an episode; repository/session alone does not. Candidates are batched by exact episode and captured occurrence day. Repository/refs/recent time retrieve context, not automatic merges. For same-day narrative updates, an exact shared typed PR/issue ref can additionally link an existing outcome across agent turns. Go supplies `editable_targets` with legal IDs/revisions; the model decides whether the linked reports actually describe the same outcome. Same repository/session/topic alone never authorizes an edit. Unknown repository identities match only a known identical worktree/cwd, never each other merely because both are empty.

## Athena's curation and replay protocol

1. Acquire worker lock. Normal runs recover saved ready live plans and finalize legacy shadow receipts without publishing them. Incompatible plans are retained as stopped, their reports leave processing for explicit retry, and unrelated work continues. Already-applied operations are recovered by publication key before acknowledgment. An explicit `--dry-run` skips recovery, claims and receipt changes; it returns validated preview actions instead of saving plans or publishing. Dry runs still reserve model-call budget, and leave reports eligible for normal evaluation.
2. Reclaim interrupted processing claims without a persisted plan. Persist a new claim identity before model execution; a completed plan is matched to that exact claim, not just matching text.
3. Select quiet/max-wait bounded episode/day batches. Held/skipped material is reconsidered only with new same-episode evidence or explicit human retry. Include relevant active, pinned, dismissed and merged outcomes and bounded preference examples.
4. Queued reports from any project are eligible for model curation; only the Daylog data directory remains excluded. Reserve the invocation budget durably before calling pi. Model failures use bounded attempt/backoff receipts; they never enable another publisher.
5. Parse the terminal assistant `message_end` from pi JSON events, allowing Pi's intermediate failed turns followed by successful retries; reject terminal failures, unfinished retries, any tool use, oversized data, trailing prose, duplicate/unknown JSON fields, invented candidates/evidence/refs, conflicting actions, invalid targets, and human-protected edits. Every input must be accounted for; one report may support several distinct outcomes.
6. Go assigns stable plan/operation/publication IDs and preserves primary reporter plus all contributor sources. Validate the complete plan before durable installation. Later-day milestones cannot amend yesterday's outcome.
7. Under the shared store lock, scan the ledger without ignoring corruption; find an existing publication key **before** checking stale revisions; otherwise check current targets/protections, append completely, sync, and release. Acknowledgment follows append. A crash in that gap finds the existing event on replay; a mid-plan crash resumes remaining operations.
8. A stale target conflict persists a stopped/error plan. Applied operations are neither duplicated nor undone. Explicit retry requests new bounded evaluation against current state. Shadow decisions are not a deferred automatic publication queue.

The v2.5 editorial policy keeps the v2.4 preference for one evolving same-day outcome over one row per report, adding a concise, warm voice with occasional dry wit. Personality changes wording, not the decision schema, permissions, or factual standards. Related implementation, review fixes, and scoped verification normally amend that outcome; duplicate progress and review housekeeping skip. Independent outcomes and material unresolved risks remain visible. Details preserve useful prior substance and final verification gaps without repeated test counts or implementation trivia; no daily entry quota is imposed. This is prospective curation, not an automatic historical cleanup job.

Human amendments pin wording, dismissal persists until human restoration, and merging is one event affecting all targets. Automated corrections cannot modify obligations or human entries. Exact already-protected candidate input cannot be republished by a forced replay. This protects mechanical replay, not an assertion that paraphrased independent reports can never be semantically duplicated.

The append/dedup scan refuses malformed complete/middle records, duplicate keys/IDs/sequences, and torn tails. Read views currently fail explicitly rather than silently omit history. `repair-tail --confirm` backs up all original bytes and removes only an unterminated final record; it does not auto-repair middle records.

## pi isolation

`internal/athena.PiRunner` invokes an argument vector, never shell-built evidence. It uses explicit provider/model/system policy, empty append policy, JSON/print mode, no session, tools, skills, extensions, themes, prompt templates, context discovery, project trust or startup network operations. Global `APPEND_SYSTEM.md` needs its own explicit override: `--no-context-files` alone is insufficient.

The worker runs one normal `pi --print --mode json --model ...` command from an empty temporary working directory. Pi uses its existing agent directory, model catalog, authentication and credential refresh locking. Daylog does not read, export, copy, or reconstruct credentials, and does not create a second Pi configuration. CLI flags disable tools and resource discovery; normal Pi settings (including retries) otherwise apply within Daylog's overall subprocess deadline. Provider stderr is withheld from persisted errors. The empty working directory is removed on exit.

Provider/model never silently falls back. `runner.binary`, `runner.agent_dir`, `runner.path`, input/output limits, timeout, attempt limits, and call budgets are persisted machine settings. No private data is sent by `add`, `today`, build, default tests, or installation. Dry runs still submit queued reports/evidence; preview is not an offline mode.

## Capture and privacy

Adapters inspect verified terminal payloads and enqueue neutral JSON, not journal entries. SessionStart captures original git context while available. Native recovery consumes approved regular JSONL files incrementally, rotating a capped file list and preserving incomplete trailing bytes for a later wakeup. Unsupported versions, rewrites, missing metadata, scope failures and bound exhaustion appear in cursor health. It never follows arbitrary transcript-provided paths or reconstructs context from the worker cwd.

Native supporting excerpts are explicitly assistant **claims**, not execution proof. No hidden reasoning, raw tool streams, credentials files, `.env` reads, repository crawling or model-directed evidence gathering. Sensitive-line redaction is deliberately described as best-effort. Native-boundary candidate text is compact; sensitive excerpt text is separately retained, including its bounded copied Athena-plan input. `prune` redacts/removes only aged evidence whose every referencing candidate is processed and whose plans cannot still apply. Receipts, hashes and report text remain for identity/diagnostics. Missing pruned evidence is visible on a requested retry.

Scheduled transcript recovery requires explicit capture scopes. Model submission has no separate project allowlist: queued reports and bounded evidence are sent to the configured provider during curation. The legacy `cloud_projects` key is accepted for config compatibility, ignored, and dropped on save. The data directory and internally tagged Athena process are excluded; the daylog source repository is not. Native session existence is not proof of task boundaries or full capture. Unrecorded hard crashes, some reference-based Codex histories, unsupported Claude metadata shapes, and unknown child lineage are honest coverage gaps.

## Validation and cutover

Tests use fake runner/clock seams, sanitized native/golden fixtures, real concurrent CLI processes, killed-worker replay, corruption/repair, mid-plan failure, stale human edits, retention, scratch resource installation, and a mock pi API. Three widget timestamp fields change in the same contract. No live inference is required; the synthetic Luna smoke test is opt-in.

New configurations use live curation; explicit `curate --once --dry-run` is the only new preview path. Old shadow configurations require explicit `setup --mode live`, and historical shadow plans can never auto-publish. Scheduler activation remains an explicit setup action. Pause by stopping the worker; keep intake durable. Do not roll an old binary/schema back into this active store, import old records, or reset it destructively.
