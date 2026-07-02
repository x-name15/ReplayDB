# ReplayDB — Documentation

Deep-dive reference for how ReplayDB is built internally. For install, quick start, and CLI usage, see the [README](../README.md).

---

## Architecture

```text
cmd/
  redb/     → server entrypoint
  recli/    → CLI client (subpackages: db/, helper/)
internal/
  domain/     → Aggregate interface + Registry (bring your own aggregate types)
  engine/     → append logic, in-memory index, time-travel replay
  storage/    → binary log + snapshot encoding, CRC32-checked
  wire/       → length-prefixed binary TCP protocol
  server/     → HTTP dashboard
  helper/     → .env loading (helper.Load / helper.GetEnv)
  tests/      → centralized test suite (external package, public API only)
  benchmarks/ → centralized benchmark suite (same convention as tests/)
```

## `internal/domain` — Aggregate interface + Registry

The engine has no built-in notion of "Order" or any specific business concept. Any type implementing:

```go
type Aggregate interface {
    Apply(eventType string, payload []byte, timestamp time.Time) error
    Version() uint32
}
```

can be registered under a `kind` string via `domain.Registry.Register`.

`OrderState` in `order.go` is a reference implementation, not a hardcoded special case. The engine reaches it only through the registry, using the same path as any other aggregate.

## `internal/storage` — Binary log + snapshot format

Every record—event or snapshot—is written as:

```text
[2-byte magic][body][CRC32 checksum]
```

`EventRecord` body layout:

```text
timestamp (int64)
kindLen (uint16)
idLen (uint16)
typeLen (uint16)
payloadLen (uint32)
kind
id
type
payload
```

`SnapshotRecord` uses the same layout, replacing the event type and payload with:

```text
version (uint32)
stateLen (uint32)
stateJSON
```

On read, both the magic bytes and CRC32 are verified before a record is trusted. Corruption is detected rather than silently replayed.

Every length-prefixed field is also bounds-checked **before** performing `make([]byte, n)`, preventing corrupted or truncated files from forcing excessive memory allocations before checksum validation.

| Field | Max size |
|-------|---------:|
| Event `kind` / `id` / `type` | 4 KiB each |
| Event `payload` | 64 MiB |
| Snapshot `kind` / `id` | 4 KiB each |
| Snapshot `stateJSON` | 128 MiB |

## `internal/wire` — Binary TCP protocol

Frames use length-prefixed encoding:

```text
[uint32 length][body bytes]
```

This replaces the original text protocol (`APPEND|id|type|payload`), which failed whenever JSON payloads contained `|`.

Both frame size and individual field sizes are bounded by `maxFieldLen` (64 MiB), preventing malformed or malicious frames from triggering unbounded allocations.

The protocol defines three opcodes:

- `OpAppend`
- `OpReplay`
- `OpSnapshot`

Message formats live in `protocol.go`, while the low-level framing helpers (`frameBuffer` and `frameReader`) live in `protocol_buffer.go`.

## `internal/engine` — Append, index, replay

- **`appender.go`** — `Appender.Append` encodes an `EventRecord`, writes it, and calls `fsync()` before returning. Every acknowledged append is durable on disk, not merely buffered by the operating system.

- **`index.go`** — `Index` maps `(kind, id) → []offset` in memory. It is built once during startup by scanning the log (`Rebuild`) and is updated incrementally on every append. Rebuild tolerates individually corrupted records (`ErrChecksumMismatch`).

- **`replay.go`** — `ReplayStateAt(kind, id, targetTime)` first finds the most recent snapshot at or before `targetTime`, skips the events already represented by that snapshot (`snapshot.Version`), and then replays the remaining events either through indexed seeks (`replayIndexed`) or a full log scan (`replayFullScan`). Replay stops immediately once an event timestamp exceeds `targetTime`. This is the complete time-travel mechanism.

## `internal/server` — HTTP dashboard

The dashboard exposes a single route (`/`) rendering `templates/dashboard.html`, embedded into the binary with `//go:embed`.

Query parameters (`?kind=&id=`) invoke `ReplayStateAt` using the current time and render the resulting aggregate state as JSON.

The HTTP server uses explicit `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, and `IdleTimeout` values instead of Go's zero-value defaults.

Optional HTTP Basic Authentication is enabled through `REDB_DASHBOARD_USER` and `REDB_DASHBOARD_PASS`. See the [README configuration section](../README.md#configuration) for details.

---

## Data integrity guarantees

- **Detection, not silent corruption.** Magic bytes and CRC32 are verified for every event and snapshot.
- **Bounded allocation.** Every length-prefixed field has a hard upper limit enforced before allocating memory.
- **`fsync()` on every append.** `Appender.Append` returns only after data is safely written to disk.
- **Replay correctness.** `internal/tests/engine_test.go` verifies that indexed replay and full-log replay always produce identical results. The index is purely a performance optimization.

## Time-travel semantics

`ReplayStateAt(dataDir, kind, id, targetTime, registry, index)` performs the following steps:

1. Instantiate a fresh aggregate with `registry.New(kind, id)`.
2. Find the latest snapshot whose timestamp is less than or equal to `targetTime`.
3. Restore the snapshot state and record its `Version` as the number of events to skip.
4. Replay the remaining events using indexed seeks when available, otherwise a full log scan.
5. Stop as soon as an event timestamp exceeds `targetTime`.

As a result:

- `ReplayStateAt(..., time.Now(), ...)` returns the current state.
- Any earlier timestamp reconstructs the state exactly as it existed then.
- Snapshots reduce replay work by avoiding replay from the beginning of the event log.

## Benchmarks

`internal/benchmarks/` follows the same "external package, public API only" convention as `internal/tests/`.

Run:

```bash
make bench
```

Notable benchmarks include:

- `BenchmarkAppender_Append`, which measures the real cost of appends including the required `fsync()` call.
- `BenchmarkReplayStateAt_Indexed` and `BenchmarkReplayStateAt_FullScan`, which quantify the performance improvement provided by the in-memory index as the event log grows.