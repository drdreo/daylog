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

All agent `work`, `sidequest`, and `note` reports **always enqueue**. The worker being absent, broken, stopped, or in shadow never enables direct publication. Reports are bounded at 16 KiB; published wording is at most 280 characters. `--idempotency-key REQUEST` deduplicates identical keyed reports; ordinary unkeyed CLI calls are retained independently.

`queued` means durably captured, **not visible in the journal**. Athena may rewrite, combine, amend, merge, skip, or hold reports. See the single [reporting skill](skills/daylog/SKILL.md) and [instruction block](docs/AGENT_INSTRUCTIONS.md).

Refs are host-qualified: `gh:pr:github.com/owner/repo#142`, `linear:ABC-123`, or `jira:PROJ-45`. `#142` expands using capture-time repository host/path. Context has `context.repository.{host,path}`, cwd, worktree, branch, HEAD, and available session/turn/task/parent identifiers. Set `DAYLOG_TASK_ID` to an explicit shared task identity when multiple agents really are collaborating; repository/session alone is not a task key.

## Human controls

```sh
daylog add --type todo "Check the release plan"
daylog accept ENTRY                       # adopt an agent proposal
daylog decline ENTRY --note "Not needed"
daylog done ENTRY --note "Verified locally"
daylog amend ENTRY "Corrected wording"    # pins wording against automation
daylog amend ENTRY "Reclassified outcome" --type work
daylog dismiss ENTRY --reason too-minor
daylog restore ENTRY
daylog merge PRIMARY DUPLICATE "Combined wording"
daylog prefer ENTRY skip --reason "Routine operational noise"
```

These controls are human-only. In a shell inheriting agent identity, explicitly use `--source human:cli` for commands that offer it, or unset `DAYLOG_SOURCE`. Agent todos are deliberate proposals, never Athena-created obligations; adoption precedes completion. Corrections resolve **folded current wording/revision**, not the original text. Merge is one atomic logical event, requires the same project/day, and leaves contributor identities inspectable.

## Configure Athena deliberately

Default mode is **shadow**: decisions are durable but nothing is published. **Live** is explicit machine configuration. Both use the same intake.

```sh
daylog setup --pi /absolute/path/to/pi \
  --pi-agent-dir "$HOME/.pi/agent" \
  --approve-project /absolute/path/to/project

daylog curate --once --shadow
daylog queue list --status processed
daylog explain CANDIDATE
# After inspecting a small sample yourself:
daylog setup --mode live
daylog curate --once
```

`--approve-project` authorizes sending reports/evidence from that captured directory to the configured model, **including in shadow mode**. It does not scan sessions or install anything by itself. Approval applies to descendants; approve narrow worktree/project roots, not your entire home directory.

Configuration is versioned `<data>/config.json`. `setup` persists executable paths and PATH for scheduled runs. Athena's default model is `openai-codex/gpt-5.6-luna` (catalog/CLI checked against pi 0.85.1). Provider/model are configurable; no provider fallback exists. `runner.credential_type` is `oauth` by default; use `api_key` for an API-key provider. Daylog reuses your installed pi harness, its configured model catalog, and existing authentication; it does not require a separate paid AI service or new API subscription. Authentication/refresh belongs to installed pi. If that harness/model is unavailable, Athena reports a clear error and retains the reports in the queue—there is no paid-provider fallback. Worker-private settings disable discovery, retries, tools, compaction, prompts and extensions; only the configured provider's short-lived credential and catalog are copied into the private temporary runtime. Global `APPEND_SYSTEM.md` is explicitly disabled as well as context files.

Initial bounds: 60-second episode quiet period, 300-second maximum wait, 8 inputs per batch, 2 model invocations per run, 40 per day, 120-second overall subprocess/auth deadline, 64 KiB input, 1 MiB event-stream output, and 3 failed attempts with backoff. These are configurable limits, **not measured latency/cost promises**. A call cap is not a dollar budget.

Old shadow plans are **never auto-applied when switching live**. `daylog queue retry CANDIDATE` explicitly requests a bounded new evaluation against current outcomes/pins/dismissals. A crashed live plan resumes its saved operations without another model call. Stale target conflicts stop that plan and require inspection/retry; already-applied operations remain.

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

Directory approval is required **both for native scanning and for the original project**. Local capture approval and cloud approval are distinct config fields, even though this setup command grants both explicitly. Set them separately in config if you want local-only capture.

Setup preserves unrelated hook/settings entries and never grants harness trust. Review Claude/Codex hook trust yourself; restart/reload pi. Installed hook definitions are pinned to the listed contracts: revalidate adapters when upgrading harnesses. Unsupported native versions, partial tails, replaced/truncated files, scope failures, and cursor bounds are reported, not called complete coverage.

Recovery reads at most 64 files per wakeup (rotating), 2 MiB per file, and 128 candidate records per file. It keeps per-file offsets and native ancestry rather than resummarizing whole histories. Original context comes from native metadata or a SessionStart capture, **never the worker's git checkout**. Deleted worktrees remain identifiable to the extent metadata was captured before deletion.

Only bounded assistant-text excerpts are copied as **claims**, not proof of execution. Hidden reasoning, images, arbitrary repository files, and raw tool output are excluded. Sensitive-line redaction is best-effort and can miss secrets: directory approval is still required. Excerpts live separately from compact native-boundary candidates. `daylog prune` removes aged processed excerpts and their copied plan-input text after the configured retention period (default 14 days); pending/error/unfinished-plan evidence is retained. Report text and compact receipts remain; a retry after evidence pruning fails visibly rather than inventing evidence.

See [capture contracts and limits](integrations/README.md). Tests verify fixtures and the pi extension through a mock API, **not live end-to-end execution of all harnesses**. Ephemeral sessions can be captured while alive; unsaved hard-crash evidence cannot be recovered. Unknown child lineage remains unknown. Codex logical history references and Claude files without supported session metadata are diagnosed as coverage gaps rather than followed outside approved scopes.

## Schedule without a daemon

```sh
daylog setup --schedule              # write resources only
daylog setup --schedule --activate   # explicit scheduler registration/start
```

The generated one-shot job runs `daylog --data-dir ABSOLUTE tick`, which attempts reconciliation then curation. Platform resources are launchd on macOS, systemd user units on Linux, and Task Scheduler XML on Windows. No scheduler or integrations are installed by `install.sh` alone.

- [macOS launchd](docs/launchd/README.md)
- [Linux systemd](docs/systemd/README.md)
- [Windows setup](docs/windows-setup.md)

Pause by stopping the scheduled job or `daylog setup --mode shadow`. Intake continues to queue, never flushes directly to the ledger. Fix the problem, inspect `explain`, and resume. Stop/disable jobs **before** `daylog setup --uninstall-resources`; only unchanged owned files and the exact registered hook commands are removed. Store/config/history remain. Modified resources are retained with an error.

## Read and diagnose

```sh
daylog today [YYYY-MM-DD] --json
daylog render [YYYY-MM-DD]
daylog status --json
daylog doctor --check-model          # catalog check, no inference
daylog explain ENTRY_OR_CANDIDATE
daylog repair-tail YYYY-MM-DD --confirm
```

Every raw event has `recorded_at` and `occurred_at`; files are partitioned by recording day. The view's required `display_at` drives sorting/display: captured occurrence day for narrative, completion occurrence time for completed todos. `filed_at` preserves the original todo filing time. Amendments do not move work to the correction day. There are no `ts`/`done_ts` compatibility fields.

Ledger corruption blocks publication, including dedup retries. `repair-tail` only removes an unterminated final line and first saves **all original bytes** beside the day file. Malformed complete/middle records require explicit inspection; no reader silently skips them. Interrupted atomic-file temporaries are diagnostic artifacts, never completed candidates.

## GitHub and widgets

```sh
daylog poll gh                         # requires authenticated gh CLI
daylog poll gh --owner 'myorg,!oldorg'
```

Configure machine scope with `github_owners` in config, `DAYLOG_GH_OWNERS`, or `--owner` (flag wins). Open PRs remain a **separate current-state snapshot**, never journal work. Snapshot PRs use their own `repo` display label and provider URLs; narrative context uses structured repository identity.

All three widgets consume only `today --json`, now using `display_at` and `filed_at`:

- [Omarchy](omarchy-plugin/README.md)
- [SwiftBar](swiftbar-plugin/README.md)
- [Windows tray](windows-plugin/README.md)

Install/update consumers together with this binary; keep the selected data directory consistent. No consumer reads the queue or Athena's decision output.

## Validation

```sh
go test ./...
go vet ./...
go test -race ./...
node --experimental-strip-types --test integrations/pi/daylog.test.mjs integrations/consumers.test.mjs swiftbar-plugin/daylog.test.mjs
GOOS=linux GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
```

Tests include concurrent producer **processes**, a killed worker after append/before acknowledgment, mid-plan replay, human edits during inference, hostile output rejection, private-file permissions, occurrence-day folding, scoped native fixtures, scratch-HOME installation, and scheduler escaping. All test data is temporary; default tests require no model credentials. Cross-compilation is **not** Linux/Windows runtime validation, and mocked widget/adapters do not establish live desktop behavior. macOS is the runtime exercised in this implementation pass, including scratch-store JXA rendering and generated-plist linting (not actual SwiftBar UI or a running LaunchAgent).

Optional synthetic-only smoke test using your existing configured pi model (not run by default). This invokes the model and uses whatever quota or billing already applies to that setup; no separate paid service is required:

```sh
DAYLOG_LUNA_SMOKE=1 DAYLOG_SMOKE_PI=/absolute/pi \
DAYLOG_SMOKE_PI_AGENT_DIR=/absolute/pi-agent-dir \
go test ./internal/athena -run TestOptInLunaSmoke -v
```

No quality/recall or latency claim is established by the seven labeled examples. Installing into a fresh operational store, examining a shadow sample, growing to 30–50 labels, and enabling live remain explicit human cutover steps.
