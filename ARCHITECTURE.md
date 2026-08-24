# Daylog — Architecture Document

**Status:** v1.3 (Phases 1–2 & Omarchy widget implemented; Phase 3 deferred) · **Date:** 2026-08-23
**Problem:** A single developer runs multiple independent coding agents (Claude Code, Codex, pi) across multiple machines. Work fragments into parallel sessions, side quests, and ~16 open PRs. There is no single place that answers "what actually happened today, and what needs me?"

---

## 1. Goals and non-goals

Daylog keeps the human in the loop by collecting a short, structured trail of what every agent (and the human) did, enriching it with live status from external systems, and rendering it as one daily view. It must remain useful when everything else fails: no network, no daemon, no UI — the raw files must still tell the story of the day.

Explicit non-goals: Daylog does not orchestrate, schedule, or manage agents (they stay independent), it does not replace the issue tracker or GitHub as the system of record for tasks and code, and it is not a metrics or surveillance product. It is a personal ledger, optimized for one human's end-of-day comprehension.

## 2. Design principles

**Events are the source of truth; everything else is derived.** The system is a small event-sourcing design. Producers append immutable events; state (open todos, current classification of an entry, today's summary) is computed by folding over events at read time. Nothing ever mutates history — corrections are new events pointing at old ones.

**One write path.** No producer writes storage files directly. Everything goes through the `daylog` CLI, which owns the schema, validation, timestamps, ID generation, and context capture. Agents, pollers, Slack bridges, and the human are all just callers of the same command. This is the single most important extensibility decision: adding an integration never means teaching a new component how the storage works.

**Producers are plural and dumb; consumers are plural and dumb.** Both sides of the store are replaceable. A new producer (a Linear poller, a Slack bridge) and a new consumer (an Omarchy widget, an end-of-day summarizer) can each be added without touching anything else.

**Local-first.** Every machine has a full, immediately usable copy of the data. Sync is replication of immutable events between peers, not a client-server dependency. The system degrades gracefully: offline machines keep logging; sync catches up later.

**Boring formats.** Newline-delimited JSON on disk, one file per day, readable with `cat` and `jq`. Human-readable markdown is a rendered view, regenerable at any time, never the source of truth.

## 3. System overview

```
┌────────────────────────── Producers ──────────────────────────┐
│  Agents (claude/codex/pi)   Human (CLI, Slack)   Pollers      │
│                                                  (gh, linear) │
└──────────────┬────────────────────┬──────────────────┬────────┘
               ▼                    ▼                  ▼
        ┌──────────────────────────────────────────────────┐
        │              daylog CLI  (single write path)     │
        │  validate · timestamp · id · capture context     │
        └───────────────────────┬──────────────────────────┘
                                ▼
        ┌──────────────────────────────────────────────────┐
        │  Event store: <data>/events/YYYY-MM-DD.jsonl     │
        │  Sidecar snapshots: <data>/state/*.json          │
        └───────────────────────┬──────────────────────────┘
                    (sync layer replicates events            
                     between machines, §8)                   
                                ▼
        ┌──────────────────────────────────────────────────┐
        │  Derived state: fold(events) + join(snapshots)   │
        │  exposed as `daylog today --json`                │
        └───────────────────────┬──────────────────────────┘
               ▼                ▼                  ▼
        Omarchy widget    `daylog today`    EOD summarizer
```

`<data>` is the platform's per-user data directory — `$XDG_DATA_HOME/daylog` (Linux, default `~/.local/share/daylog`), `~/Library/Application Support/daylog` (macOS), `%AppData%\daylog` (Windows) — overridable with `$DAYLOG_DIR`, so the layout is cross-platform by construction.

## 4. Data model

### 4.1 Event schema

Every event is one JSON object on one line. The schema is deliberately small, with two designated extension points (`refs` and `meta`):

```json
{
  "id": "01J5X7K3...",
  "ts": "2026-08-23T14:32:05+02:00",
  "host": "arch-desktop",
  "source": "agent:claude",
  "type": "work",
  "tldr": "Fixed token refresh race condition, opened PR",
  "refs": ["gh:pr:owner/repo#142"],
  "ctx": { "repo": "owner/repo", "branch": "auth-refactor", "cwd": "~/src/repo" },
  "parent": null,
  "meta": {}
}
```

Field semantics: `id` is a ULID — lexically sortable by creation time and globally unique without coordination, which is what makes multi-machine merge trivial (§8). `host` records the originating machine. `source` is a namespaced producer identity (`agent:claude`, `agent:codex`, `human:cli`, `human:slack`, `poller:gh`, `poller:linear`); consumers can filter or group on the namespace without knowing the full list of producers. `type` is the event kind (§4.2). `refs` is a list of typed URIs linking the event to external objects. `ctx` is auto-captured by the CLI, never supplied by the caller. `parent` points at an earlier event's `id` for corrections and closures. `meta` is a free-form object for producer-specific payload that the core schema doesn't model.

### 4.2 Event types

The core vocabulary: `work`, `sidequest`, and `note` record activity; `todo` opens an obligation; `done` (with `parent`) closes one; `reclassify` (with `parent` and a new type in `meta.to`) reinterprets an earlier entry — this is how a side quest gets promoted to real work without editing history; `transition` records an externally observed state change (a PR merged, a Linear issue moved), emitted only by pollers. Unknown types must be tolerated by all consumers (rendered generically, never crashed on), so a new producer can introduce a type before every consumer learns about it.

### 4.3 Typed refs

Refs are URIs with a scheme, not bare strings: `gh:pr:owner/repo#142`, `linear:ABC-123`, `jira:PROJ-45`, `slack:C0123/p16929...`. This is the second extension point. The join between an agent's "I opened PR #142" and the poller's live status happens by ref equality at read time; adding Linear support means the Linear poller writes snapshots keyed by `linear:` refs and nothing else changes. The CLI normalizes shorthand (`#142` in a repo context becomes the full `gh:pr:` ref using captured `ctx.repo`).

### 4.4 Snapshots

Machine-scoped settings live in `<data>/config.json`, alongside the snapshots and under the same non-sync rule: flags configure a run, environment variables configure a shell, and the file configures the machine — which is the only one of the three that a GUI-launched widget button or a scheduled job inherits. Pollers maintain sidecar files under `<data>/state/` — `gh-prs.json`, later `linear-issues.json` — each an atomic-rename-replaced document containing the current truth for its domain plus a `fetched_at` timestamp. Snapshots are per-machine caches of external state: they are *not* synced (each machine can poll for itself, or one machine's staleness marker makes the situation honest), and they are *not* events. The event store holds the narrative; snapshots hold the now.

## 5. The write path: `daylog` CLI

The CLI is the system's contract. Core commands:

```
daylog add [--type work|sidequest|note|todo] [--ref REF]... "TLDR text"
daylog done <id|fuzzy-match>
daylog reclassify <id> <new-type>
daylog today [--json] [--type ...] [--source ...]
daylog render [DATE]          # emit the markdown view
daylog poll gh                # run a poller once (same path the timer uses)
daylog sync                   # push/pull events (§8)
```

On `add`, the CLI generates the ULID and timestamp, reads `$DAYLOG_SOURCE` (each agent's environment sets its identity once, e.g. `agent:claude`), captures cwd/repo/branch, normalizes refs, validates, and appends one line with `O_APPEND` — which on Linux is atomic for writes of this size, so concurrent agents need no locking. Exit codes are meaningful and the failure mode is loud to the caller but never corrupting to the store: a malformed call is rejected before anything touches disk.

The CLI enforces a 280-character limit on `tldr` at write time, rejecting (not truncating) oversized entries so the caller rewrites rather than silently losing the tail. Prompt-level requests keep agents honest most of the time; validation at the single write path keeps them honest all of the time.

### 5.1 The canonical agent instruction

One markdown block, written once and pasted verbatim into each agent's global config — `~/.claude/CLAUDE.md` for Claude Code, `~/.codex/AGENTS.md` for Codex, pi's equivalent. Agent identity is deliberately *not* in the prompt: each harness's launch wrapper sets `DAYLOG_SOURCE=agent:<name>` once, so `source` is correct by construction rather than by agent self-report.

```markdown
## Work logging (daylog)

When you complete a task — not after every message — run:

    daylog add --type <work|sidequest> "one-line TLDR, ≤280 chars"

If you notice something actionable that you are NOT doing, file it for
the human to review:

    daylog add --type todo "concrete action, for human review"

- Add `--ref '#142'` (or a Linear/Jira id) for any PR or issue involved.
- `work` = the task you were asked to do; `sidequest` = anything you did
  that wasn't the original ask. When unsure, use `sidequest`.
- Log failures and dead ends too: "attempted X, blocked by Y" is valid.
- Todos go to the human's review queue. Do not act on them, track them,
  or file them for yourself — filing one ends your involvement with it.
- No thinking-out-loud or observations: only concrete actions.
- One entry per completed task. Never write to daylog's data files directly.
```

The `note` type remains in the schema for human quick-capture and the EOD summarizer, but is intentionally excluded from the agent vocabulary to keep agent noise out of the log. Where a harness supports deterministic hooks (Claude Code's Stop hook), a backstop checks that a task entry was logged and reminds the agent if not — reminding rather than auto-generating, because an auto-generated TLDR ("edited 3 files") defeats the purpose of delegating the summarization.

### 5.2 Agent todos are proposals

Agent-filed todos are proposals, not commitments — but they are still todos, and they live in the same list as your own. Splitting them into a second "inbox" bucket bought noise isolation at the price of a second list to work from, and of every consumer rendering the same todo twice; `open_todos` now holds every open todo and `needs_triage` *filters* it down to the agent-filed ones still awaiting a verdict.

Triage is a `triage` event carrying `meta.verdict`, and it is the human's call — the CLI rejects a verdict from an `agent:*` source, or a self-approving agent would make the queue ceremonial:

- **accept** (`daylog accept <id>`) — adopt it as yours. The type is unchanged and it stays in `open_todos`; accepting only clears the awaiting-triage flag so the widget stops nagging.
- **decline** (`daylog decline <id> --note "why"`) — reject it. The event stays in the ledger, but the todo drops out of every rendered view: it was never yours to carry.

Both are append-only, so the last verdict wins and a decline is reversible by a later accept. Proposals left untriaged are surfaced prominently — an ignored review queue silently recreates the lost-context problem the system exists to solve.

## 6. Producers

**Agents** are pure CLI callers as described above. No SDK, no library, no per-agent code.

**The human** logs via the same CLI (`daylog add "lunch idea: cache the embeddings"`), and later via Slack (§7.2). Reclassification and todo-closure are human-only operations in practice, though nothing enforces that.

**Pollers** follow one shared pattern, established by the GitHub poller and reused by every future integration: a systemd user timer fires `daylog poll <name>`; the poller fetches current state from the external API; writes the snapshot atomically; diffs against the previous snapshot; and emits `transition` events *only for meaningful changes* (PR merged, checks flipped red/green, review decision changed, issue moved column, issue assigned to you). Two invariants: a failed or partial fetch must skip the diff entirely rather than fabricate transitions, and a poller with no network exits 0 and leaves the old snapshot with its honest `fetched_at`. A poller may also be scoped per machine — the GitHub poller takes an owner filter (`--owner`, `$DAYLOG_GH_OWNERS`, or `gh_owners` in `<data>/config.json`, in that precedence) because one machine is rarely one context, and a work laptop should not narrate the side project's PRs. Scope is a fetch-time concern, never a diff-time one: a PR leaving scope stops being tracked, it is not narrated as closed. New integrations are new pollers; the core never changes. Pollers shell out to the provider's own CLI where one exists (`gh api` for GitHub) rather than speaking HTTP natively: auth comes for free from the tool the machine already uses, and the failure modes stay honest — `gh` absent or unauthenticated means skip the diff and keep the stale snapshot. The daylog binary itself stays dependency-free for everything except polling.

## 7. Planned integrations

### 7.1 Issue trackers (Linear, Jira)

An issue-tracker poller is structurally identical to the GitHub poller: snapshot of your assigned/active issues into `linear-issues.json`, transitions for state changes worth narrating. Two integration touch points come for free from the data model: agents can `--ref linear:ABC-123` when a task originates from an issue, and the rendered day view groups entries under the issues they reference — turning "16 disconnected log lines" into "3 issues progressed, 2 side quests." Recommended sequencing: build this only after the GitHub poller has been running for a couple of weeks, so the poller pattern is proven before it's duplicated.

### 7.2 Slack quick-note

Slack is an *inbound producer*, not a consumer integration: the goal is capturing thoughts from your phone or from a work conversation into today's log. The simplest robust design is a tiny bridge (a slash command `/daylog note ...` or a dedicated DM channel watched by a small bot) that translates messages into `daylog add --source human:slack` calls on a machine you control. Because events carry `source` and sync merges by union (§8), it doesn't matter which machine the bridge runs on. A later, optional outbound direction — the EOD summary posted to a private Slack channel — is just another consumer and needs no new architecture.

### 7.3 End-of-day summarizer

A scheduled agent invocation (any of the three agents can do it) that reads `daylog today --json`, writes a narrative summary, and appends it as a `note` event with `source: agent:<name>` and `meta.kind: eod-summary`. This closes the loop elegantly: the summarizer is simultaneously a consumer and a producer, using only the two public interfaces.

## 8. Multi-machine sync

The append-only, immutable-event design was chosen partly because it makes sync nearly trivial. Since events are never edited, never deleted, and carry globally unique ULIDs, **merging two machines' logs is a set union**: collect all events for a day from all machines, dedupe by `id`, sort by ULID. There are no conflicts by construction — the classic sync problem (two machines edited the same thing) cannot occur, because nothing is ever edited. Two machines *reclassifying* the same entry produces two `reclassify` events, and the fold resolves it deterministically (last ULID wins), with both opinions preserved in history.

Events are stored in a single per-day file (`events/2026-08-23.jsonl`) — one source of truth per day; the originating machine is recorded in-band by each event's `host` field, not in the filename. Two machines writing the same day therefore produce diverging copies of one file, and `daylog sync` resolves that with the same union: concatenate both versions, dedupe by `id`, sort by ULID, rewrite (with git as the transport, a `union`-style merge driver amounts to the same thing). Derived state and rendered markdown are never synced — each machine recomputes them locally. Snapshots are never synced either (§4.4).

Three transport options, in recommended order:

**Git (recommended start).** `<data>/events` is a git repo; `daylog sync` commits and pushes/pulls against a private remote (GitHub private repo, or self-hosted). Same-day writes from two machines merge by the id-union rule above, configured once as a git merge driver. This costs nothing, gives free history/backup/audit, works through any firewall you already work through, and is inspectable with tools you already know. Its only weakness is latency — sync happens when triggered (post-add hook, timer, or manual), not instantly.

**Syncthing.** Continuous peer-to-peer replication of the events directory, no cloud party involved. Good fit if the machines are often on the same network and you want near-real-time cross-desktop views. Slightly riskier around partial-file propagation; mitigated by line-oriented parsing that skips a torn final line (though with a shared per-day file, Syncthing's last-writer-wins conflict copies need the same id-union reconciliation).

**Object storage (S3/R2).** Each host pushes its own day-files to a bucket; readers pull all hosts' files. Cleanest for a future phone/web consumer, but introduces credentials and a cloud dependency for what is otherwise a local-first system. Defer until an actual remote consumer exists.

The sync layer is deliberately a dumb file-replication concern *underneath* the store, invisible to producers and consumers. Switching transports later changes `daylog sync` and nothing else.

## 9. Consumers

All consumers read exactly one interface: `daylog today --json` (and `daylog render` for markdown). The terminal view ships first and validates the schema through real daily use. The bar widgets come second — the Omarchy plugin (a QML bar-widget plugin for Quickshell), its macOS sibling (a SwiftBar/xbar menu bar plugin in dependency-free JXA), and its Windows sibling (a system tray widget in dependency-free PowerShell/WinForms), all shelling out to `daylog today --json` and rendering entries grouped by type, with PR decorations from the snapshot join; each is by design the *thinnest* component in the system, replaceable in an afternoon, which is exactly why UI was deferred to the end. That all three exist without any knowing about the others is the dumb-consumer principle paying out. The EOD summarizer (§7.3) and any future phone view are additional consumers with no special privileges.

## 10. Failure modes and trust boundaries

The store survives every component failing: a crashed agent leaves at most a missing line; a crashed poller leaves a stale-but-honest snapshot; a dead sync remote leaves fully functional local machines. The one systemic risk is schema drift between producers and consumers, contained by three rules — consumers tolerate unknown `type` and `source` values, the CLI is the only component that validates on write, and schema changes are additive only (new fields, never repurposed ones), with a `v` field addable later if a breaking change ever becomes unavoidable.

Trust: everything runs as your user on your machines; agents can only call the CLI, which validates input, so a confused agent cannot corrupt the store — worst case is a garbage TLDR, which is visible and correctable via `reclassify`. The Slack bridge is the only component exposed to input from outside the machine and should treat message text strictly as opaque payload for `tldr`, never as instructions or shell input.

## 11. Build order

Phase 1 — the spine: event schema, `daylog add/today/render`, agent instructions in all three agents' configs. Live on it for a week; this validates the schema before anything is built on top.

Phase 2 — GitHub poller: snapshot + diff + transitions, systemd user timer, ref-join in the `today` view. This is where the "16 PRs" problem is actually solved.

Phase 3 — sync: git transport, id-union merge driver, `daylog sync` with a timer and/or post-add hook. Second machine joins. *(Deferred by decision: single-machine use is the reality today; revisit when a second machine actually joins.)*

Phase 4 — surfaces: Omarchy widget (implemented in `omarchy-plugin/`, a Quickshell bar-widget plugin for Omarchy 4 that reads `daylog today --json`), macOS menu bar widget (implemented in `swiftbar-plugin/`, a SwiftBar plugin reading the same contract), Windows 11 tray widget (implemented in `windows-plugin/`, a PowerShell/WinForms tray widget reading the same contract), EOD summarizer.

Phase 5 — integrations as demand proves out: Linear/Jira poller (clone of the gh poller), Slack inbound bridge.

Each phase is independently useful, and no phase requires rework of a previous one — that property falls directly out of the single-write-path and dumb-consumer principles.

## 12. Open questions

Whether PR transitions should cover all your open PRs or only those referenced in daylog entries (start with all; add a filter if noisy). Whether `todo` events deserve due-dates and priorities or stay deliberately flat (recommend flat until proven insufficient). Retention: whether to ever compact old days into monthly summaries, or keep everything forever (JSONL of one-line TLDRs is small enough that forever is plausible). And identity for the Slack bridge: slash command (simpler, requires a public endpoint or Slack app) versus bot-watched DM channel (simpler auth, slightly clunkier UX).
