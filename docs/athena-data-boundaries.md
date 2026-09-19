# Current curator data boundaries

This inventory covers Daylog's journal curator; follow-up work is tracked in
[Daylog #27](https://github.com/drdreo/daylog/issues/27). It is not a new permissions
engine, approval for real-data trials, or security certification. Local storage
does **not** mean local inference.

## Read, persist, submit, act

| Data class | Current local behavior | Model/provider boundary | Action authority |
| --- | --- | --- | --- |
| Explicit agent reports | `add` captures bounded private candidates and context; no model call at capture | Curation, including dry-run, submits selected queued reports to configured Pi provider/model; no per-project submission allowlist | Model proposes journal actions; Go validates and applies them. No task lifecycle authority |
| Native transcript excerpts | Opt-in approved source directory **and** project scopes; supported bounded assistant-text claims only; no hidden reasoning/raw tool streams | Selected excerpts may accompany curation; directory approval is not a guarantee redaction finds all secrets | Imported text and paths cannot grant new scopes or tools |
| Journal outcomes/editorial examples | Existing ledger and bounded `prefer` examples; not a general user profile | Bounded relevant outcomes/preferences can enter curation input | Human edits are protected; editorial examples do not authorize task adoption/completion |

The production default is currently `openai-codex/gpt-5.6-luna`, but machine
configuration can select a different provider/model. Inspect the invoked binary,
configuration and saved receipt/model identity; do not assume a checkout or
these docs describe a scheduler's installed binary. Existing Pi owns auth and
refresh; Daylog does not copy credentials or choose provider retention terms.
Provider-side logging/retention is outside Daylog's local deletion control;
verify current account/provider policy before sharing real personal data. No
claim of zero retention is made.

## What code enforces — and what it does not

The curator invokes Pi with tools/resources/session discovery disabled and
runs from an empty temporary cwd. Its Go validator rejects unsupported actions,
invented citations, obligation publication, stale/protected targets and invalid
structures. Model text cannot select the writer's timestamps or identities.
These CLI/process restrictions are not an OS sandbox against a compromised Pi
binary or another process with the same user's filesystem permissions. Source
strings such as `human:cli` are supplied conventions, not authenticated identities.

**Guidance, not semantic enforcement:** source text remains untrusted even if
requested by the user. The prompt instructs the model not to obey embedded
instructions or report false completion. Code cannot establish that arbitrary
prose is true, secret-free, or faithful to a reporter's intent. The structural
curator validator can accept a fabricated success or a sensitive string as
otherwise valid narrative text. Hashes establish content identity, not truth or
real-world human consent.

Select the least context needed **before** sending it. Do not let a retrieved
record, source date, fake admin approval or quoted command alter tool authority.
Review third-party skills/scripts and their actual executable behavior before
any separately approved activation; a skill's reassuring prompt is not a
permission boundary.

## Retention and deletion today

| Local surface | Retention/deletion behavior and limits |
| --- | --- |
| Journal/history/reports | Append-only history and private candidate text are retained. `dismiss` hides presentation, not deletion. `prune` does not erase report text or the ledger |
| Native evidence and copied plan excerpts | Explicit `prune` removes aged evidence only when all references are processed and no unfinished plan needs it; default configured age is 14 days. Pending/error/unfinished-plan evidence stays. Eligible copied plan evidence text is blanked; identities/hashes remain |
| Receipts, plans, preferences, diagnostics | Existing receipts/reasons, plan metadata/input reports and editorial preferences persist; bounded `explain`/diagnostic output may still be private. Provider stderr is withheld from persisted runner errors |
| Independent copies | Terminal/session copies, backups, filesystem snapshots and provider copies remain outside local pruning |

Local pruning is not a promise of forensic destruction on all hardware,
backups or external copies. Any real-data retention requirement must identify
those surfaces separately before capture; Daylog has no automatic erasure service.

## Concise audit, not hidden reasoning

Use existing records, not a new audit store:

1. **Source references:** candidate/evidence IDs, observation times and exact
   input hash.
2. **Observed facts:** what the source actually reports, including corrections,
   missing evidence and staleness.
3. **Applicable rule/preference:** the scoped human correction or saved curator
   policy/model, not an instruction embedded in retrieved prose.
4. **Proposed action and short reason:** a validated journal action in a curator
   plan.
5. **Approval/authority:** say absent when absent. Existing configured curation
   authorizes bounded journal publication, not resource actions. Human task
   adoption/completion remain explicit separate acts.
6. **Confirmed result:** a response is not execution. `explain` applied IDs plus
   ledger history can confirm recorded journal writes; a proposed plan alone
   cannot. A crash can append before acknowledgment. Event `recorded_at` is
   saved planning time on replay, not exact append wall time.

Keep that short factual trace; do not store private internal reasoning as an
audit explanation.

## Evidence and limits

- `internal/athena/assistant_boundaries_test.go`: current validator rejects
  injected privileged actions and invented evidence, accepts a useful failure
  report, and explicitly demonstrates its lack of semantic truth/secret checks.
- Curator tests cover human edits, replay, missing/pruned evidence and tool-call
  rejection. These are synthetic regressions, not live security or cross-platform
  runtime certification.

These boundaries do not add a security service, telemetry, connector, event
schema, policy engine or automatic learning.
