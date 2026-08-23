# daylog

A personal ledger of what every coding agent (and the human) did today.
Producers append immutable events through one CLI; the daily view is derived
by folding over them. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full
design; this build covers the spine (schema + `add`/`done`/`reclassify`/
`today`/`render`), the GitHub poller with the PR snapshot join in the day
view, and the bar widgets (Omarchy on Linux, SwiftBar on macOS).
(Multi-machine sync — Phase 3 — is deliberately deferred.)

## Install

```sh
./install.sh                  # builds and installs to ~/.local/bin
./install.sh --timer          # …and enables the systemd poll timer (Linux)
./install.sh --omarchy        # …and installs the Omarchy bar widget (Linux)
./install.sh --swiftbar       # …and installs the SwiftBar menu bar widget (macOS)
```

Override the destination with `DAYLOG_INSTALL_DIR`. Or do it by hand —
it's a single static binary, works on Linux, macOS, and Windows:

```sh
go build -o daylog .          # or: go install github.com/drdreo/daylog@latest
```

## Usage

```sh
daylog add --type work --ref '#142' "Fixed token refresh race, opened PR"
daylog add --type todo "Rotate the leaked staging token"
daylog add "lunch idea: cache the embeddings"        # defaults to note

daylog done "staging token" --note "won't do: already rotated"
daylog reclassify 01m0pw23bgqh work                  # promote a sidequest

daylog today                    # markdown view of today
daylog today --json             # the consumer contract (widget, summarizer)
daylog today --source agent     # only agent entries
daylog render 2026-08-20        # any past day

daylog poll gh                  # one GitHub poll cycle (see below)
daylog poll gh --dry-run        # show transitions without writing anything
```

`done` and `reclassify` take a unique ULID prefix (shown as the trailing
code on every rendered line) or a case-insensitive substring of the entry
text; open todos are searched first and ambiguity is an error, never a guess.

## Where data lives

One JSONL file per day, readable with `cat` and `jq`:

| Platform | Default |
|---|---|
| Linux | `$XDG_DATA_HOME/daylog` (→ `~/.local/share/daylog`) |
| macOS | `~/Library/Application Support/daylog` |
| Windows | `%AppData%\daylog` |

Override with `DAYLOG_DIR`. Events land in `events/YYYY-MM-DD.jsonl`;
poller snapshots land in `state/` (per-machine caches, never synced).

## The GitHub poller

`daylog poll gh` tracks every open PR you authored. It shells out to the
[`gh` CLI](https://cli.github.com) — auth comes for free from the tool the
machine already uses; `gh` absent, unauthenticated, or offline means the
poll skips honestly (exit 0, old snapshot kept) rather than fabricating
anything. Each cycle:

1. fetches your open PRs plus any PR the last snapshot still thought was
   open (that re-fetch is how a merge or close gets observed),
2. replaces `state/gh-prs.json` atomically, and
3. diffs against the previous snapshot, logging `transition` events with
   `source: poller:gh` for the changes worth narrating: **PR merged**,
   **closed without merge**, **checks flipped red/green**, and **review
   decisions** (approved / changes requested).

The first run only establishes a baseline. Nothing is logged for a PR
merely appearing (opening it was your own action, already narrated) or for
checks going pending (a fresh push is not news).

The day view joins the snapshot by ref equality: entries referencing a PR
show its live status, and an **Open PRs** section lists everything open —
marked STALE when the snapshot is hours old. In `--json`, entries gain a
`pr` object and the day gains `prs` + `prs_fetched_at`.

The poller itself is cross-platform — `daylog poll gh` works anywhere the
`gh` CLI does. Only the scheduling mechanism is per-platform; systemd is
just the Linux convenience, not a dependency.

**Linux** (systemd user timer):

```sh
cp docs/systemd/daylog-poll-gh.{service,timer} ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now daylog-poll-gh.timer
```

**macOS** — cron is the two-line option (`crontab -e`):

```
*/10 * * * * /usr/local/bin/daylog poll gh
```

or use launchd with a `StartInterval: 600` agent plist if you want it to
survive sleep/wake more gracefully.

**Windows** (Task Scheduler):

```powershell
schtasks /Create /TN "daylog poll gh" /SC MINUTE /MO 10 /TR "C:\path\to\daylog.exe poll gh"
```

## The bar widgets

Two thin surfaces over the same contract — an icon that lights up when
something needs you (untriaged agent todos, failing PR checks) and a
panel/menu with the whole day: entries, todos, inbox, and open PRs. Both
read only `daylog today --json`, exactly as the architecture prescribes for
consumers.

- **Linux / Omarchy** — [`omarchy-plugin/`](omarchy-plugin/) is a bar widget
  for Omarchy 4's Quickshell desktop. Install with `./install.sh --omarchy`;
  details in [omarchy-plugin/README.md](omarchy-plugin/README.md).
- **macOS** — [`swiftbar-plugin/`](swiftbar-plugin/) is a menu bar widget
  for [SwiftBar](https://swiftbar.app) (xbar-compatible), a single
  dependency-free JXA script. Install with `./install.sh --swiftbar`;
  details in [swiftbar-plugin/README.md](swiftbar-plugin/README.md).

## Producer identity

Every event carries a `source`. It resolves from `--source`, then
`$DAYLOG_SOURCE`, then defaults to `human:cli`. Each agent harness's launch
wrapper sets `DAYLOG_SOURCE=agent:<name>` once, so identity is correct by
construction. The instruction block to paste into agent configs is in
[docs/AGENT_INSTRUCTIONS.md](docs/AGENT_INSTRUCTIONS.md).

## The JSON contract

`daylog today --json` emits the folded view — reclassifications and closures
already applied, so consumers stay dumb:

```json
{
  "date": "2026-08-23",
  "generated_at": "2026-08-23T18:02:11+02:00",
  "entries":    [ { "id", "ts", "source", "type", "original_type?", "tldr",
                    "refs", "ctx", "done", "done_note?", "meta?", "pr?" } ],
  "open_todos": [ "…open human todos, any day…" ],
  "agent_inbox":[ "…untriaged agent-filed todos, any day…" ],
  "prs":        [ { "ref", "repo", "number", "title", "url", "state",
                    "draft", "checks", "review", "updated_at" } ],
  "prs_fetched_at": "…snapshot age; absent until the poller has run…"
}
```

Raw events remain available in the `.jsonl` files for anything the fold
doesn't answer.

## Development

```sh
go test ./...
go vet ./...
```
