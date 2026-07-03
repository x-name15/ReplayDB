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
  server/     → HTTP dashboard + /metrics
  metrics/    → operational counters, exposed in Prometheus text format
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

- **`appender.go`** — `Appender.Append` encodes an `EventRecord`, writes it, and calls `fsync()` before returning. Every acknowledged append is durable on disk, not merely buffered by the operating system. Also emits a `[APPEND]` terminal log line and records `internal/metrics` counters (success/error, duration).

- **`index.go`** — `Index` maps `(kind, id) → []offset` in memory. It is built once during startup by scanning the log (`Rebuild`) and is updated incrementally on every append. Rebuild tolerates individually corrupted records (`ErrChecksumMismatch`) and logs a summary (`[INDEX]`) of aggregates/events/corrupt-records-skipped. `Index.Len()` reports the number of distinct aggregates tracked, used by the `replaydb_index_aggregates` metric.

- **`replay.go`** — `ReplayStateAt(kind, id, targetTime)` first finds the most recent snapshot at or before `targetTime`, skips the events already represented by that snapshot (`snapshot.Version`), and then replays the remaining events either through indexed seeks (`replayIndexed`) or a full log scan (`replayFullScan`). Replay stops immediately once an event timestamp exceeds `targetTime`. This is the complete time-travel mechanism. Emits a `[TRAVEL]` terminal log line showing which path was taken, and records the corresponding `internal/metrics` counters.

## `internal/server` — HTTP dashboard

The dashboard exposes two routes on the same HTTP server:

- **`/`** — renders `templates/dashboard.html`, embedded into the binary with `//go:embed`. Query parameters (`?kind=&id=`) invoke `ReplayStateAt` using the current time and render the resulting aggregate state as JSON.
- **`/metrics`** — Prometheus-formatted operational metrics (see [Observability](#observability) below).

The HTTP server uses explicit `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, and `IdleTimeout` values instead of Go's zero-value defaults.

Optional HTTP Basic Authentication is enabled through `REDB_DASHBOARD_USER` and `REDB_DASHBOARD_PASS`, and applies only to `/` — `/metrics` is unauthenticated by default (see below). See the [README configuration section](../README.md#configuration) for details.

---

## Observability

ReplayDB exposes an unauthenticated `/metrics` endpoint on the same HTTP
server as the dashboard, in the Prometheus text exposition format —
hand-written in `internal/metrics` rather than pulled in via the official
client library, keeping the zero-dependency guarantee intact.

```bash
curl http://localhost:8080/metrics
```

| Metric | Type | Meaning |
|---|---|---|
| `replaydb_append_total` / `_errors_total` | counter | Append calls, and how many failed |
| `replaydb_append_duration_seconds_avg` | gauge | Average Append latency (fsync included) |
| `replaydb_travel_total` / `_errors_total` | counter | ReplayStateAt calls, and how many failed |
| `replaydb_travel_duration_seconds_avg` | gauge | Average ReplayStateAt latency |
| `replaydb_travel_indexed_total` / `_fullscan_total` | counter | Which replay path was actually taken |
| `replaydb_snapshot_total` / `_errors_total` | counter | SaveSnapshot calls, and how many failed |
| `replaydb_connections_opened_total` / `_active` | counter / gauge | TCP wire connections |
| `replaydb_events_log_bytes` / `replaydb_snapshots_log_bytes` | gauge | On-disk log sizes |
| `replaydb_index_aggregates` | gauge | Distinct aggregates tracked by the in-memory index |

If you're exposing the HTTP port beyond localhost, put `/metrics` behind
the same reverse proxy or firewall rule you'd use for
`REDB_DASHBOARD_USER`/`REDB_DASHBOARD_PASS` — it isn't behind Basic Auth by
default, following the usual Prometheus convention of scraping from a
trusted internal network.

## Data integrity guarantees

## Guarantees

What ReplayDB actually promises, stated explicitly rather than implied.
If it's not listed as guaranteed below, assume it isn't.

### Guaranteed

| Guarantee | How it's enforced |
|---|---|
| **Durability** — an acknowledged `Append`/`SaveSnapshot` is on disk, not just buffered | Explicit `fsync()` after every write in `Appender`, before returning to the caller |
| **Corruption detection** — a damaged record is caught, not silently replayed as valid data | Magic bytes + CRC32 checksum verified on every event and snapshot read |
| **Bounded allocation** — a corrupted or hostile length prefix can't force an unbounded `make([]byte, n)` | Every length-prefixed field is checked against a hard max *before* allocation (`internal/storage`, `internal/wire`) |
| **Per-aggregate ordering** — events for a given `(kind, id)` are always applied in the order they were appended | The log is append-only; a single `Appender` with a `Mutex` serializes all writes; index offsets are monotonically increasing per aggregate |
| **Time-travel correctness** — `ReplayStateAt(..., targetTime, ...)` reflects exactly the events with `timestamp <= targetTime`, no more, no less | Replay stops the instant an event's timestamp exceeds `targetTime`; verified by `TestReplayStateAt_RespectsTimeTravel` |
| **Replay path equivalence** — indexed replay and full-log-scan replay always produce identical state | `TestReplayStateAt_IndexedAndFullScanAgree`; the index is a pure performance optimization, never a source of truth |
| **Concurrency safety** — no data races under concurrent `Append`/read | `Appender` writes are mutex-serialized, `Index` reads/writes are `RWMutex`-protected; verified with `go test -race` in CI |

### Not guaranteed

| Not guaranteed | Why it matters |
|---|---|
| **Cross-aggregate transactions** | Each `Append` commits one event to one aggregate. There is no way to atomically commit events to two different aggregates together — if you need that, coordinate it at the application layer. |
| **Tamper resistance** | CRC32 catches *accidental* corruption (disk errors, truncated writes). It is not a cryptographic signature — anyone with write access to the data files can forge a record with a valid checksum. Don't treat ReplayDB as an audit-proof ledger without adding your own signing layer. |
| **High availability / replication** | ReplayDB is a single process, single data directory. There's no clustering, replication, or failover. If the disk or the process dies, you're restoring from your own backup of `events.redb`/`snapshots.redb`. |
| **Global ordering across aggregates** | Ordering is only guaranteed *within* one `(kind, id)`. Two events for different aggregates appended "at the same time" from different goroutines have no defined relative order. |
| **Read isolation from in-flight writes** | There's no MVCC/snapshot isolation. A `ReplayStateAt` call running concurrently with an `Append` to the same aggregate may or may not include that event, depending on timing — it will never see a *partial* event (checksummed, all-or-nothing), but "before or after" isn't deterministic mid-flight. |
| **Backward-compatible binary format across major versions** | The on-disk format (magic bytes, field layout) isn't versioned yet. A future breaking format change would require a migration path that doesn't exist today — don't assume you can upgrade major versions in place without checking the changelog first. |
| **Retention / compaction** | The event log only grows. There's no TTL, deletion, or compaction of old events — plan disk capacity accordingly. |
| **Rate limiting** | Nothing stops a client from calling `Append` as fast as the network allows. Put a rate limiter in front if you're exposing ReplayDB to untrusted clients. |

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