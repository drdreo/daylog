# Owner-scoped Athena memory

Part of #17 and #18. This local foundation stores structured memory and offers
literal-term recall. It does not generate dreams, compute embeddings, learn
skills, call a model, scan source files, or publish journal entries.

## Storage and ownership

Every command requires an explicit absolute `--root`. There is no config,
`DAYLOG_DIR`, or environment fallback for this root. Use a dedicated private
location; examples below use a synthetic temporary directory.

The root holds separate canonical stores for `athena` and `dreo`, with derivative
search indexes. A write opens both stores so correction and forgetting can
maintain tracked derivatives atomically. Read-only commands open only the
explicitly permitted owners. They never initialize missing stores, create a
lock, or change store permissions. A missing store is an error, not an empty
initialized store. Commands, including input reads, use a ten-second timeout; concurrent access can fail busy rather than wait indefinitely. Existing roots must already be private. Files and lock are mode 0600 and the root mode 0700 on Unix; Windows uses current-user/SYSTEM DACLs.

Writes pin one connection with an on-disk main database and attach the other owner file from the same root. Both use `journal_mode=DELETE` and `synchronous=FULL`; SQLite's super-journal, not merely the process lock, provides cross-database crash atomicity. WAL/SHM files or other journal modes are refused, never converted silently. Locality is checked before initialization or attachment: Darwin requires `MNT_LOCAL`; Windows requires a fixed, removable or RAM drive. Linux allows ext, XFS, Btrfs, tmpfs, ramfs, overlay and F2FS; other types (including FUSE), remote mounts and unknown drives fail closed. All owner files are unlinked direct children on the root filesystem. External writers are unsupported. Tests interrupt uncommitted attached writes, check private rollback journals and reopen both stores; they do not simulate every power-loss point or establish filesystem/hardware guarantees.

`owner` identifies whose memory it is. `author` identifies who supplied the
claim. `subject` identifies what or whom the claim concerns. These are distinct:
an Athena-authored inference about Dreo belongs to Athena, not Dreo. A source
reference does not transfer ownership or turn an inference into a fact.

Mutations use the existing `humanSource` convention: `--source`, then
`DAYLOG_SOURCE`, then `human:cli`. A nonhuman source is rejected unless the caller
explicitly supplies a human source. This is same-user accident prevention, not
authentication, operating-system identity enforcement, or a security boundary
against another process running as the same user.

## Input and output

`record` and `correct` accept exactly one JSON **Input**, from `--file FILE` or
`--file -` for stdin. Input is bounded to 32 KiB and decoded strictly: unknown
fields, duplicate fields, trailing JSON/prose, and bulk arrays are rejected.
Explicit input files must be regular files, not pipes/devices. Darwin/Linux stdin pipes use a private descriptor with cancellable deadlines; original descriptor flags are restored. On other platforms, a pipe without deadline support fails closed with regular `--file` guidance. This is not a promise of Windows pipe runtime support.

The input owner must exactly match `--owner`; correction also requires the exact
input ID to match the positional ID. The store validates field semantics.

| Input field | Meaning and limits |
| --- | --- |
| `id` | Caller-supplied idempotency key: 1–64 ASCII letters, digits, `_`, or `-`. |
| `owner` | `athena` or `dreo`; no implicit shared ownership. |
| `author` | `athena`, `dreo`, or `other`. Attribution is supplied data, not authenticated identity. |
| `subject` | The person or subject the claim concerns; does not grant owner access. |
| `kind` | Owner-specific category, listed below. |
| `status` | Epistemic status, listed below; not the revision lifecycle. |
| `content` | Memory text, at most 16 KiB. |
| `context` | Supplied context label, not automatically captured or scanned. |
| `project` | Supplied project label. |
| `topic` | Supplied topic label. |
| `occurred_at` | Reported occurrence time, RFC3339Nano, within 1970–2200. |
| `expires_at` | Optional RFC3339Nano expiry after occurrence, no later than 2200. Already expired inputs are rejected; later expiry blocks recall and source citation. |
| `provenance` | Supplied origin description, at most 1024 bytes; not a file-discovery instruction. |
| `sources` | Optional array of at most 16 current references: `owner`, `id`, `revision`, `hash`. |

Metadata values are bounded to 256 bytes, with the tighter ID and enumerated
field constraints above. References must identify the exact current source
revision and hash, not just a matching ID. The CLI never follows provenance
paths or loads external source content automatically.

A returned **Record** includes all Input fields plus store-managed `revision`,
`hash`, `lifecycle`, and `recorded_at`. `recorded_at` is storage time, distinct
from reported occurrence time. Lifecycle tracks current/corrected/ineligible
history, not confidence. All command results are JSON on stdout.

### Categories

Athena categories:

- `episode`: an attributed occurrence or experience.
- `thought`: a reflection or inference.
- `idea`: a proposed possibility, not proof of execution.
- `dream`: a speculative or inferred stored artifact, not a factual recollection.
- `mistake`: an attributed mistake, distinct from a general lesson.
- `lesson`: a proposed lesson or takeaway, not an automatically learned skill.

Dreo categories:

- `memory`: a supplied recollection.
- `mistake`: a supplied account of a mistake.
- `preference`: a supplied preference; explicit confirmation has a separate gate.

Only Athena `thought`, `idea`, `dream`, and `lesson` records may have `sources`.
These references identify tracked derived artifacts. Dreo canonical records use
supplied external `provenance`, never derivative edges. Cross-owner derivative
invalidation therefore does not rewrite Dreo's canonical recollections or
preferences.

### Epistemic statuses

- `reported`: supplied as a report, not independently verified by the store.
- `grounded`: represented as supported by evidence; the store does not verify
  the truth of supplied external provenance.
- `confirmed`: reserved for a Dreo-owned, Dreo-authored `preference`, with the
  explicit `--confirm-preference` option. JSON alone cannot confirm it.
- `inferred`: an interpretation rather than a direct observation.
- `speculative`: a possibility, not an established fact.
- `disputed`: a claim whose accuracy is contested.

Dreams must be `speculative` or `inferred`. No status silently promotes an
Athena inference into Dreo's confirmed preference.

## Record and inspect

This example uses synthetic input only:

```sh
WORK="$(mktemp -d)"
ROOT="$WORK/memory"
cat > "$WORK/input.json" <<'JSON'
{
  "id": "example-episode",
  "owner": "athena",
  "author": "athena",
  "subject": "athena",
  "kind": "episode",
  "status": "reported",
  "content": "Synthetic violet lantern observation",
  "context": "test-room",
  "project": "synthetic-project",
  "topic": "synthetic-topic",
  "occurred_at": "2026-01-02T03:04:05Z",
  "provenance": "synthetic example; no external source"
}
JSON

daylog athena memory --root "$ROOT" record \
  --owner athena --file "$WORK/input.json"
daylog athena memory --root "$ROOT" show example-episode --owner athena
daylog athena memory --root "$ROOT" list --owner athena --limit 20
daylog athena memory --root "$ROOT" recall "violet lantern" --owner athena
```

Repeating the exact active Input with the same ID is idempotent. A conflicting,
stale, or forgotten ID fails; it is not silently overwritten or resurrected.

## Explicit read scopes and recall

`list`, `recall QUERY`, and `export` require exactly one of `--owner OWNER` or
`--owners athena,dreo`. Both-owner access is never implicit. They accept
`--kind`, `--status`, `--project`, `--topic`, `--context`, and `--limit`.
Project, topic, and context filters are exact labels.

Recall accepts a nonempty query of at most 512 bytes and at most 16 alphanumeric
Unicode terms. Terms are literal search input, not advanced FTS syntax. Quotes,
operators, or punctuation do not enable an FTS query language. Results include
only current eligible records and use deterministic rank tie breaks.

A derived record is not eligible when a source is stale, expired, invalid,
forgotten, or outside the allowed owner scope. Merely reading an Athena record
does not authorize reading its Dreo sources. To inspect a cross-owner derived
record, explicitly allow both owners:

```sh
daylog athena memory --root "$ROOT" show derived-id \
  --owner athena --owners athena,dreo
daylog athena memory --root "$ROOT" recall "literal terms" \
  --owners athena,dreo --limit 20
```

`show --owners` must include the record owner. `show --history` explicitly
requests corrected history, but still excludes ineligible source dependencies,
expired, forgotten, and invalidated records. History is bounded to 100 revisions and fails explicitly above that limit. Expiry removes eligibility; it is not a promise of plaintext erasure.

## Correct, forget, and rebuild

`correct` performs a full replacement, not a patch. Supply the same owner and ID
and the current positive `--revision`. Compare-and-swap rejects stale writers.
A successful correction increments the revision and preserves the old revision
as corrected history. It transactionally invalidates tracked derivatives across
both stores. Invalidation makes their text ineligible for retrieval, but does not erase historical plaintext; forgetting is the logical erasure operation. Invalidated artifacts cannot be revived by correction; explicitly record a newly reviewed artifact with a new ID and current sources. Maintenance follows all retained source edges conservatively, so a later corrected artifact is still invalidated/forgotten if an earlier revision used the source.

```sh
daylog athena memory --root "$ROOT" correct example-episode \
  --owner athena --revision 1 --file "$WORK/corrected-input.json"
daylog athena memory --root "$ROOT" show example-episode \
  --owner athena --history
daylog athena memory --root "$ROOT" forget example-episode \
  --owner athena --revision 2 --confirm
daylog athena memory --root "$ROOT" rebuild
```

`forget` requires an exact current revision and explicit `--confirm`. It scrubs
all local historical plaintext for that source and its transitive tracked
derivatives, retaining minimal identity/revision/lifecycle tombstones. Forgotten
IDs cannot be resurrected. Only recorded source edges can identify derivatives;
untracked copies or independently entered text are not discovered by scanning.

Erasure is store-local. Exports, backups, snapshots, terminal output, provider
copies, session transcripts, and other external copies are **not** deleted. Do
not interpret store-local erasure as secure destruction of storage hardware or
all copies of a claim. Canonical rows, historical payloads, tracked dependency edges and FTS rows are deleted in one transaction. SQLite and FTS5 secure-delete are enabled, but rollback journals, prior WALs, free storage, backups or hardware snapshots are not a universal forensic-erasure guarantee.

`rebuild` is explicit writable maintenance for both derivative search indexes.
It is not a source reimport, model call, or generation step. Canonical memory
remains the source of truth; no additional source-text cache is introduced.

## Bounded export and one-record import

`export` prints one JSON array page of Records, not an unlimited dump. Default
limit is 20; valid limits are 1–100. `list` and `export` support an exclusive
`--after ID` cursor for one owner, or `--after owner:ID` with both owners. Continue from the last returned owner/ID until an empty page. Recall does not support cursors. To include Athena artifacts with Dreo sources, explicitly use both owners while exporting; single-owner scope intentionally excludes those artifacts.

```sh
daylog athena memory --root "$ROOT" export --owner athena --limit 100 \
  > "$WORK/export-page.json"
daylog athena memory --root "$ROOT" export --owner athena \
  --limit 100 --after last-returned-id
```

Redirected files are external copies managed by the caller. There is no bulk
export-ingestion command. For an explicit single-record roundtrip, select one
Record, project its Input fields, and pass that object to `record --file`.
Remove `revision`, `hash`, `lifecycle`, and `recorded_at`; importing those
store-managed fields is rejected. A `confirmed` preference still requires
`--confirm-preference`, and source references still require valid current
records in the destination. Import does not bypass ownership, provenance,
idempotency, or size validation.

## Read-only dream viewer

```sh
daylog athena dreams --root "$ROOT" --limit 20
daylog athena dreams dream-id --root "$ROOT"
daylog athena dreams dream-id --root "$ROOT" --owners athena,dreo
```

`dreams` fixes the record owner to Athena and the kind to `dream`; it cannot
switch to Dreo records or other kinds. By default only Athena sources are
allowed. `--owners athena,dreo` explicitly permits cited Dreo source reads; it
does not change dream ownership. The ID form rejects non-dream records.

The list form accepts `--status`, `--project`, `--topic`, `--context`, `--limit`,
and `--after` (ID for Athena-only scope, owner:ID with both owners). These list filters are rejected with an ID instead
of being silently ignored. This viewer cannot import, mutate, confirm, forget,
generate, or initialize stores. Its speculative content is not evidence that
an event occurred or a skill was learned.
