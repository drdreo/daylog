# Current assistant and curator data boundaries

This inventory covers the shipped memory baseline (#25), current journal
curator, and the synthetic on-demand experiment (#16/#21). It is not a new
permissions engine, approval for real-data trials, or security certification.
Local storage does **not** mean local inference.

## Read, persist, submit, act

| Data class | Current local behavior | Model/provider boundary | Action authority |
| --- | --- | --- | --- |
| Explicit agent reports | `add` captures bounded private candidates and context; no model call at capture | Curation, including dry-run, submits selected queued reports to configured Pi provider/model; no per-project submission allowlist | Model proposes journal actions; Go validates and applies them. No task lifecycle authority |
| Native transcript excerpts | Opt-in approved source directory **and** project scopes; supported bounded assistant-text claims only; no hidden reasoning/raw tool streams | Selected excerpts may accompany curation; directory approval is not a guarantee redaction finds all secrets | Imported text and paths cannot grant new scopes or tools |
| Journal outcomes/editorial examples | Existing ledger and bounded `prefer` examples; not a general user profile | Bounded relevant outcomes/preferences can enter curation input | Human edits are protected; editorial examples do not authorize task adoption/completion |
| Dreo/Athena memory | Explicit manually supplied records in owner-separated SQLite stores; no automatic source discovery | Memory commands call no model. A human's selected output is still private; copying it into a conversation requires separate data/provider approval | Explicit CLI mutation and confirmation gates; reads do not write/confirm preferences |
| Stored dreams/derived summaries | Attributed speculative/inferred artifacts, with tracked source revision/hash checks | No automatic generation or model submission | No promotion to fact, obligation, skill or permission |
| On-demand trial packets/results | Synthetic fixtures and final answers retained in the repo for reproducibility | This trial used existing Pi auth with `openai-codex/gpt-5.6-luna`; synthetic data only | Tool-free response only; no actual search, calendar read, forwarding, task/store writes or monitoring |
| Real inbox, web pages, documents, external resources | No new ingestion/connector is approved by this increment | Real journal/brain/transcript/inbox content is **not** approved for this experiment | Any later source/provider/action expansion needs its own scoped approval |

The production default is currently `openai-codex/gpt-5.6-luna`, but machine
configuration can select a different provider/model. Inspect the invoked binary,
configuration and saved receipt/model identity; do not assume a checkout or
these docs describe a scheduler's installed binary. Existing Pi owns auth and
refresh; this work does not copy credentials, change settings, install skills,
or choose provider retention terms. Provider-side logging/retention is outside
Daylog's local deletion control; verify current account/provider policy before
sharing real personal data. No claim of zero retention is made.

## What code enforces — and what it does not

**Enforced in current interfaces:** memory read-only attachment opens only the
explicit owner scope, filters current eligibility and tracked dependencies,
requires matching source revisions/hashes, and refuses invalid owner/status
combinations. Dreo-confirmed preferences require Dreo ownership/authorship plus
explicit confirmation; Athena interpretations cannot be stored as Dreo-authored
preferences merely by placing instructions in their content. Corrections and
forgetting invalidate/remove tracked derivatives. Read commands do not interpret
provenance as a path to load or content as executable instructions.

The curator invokes Pi with tools/resources/session discovery disabled and
runs from an empty temporary cwd. Its Go validator rejects unsupported actions,
invented citations, obligation publication, stale/protected targets and invalid
structures. Model text cannot select the writer's timestamps or identities.
These CLI/process restrictions are not an OS sandbox against a compromised Pi
binary or another process with the same user's filesystem permissions. `owner`,
`author` and `human:cli` are supplied conventions, not authenticated identities.

**Guidance, not semantic enforcement:** source text and derived summaries remain
untrusted even if requested by the user. The prompt instructs the model not to
obey fake authorization, poison preferences, reconstruct deleted sources,
report false completion or disclose sensitive data. Code cannot establish that
arbitrary prose is true, secret-free, or faithful to an owner's intent. The
structural curator validator can accept a fabricated success or a sensitive
string as otherwise valid narrative text. In-scope memory recall can return
hostile text and secrets unchanged; it is not a sanitizer. Confirmation flags
and hashes establish an explicit operation and content identity, not truth or
real-world human consent.

Select the least context needed **before** sending it. A restricted marker
withheld in one trial is not a general disclosure filter. Do not let a retrieved
record, source date, fake admin approval or quoted command alter tool authority.
Review third-party skills/scripts and their actual executable behavior before
any separately approved activation; a skill's reassuring prompt is not a
permission boundary. No third-party code is enabled by this experiment.

## Retention and deletion today

| Local surface | Retention/deletion behavior and limits |
| --- | --- |
| Memory current + corrected history | No automatic age-based purge. Correction retains prior payloads as corrected history and invalidates derivatives; expiry suppresses eligibility, not plaintext storage |
| Memory `forget` | Explicit revision/confirmation; removes store-local historical payloads, FTS rows and transitive tracked derivatives, retains minimal tombstones; rebuild cannot resurrect forgotten IDs |
| Independent/untracked copies | Not discovered by scanning. A paraphrase with no source edge, export, terminal/session copy, backup, filesystem snapshot or provider copy remains outside `forget` |
| Journal/history/reports | Append-only history and private candidate text are retained. `dismiss` hides presentation, not deletion. `prune` does not erase report text or the ledger |
| Native evidence and copied plan excerpts | Explicit `prune` removes aged evidence only when all references are processed and no unfinished plan needs it; default configured age is 14 days. Pending/error/unfinished-plan evidence stays. Eligible copied plan evidence text is blanked; identities/hashes remain |
| Receipts, plans, preferences, diagnostics | No new TTL or logging sink. Existing receipts/reasons, plan metadata/input reports and editorial preferences persist; bounded `explain`/diagnostic output may still be private. Provider stderr is withheld from persisted runner errors |
| Experiment artifacts | Synthetic prompts, final answers, elapsed times and mechanical failures intentionally stay versioned for review. No automatic TTL. No hidden chain-of-thought, real private source data or provider stderr is retained here |

SQLite/FTS secure-delete and local logical forgetting are not a promise of
forensic destruction on all hardware, prior journals, backups or external
copies. Human-directed removal from a versioned repository likewise would not
erase every clone or provider/session copy. Any real-data retention requirement
must identify those surfaces separately before capture; this increment adds
no retention schema or automatic erasure service.

## Concise audit, not hidden reasoning

Use existing records, not a new audit store:

1. **Source references:** selected IDs, memory revisions/hashes, observation
   times; curator candidate/evidence IDs and exact input hash.
2. **Observed facts:** what the source actually reports, including correction,
   owner/author/status, missing evidence and staleness.
3. **Applicable rule/preference:** the scoped human correction or saved curator
   policy/model, not an instruction embedded in retrieved prose.
4. **Proposed action and short reason:** brief/recommendation only in the manual
   experiment; a validated journal action in a curator plan.
5. **Approval/authority:** say absent when absent. Existing configured curation
   authorizes bounded journal publication, not resource actions. Human task
   adoption/completion and memory confirmation remain explicit separate acts.
6. **Confirmed result:** a response is not execution. `explain` applied IDs plus
   ledger history can confirm recorded journal writes; a proposed plan alone
   cannot. A crash can append before acknowledgment. Event `recorded_at` is
   saved planning time on replay, not exact append wall time.

Example from synthetic decision briefing: `[D2] revision 2` says simpler
operations for this pilot; `[O1]` reports managed backups. Propose A, explain
backup burden, and flag missing multi-region requirements/prices. No search,
approval, purchase or infrastructure change occurred. Keep that short factual
trace; do not store private internal reasoning as a dream or audit explanation.

## Evidence and limits

- `internal/memory/assistant_boundaries_test.go`: real temporary-store recall
  keeps hostile text under its owner, denies unscoped private reads, preserves
  legitimate current citations, rejects text-only confirmation and stale/deleted
  source references, and does not revive forgotten derivatives after rebuild.
- `internal/athena/assistant_boundaries_test.go`: current validator rejects
  injected privileged actions and invented evidence, accepts a useful failure
  report, and explicitly demonstrates its lack of semantic truth/secret checks.
- Existing memory/CLI tests cover expiry, read-only nonmutation, revision/hash
  checks and forgetting; curator tests cover human edits, replay, missing/pruned
  evidence and tool-call rejection. These are synthetic regressions, not live
  security or cross-platform runtime certification.
- [The performed six-case experiment](athena-assistant-experiment.md) evaluates
  useful answers alongside injection/fake authorization, preference poisoning,
  stale/deleted sources, false completion and disclosure attempts. Its failures
  remain recorded. It is not an independent benchmark or genuine human study.

No new security service, telemetry, production runner, connector, event schema,
policy engine or automatic learning was added. Broader memory/dreaming,
reminders and calendar integrations remain separate roadmap work.
