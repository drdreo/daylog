# daylog

A local journal with **private agent reports, Athena gatekeeping, human notes/todos, and GitHub snapshots**.

**Athena is daylog's gatekeeping subsystem**, implemented in `internal/athena`—not a model name or persona. It coordinates bounded curation, policy checks, durable decisions, retries, and replay-safe publication. Its configured pi model proposes outcomes; Athena's Go code validates and applies them through daylog's shared store. Capture feeds its private queue; human notes and todos remain outside automated curation.

This is a clean-break **store/event/view v2** implementation of the [gatekeeper plan](docs/gatekeeper-plan.md). There are no legacy readers, direct-agent mode, migrations, or history import. Existing unversioned/unsupported data is refused, never converted or deleted.

## Build and start fresh

Go 1.24+:

```sh
./install.sh                         # binary only; no hooks, store, or network calls
export DAYLOG_DIR="$HOME/daylog-v2"  # choose a NEW private directory
# --data-dir /absolute/path overrides DAYLOG_DIR for any command
daylog init
daylog add "A human quick note"
daylog today --json
daylog version
```

Do not point the new binary at old data or mix binaries against one store. Keep archived history outside the active directory. Nothing needs pi, credentials, or network access to capture reports, write human notes, or read the journal.

## Report, don't publish

Set `DAYLOG_SOURCE=agent:pi`, `agent:claude`, or `agent:codex` in the corresponding harness launch environment. Without an override the source is `human:cli`; source strings prevent attribution accidents, not access by another same-user process.

```sh
daylog add --type work --ref '#142' \
  "Implemented refresh locking. The regression test passed locally; deployment not attempted."
# agent source: queued <candidate-id>
# human source: logged <entry-id>
```

All agent `work`, `sidequest`, and `note` reports **always enqueue**. The worker being absent, broken, stopped, or being previewed never enables direct publication. Reports are brief handovers: what changed or was learned, checks actually performed, and important limitations. They are bounded at 16 KiB; Athena writes a headline of at most 100 characters, optional expandable details of at most 2,000 characters, and up to three optional tags. Human notes and historical TLDRs retain their 280-character limit. No fixed form, proof attachments, or extra verification work are required just to report. `--idempotency-key REQUEST` deduplicates identical keyed reports; ordinary unkeyed CLI calls are retained independently.

Athena's `athena-v2.5` editorial policy keeps the journal at outcome level: related same-day fixes, follow-ups, and verification normally update one concise entry rather than generate a row per agent turn. Same-day outcomes can be linked by an exact shared PR/issue ref across turns; the model receives an explicit list of editable targets. Mere repository/session similarity cannot authorize an edit. Human-edited entries remain protected, later-day milestones stay separate, and verification of an earlier build never certifies later changes. Routine review housekeeping, unchanged tests, and redundant progress skip; meaningful independent outcomes and unresolved risks remain visible. No entry-count quota or automatic cleanup of existing history is introduced.

Athena's voice is warm, concise, and quietly sassy: simplify complicated work and allow an occasional dry nudge without obscuring facts or limitations. Plain language beats forced jokes. The [small personality eval loop](docs/athena-evals.md) uses eight synthetic cases, deterministic checks, and a separate tool-free Pi judge for fidelity, editorial judgment, simplicity, and voice. Judge verdicts include reasons and quotes; they are fallible assessments, not factual proof. This adds no chat interface or personal-memory storage.

`queued` means durably captured, **not visible in the journal**. Athena may rewrite, combine, amend, merge, skip, or hold reports. Concrete agent reports are sufficient source material: Athena curates them rather than requiring independent proof. It preserves reported uncertainty and holds only genuinely unclear or contradictory results. See the single [reporting skill](skills/daylog/SKILL.md) and [instruction block](docs/AGENT_INSTRUCTIONS.md).

Refs are host-qualified: `gh:pr:github.com/owner/repo#142`, `linear:ABC-123`, or `jira:PROJ-45`. `#142` expands using capture-time repository host/path. Context has `context.repository.{host,path}`, cwd, worktree, branch, HEAD, and available session/turn/task/parent identifiers. Set `DAYLOG_TASK_ID` to an explicit shared task identity when multiple agents really are collaborating; repository/session alone is not a task key.

## Human controls

```sh
daylog add --type todo "Check the release plan"
daylog accept ENTRY                       # adopt an agent proposal
daylog decline ENTRY --note "Not needed"
daylog done ENTRY --note "Verified locally"
daylog reopen ENTRY                         # undo completion; human only
daylog amend ENTRY "Corrected wording"    # pins wording against automation
daylog amend ENTRY "Reclassified outcome" --type work
daylog dismiss ENTRY --reason too-minor
daylog restore ENTRY
daylog merge PRIMARY DUPLICATE "Combined wording"
daylog prefer ENTRY skip --reason "Routine operational noise"
```

These controls are human-only. In a shell inheriting agent identity, explicitly use `--source human:cli` for commands that offer it, or unset `DAYLOG_SOURCE`. Agent todos are deliberate proposals, never Athena-created obligations; adoption precedes completion. Corrections resolve **folded current wording/revision**, not the original text. Merge is one atomic logical event, requires the same project/day, and leaves contributor identities inspectable.

## Configure Athena deliberately

New configurations are **live by default**: the worker curates queued reports from any project and publishes useful outcomes. There is no shadow rollout to complete. Use an explicit **dry run** when you want to inspect proposed decisions without processing reports.

```sh
daylog setup --pi /absolute/path/to/pi \
  --pi-agent-dir "$HOME/.pi/agent"

daylog curate --once --dry-run  # optional preview JSON; reports stay pending
daylog curate --once            # fresh evaluation and publication
daylog explain CANDIDATE
```

The journal and queue are local, but curation sends queued reports and bounded evidence to your configured Pi model/provider, **including a dry run**. There is no per-project submission allowlist. The legacy `cloud_projects` config field is accepted but ignored and omitted when config is saved. Native transcript scanning still requires explicit capture scopes; submitting a report does not authorize scanning sessions or uploading a repository.

Configuration is versioned `<data>/config.json`. `setup` persists executable paths and PATH for scheduled runs. Athena's default model is `openai-codex/gpt-5.6-luna` (catalog/CLI checked against pi 0.85.1). Provider/model are configurable; no provider fallback exists. Daylog reuses your installed pi harness, its configured model catalog, and existing authentication; it does not require a separate paid AI service or new API subscription. Authentication/refresh belongs to installed pi. If that harness/model is unavailable, Athena reports a clear error and retains the reports in the queue—there is no paid-provider fallback. The worker invokes Pi directly with its normal configuration and authentication—no credential copying or custom token handling. CLI flags disable tools, extensions, skills, prompt templates and context discovery, including global `APPEND_SYSTEM.md`. Normal Pi retry settings apply within Daylog's subprocess deadline.

Initial bounds: 60-second episode quiet period, 300-second maximum wait, 8 inputs per batch, 2 model invocations per run, 40 per day, 120-second overall subprocess deadline, 64 KiB input, 1 MiB event-stream output, and 3 failed attempts with backoff. These are configurable limits, **not measured latency/cost promises**. A call cap is not a dollar budget.

For an opt-in editorial check using only synthetic reports and temporary stores (not your journal), run:

```sh
DAYLOG_LUNA_SMOKE=1 DAYLOG_SMOKE_PI="$HOME/.local/bin/pi" \
  DAYLOG_SMOKE_PI_AGENT_DIR="$HOME/.pi/agent" \
  go test ./internal/athena -run TestOptInEditorialConsolidation -v
```

These checks exercise model choices about updates, noise, batching, independent risks, and stale verification. Ordinary tests cover deterministic linkage, day boundaries, and human protection without inference. A passing synthetic check is not a guarantee of every future editorial judgment.

Dry runs return `preview` actions without changing candidates, receipts, saved plans, or the journal. They still send queued inputs to Pi and count against the model-call budget. The next normal run evaluates pending reports afresh; no retry is needed after a dry run.

Reports already in error from the removed project gate still need `daylog queue retry CANDIDATE` to request fresh evaluation; removing the gate does not silently reset exhausted retries or replay previously held/skipped reports.

For old installations only, an existing `mode: shadow` configuration stays paused until `daylog setup --mode live`. Old persisted shadow decisions are **never auto-applied**; use `daylog queue retry CANDIDATE` to explicitly request fresh evaluation against current outcomes/pins/dismissals. A crashed live plan resumes its saved operations without another model call. Stale targets or a policy change stop the affected plan and make its reports explicitly retryable without blocking unrelated reports. The plan and already-applied operations are retained, including writes recovered from an append-before-ack crash.

## Optional fallback capture and recovery

Adapters enqueue only. They never request another agent turn or run Athena's curation worker. Verified contract targets and sanitized fixtures:

| Harness | Hook boundaries | Native recovery |
|---|---|---|
| pi 0.85.1 | SessionStart, `agent_settled` | Session v3 JSONL, native node IDs, request ancestry |
| Claude Code 2.1.224 | SessionStart, Stop, SubagentStop, StopFailure | Version-tagged user/assistant JSONL records |
| Codex 0.153.2 | SessionStart, Stop, SubagentStop, Interrupt | Version-tagged rollout session metadata, turn context, terminal events |

```sh
daylog setup --approve-project /absolute/project \
  --capture-scope pi=/absolute/approved/pi-session-directory \
  --capture-scope claude=/absolute/approved/claude-project-directory \
  --capture-scope codex=/absolute/approved/codex-session-directory \
  --install-adapters --install-skills

daylog reconcile
```

Capture approval is required **both for the native session directory and for the original project**. `--approve-project` supplies the project roots for the specified `--capture-scope` entries; it is not needed for curation. Capture scopes remain an explicit restriction on which transcripts can be read. Once captured, reports are eligible for curation without another project gate. Stop the worker if you want intake without model submission.

Setup preserves unrelated hook/settings entries and never grants harness trust. Review Claude/Codex hook trust yourself; restart/reload pi. Installed hook definitions are pinned to the listed contracts: revalidate adapters when upgrading harnesses. Unsupported native versions, partial tails, replaced/truncated files, scope failures, and cursor bounds are reported, not called complete coverage.

Recovery reads at most 64 files per wakeup (rotating), 2 MiB per file, and 128 candidate records per file. It keeps per-file offsets and native ancestry rather than resummarizing whole histories. Original context comes from native metadata or a SessionStart capture, **never the worker's git checkout**. Deleted worktrees remain identifiable to the extent metadata was captured before deletion.

Only bounded assistant-text excerpts are copied as **claims**, not proof of execution. Hidden reasoning, images, arbitrary repository files, and raw tool output are excluded. Sensitive-line redaction is best-effort and can miss secrets: directory approval is still required. Excerpts live separately from compact native-boundary candidates. `daylog prune` removes aged processed excerpts and their copied plan-input text after the configured retention period (default 14 days); pending/error/unfinished-plan evidence is retained. Report text and compact receipts remain; a retry after evidence pruning fails visibly rather than inventing evidence.

See [capture contracts and limits](integrations/README.md). Tests verify fixtures and the pi extension through a mock API, **not live end-to-end execution of all harnesses**. Ephemeral sessions can be captured while alive; unsaved hard-crash evidence cannot be recovered. Unknown child lineage remains unknown. Codex logical history references and Claude files without supported session metadata are diagnosed as coverage gaps rather than followed outside approved scopes.

## Schedule without a daemon

```sh
daylog setup --schedule              # write resources only
daylog setup --schedule --activate   # explicit scheduler registration/start
```

The generated one-shot job runs `daylog --data-dir ABSOLUTE tick`, which runs reconciliation and curation alongside independently throttled GitHub snapshot polling. Platform resources are launchd on macOS, systemd user units on Linux, and Task Scheduler XML on Windows. No scheduler or integrations are installed by `install.sh` alone.

- [macOS launchd](docs/launchd/README.md)
- [Linux systemd](docs/systemd/README.md)
- [Windows setup](docs/windows-setup.md)

Pause by stopping the scheduled job. For a one-off preview, use `daylog curate --once --dry-run`. Intake continues to queue, never flushes directly to the ledger. Fix the problem, inspect `explain`, and resume. Stop/disable jobs **before** `daylog setup --uninstall-resources`; only unchanged owned files and the exact registered hook commands are removed. Store/config/history remain. Modified resources are retained with an error.

## Read and diagnose

```sh
daylog today [YYYY-MM-DD] --json
daylog days [YYYY-MM] --json          # nonempty journal days and entry counts
daylog render [YYYY-MM-DD]
daylog status --json
daylog doctor --check-model          # catalog check, no inference
daylog explain ENTRY_OR_CANDIDATE
daylog repair-tail YYYY-MM-DD --confirm
```

Every raw event has `recorded_at` and `occurred_at`; files are partitioned by recording day. The view's required `display_at` drives sorting/display: captured occurrence day for narrative, completion occurrence time for completed todos. `filed_at` preserves the original todo filing time. Amendments do not move work to the correction day. There are no `ts`/`done_ts` compatibility fields.

`days --json` returns `{version: 2, month: "YYYY-MM", days: [{date: "YYYY-MM-DD", count: N}]}` for the requested month (current month by default). It reads an existing store once and counts the same effective entries as `today`: narrative plus completed todos, excluding open obligations, suppressed entries, queue items, and PR snapshots. Dates retain captured display components; amendments do not add a dot on the correction day. Empty months return `days: []`.

Ledger corruption blocks publication, including dedup retries. `repair-tail` only removes an unterminated final line and first saves **all original bytes** beside the day file. Malformed complete/middle records require explicit inspection; no reader silently skips them. Interrupted atomic-file temporaries are diagnostic artifacts, never completed candidates.

## GitHub and widgets

```sh
daylog poll gh                         # requires authenticated gh CLI
daylog poll gh --owner 'myorg,!oldorg'
```

With the scheduled worker active, GitHub PRs refresh automatically every **5 minutes**, even while the macOS panel is closed. Set `github_poll_seconds` in `<data>/config.json` to change the interval (60–86400 seconds), or `0` to disable automatic polling. Existing configs default to 300 seconds. Failed attempts are throttled too and retain the previous snapshot timestamp; each scheduled fetch is bounded to two minutes. Manual `poll gh` and the app's refresh button remain immediate. Scheduled runs use the PATH captured by `setup` to locate `gh` and its existing authentication, independently of Athena's model budget or queue state.

Configure machine scope with `github_owners` in config, `DAYLOG_GH_OWNERS`, or `--owner` (flag wins). Open PRs remain a **separate current-state snapshot**, never journal work. Snapshot PRs use their own `repo` display label and provider URLs; narrative context uses structured repository identity.

The desktop consumers read only `today --json`, using `display_at` and `filed_at`:

- [Native macOS app (SwiftUI POC)](macos-app/README.md) — no SwiftBar required
- [Omarchy](omarchy-plugin/README.md)
- [Windows tray](windows-plugin/README.md)
- [SwiftBar (optional legacy widget)](swiftbar-plugin/README.md)

Install/update consumers together with this binary; keep the selected data directory consistent. No consumer reads the queue or Athena's decision output.

## Validation

```sh
go test ./...
go vet ./...
swift run --package-path macos-app DaylogCoreChecks  # macOS only
go test -race ./...
node --experimental-strip-types --test integrations/pi/daylog.test.mjs integrations/consumers.test.mjs swiftbar-plugin/daylog.test.mjs
GOOS=linux GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
```

Tests include concurrent producer **processes**, a killed worker after append/before acknowledgment, mid-plan replay, human edits during inference, hostile output rejection, private-file permissions, occurrence-day folding, scoped native fixtures, scratch-HOME installation, and scheduler escaping. All test data is temporary; default tests require no model credentials. Cross-compilation is **not** Linux/Windows runtime validation, and mocked widget/adapters do not establish live desktop behavior. macOS has also been exercised with real Pi/Luna calls, nine backlog publications, Swift CLI decoding, and an activated LaunchAgent that successfully retried a rejected model decision. This does not establish live native-hook capture coverage or Linux/Windows runtime behavior.

Optional synthetic-only smoke test using your existing configured pi model (not run by default). This invokes the model and uses whatever quota or billing already applies to that setup; no separate paid service is required:

```sh
DAYLOG_LUNA_SMOKE=1 DAYLOG_SMOKE_PI=/absolute/pi \
DAYLOG_SMOKE_PI_AGENT_DIR=/absolute/pi-agent-dir \
go test ./internal/athena -run TestOptInLunaSmoke -v
```

The optional Luna checks exercise eight synthetic cases: a report without proof attachments, partial work, an attempted fix, routine noise, unclear and contradictory results, an overbroad claim, and a detailed handover that needs a short headline plus expandable details. Dispositions are checked automatically; emitted wording still needs human review. These small samples do not establish a general quality/recall or latency guarantee. Dry-run evaluation is optional, not an installation prerequisite.
