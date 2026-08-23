# daylog

A personal ledger of what every coding agent (and the human) did today.
Producers append immutable events through one CLI; the daily view is derived
by folding over them. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full
design; this is the Phase 1 build (the spine: schema + `add`/`done`/
`reclassify`/`today`/`render`).

## Install

```sh
go build -o daylog .          # or: go install github.com/drdreo/daylog@latest
```

Single static binary, works on Linux, macOS, and Windows.

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

Override with `DAYLOG_DIR`. Events land in `events/YYYY-MM-DD.jsonl`.

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
                    "refs", "ctx", "done", "done_note?", "meta?" } ],
  "open_todos": [ "…open human todos, any day…" ],
  "agent_inbox":[ "…untriaged agent-filed todos, any day…" ]
}
```

Raw events remain available in the `.jsonl` files for anything the fold
doesn't answer.

## Development

```sh
go test ./...
go vet ./...
```
