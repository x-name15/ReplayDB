# Changelog

All notable changes to ReplayDB are documented in this file.

## [1.1.1] - 2026-07-03 — Real-Time Event Streaming & Watchers

### Added

#### Engine & Storage
- Introduced a database-native Log Tailing mechanism for real-time event streaming.
  - `internal/engine/appender.go`: Added a global observer registry (`RegisterWatcher`, `RemoveWatcher`).
  - The engine now asynchronously broadcasts committed events to all active watchers immediately following a successful `fsync()`. This ensures zero-latency propagation without the complexity of an internal message broker.
#### Networking & Protocol
- Expanded the binary TCP protocol to support long-lived, stateful streaming connections.
  - `pkg/wire/protocol.go`: Introduced the new `OpWatch` (0x05) operation.
  - `cmd/redb`: The TCP server now supports persistent connections for `OpWatch` requests. It implements efficient tail filtering on the client's goroutine, routing only the requested `Kind` and `ID` events over the network to minimize bandwidth overhead.
#### SDK
- Exposed real-time subscription capabilities to Go developers.
  - `sdk/go/client.go`: Added the `Watch(ctx context.Context, kind, id string) (<-chan wire.BatchEvent, error)` method. This empowers consumers to listen to live event streams using idiomatic Go channels, automatically handling the underlying TCP stream decoding and context cancellation.

---
## [1.1.0] - 2026-07-03 — Official Go SDK & Public Protocol Exposure

### Added

#### Engine & Storage
- Introduced high-performance event batching to drastically reduce disk I/O bottlenecks.
  - `internal/engine/appender.go`: Added `AppendBatch(events)` which processes an array of events in memory and performs a single `fsync()` at the end of the operation, massively multiplying throughput for bulk inserts.

#### Networking & Protocol
- `pkg/wire/protocol.go`: Expanded the binary TCP protocol with a new `OpAppendBatch` (0x04) operation.
  - Added the `BatchEvent` struct to represent individual events within a batch frame.
  - The protocol now safely iterates and decodes variable-length batches while respecting existing memory boundaries (`maxFieldLen`).
- `cmd/redb`: The core TCP server loop now recognizes and efficiently processes incoming `OpAppendBatch` requests.

#### SDK
- Introduced the official Go SDK under `sdk/go/` featuring a clean, idiomatic, and context-aware `Client` interface.
  - `sdk/go/client.go`
    - Implemented `NewClient(cfg Config)` with support for network deadlines and configurable timeouts.
    - Added `Append(ctx, kind, id, eventType, payload)` for injecting runtime-safe immutable events.
    - Added `Travel(ctx, kind, id, at)` utilizing the RFC3339 time format over payload boundaries to reconstruct state at specific points in time.
    - Added `Snapshot(ctx, kind, id)` to programmatically trigger server-side state consolidation.
    - Errors across the entire SDK layer standardised to descriptive English text.
    - `sdk/go/client.go`: Exposed the new batching capability to external consumers via the `AppendBatch(ctx context.Context, events []wire.BatchEvent)` method.
#### Repository Management
- Added a unified Go workspace configuration (`go.work`) at the repository root.
  - Streamlines local multi-module development, enabling simultaneous tracking of the engine module (`.`) and the decoupled client SDK module (`./sdk/go`).

### Changed

#### Architecture & Refactoring
- Promoted the binary framing protocol out of the isolation layer to make it accessible to external consumers.
  - Moved `internal/wire/` to `pkg/wire/`.
  - Updated all internal package resolution rules across the core engine (`cmd/redb`), dashboard templates, and the interactive CLI tool (`cmd/recli`) to bind to the new public `pkg/wire` layout.


---
## [1.0.1] - 2026-07-03 — Major General Fixes for Stability

### Added

#### Security
- Added shared-token authentication for the TCP wire protocol.
  - `internal/wire/auth.go`
    - Introduced `WriteAuthToken` / `ReadAuthToken` using the existing framing protocol.
    - Added `TokensEqual` based on `subtle.ConstantTimeCompare` to mitigate timing attacks.
  - `cmd/redb/main.go`
    - Added the `REDB_AUTH_TOKEN` environment variable.
    - Connections must authenticate before sending `OpAppend`, `OpReplay`, or `OpSnapshot`.
    - Connections providing an invalid token or failing to authenticate before `connReadTimeout` are closed without processing requests.
  - `cmd/recli/helper/client.go`
    - The CLI automatically sends `REDB_AUTH_TOKEN` (when available) as the first frame of every connection.
  - Added a startup warning when the server is launched without `REDB_AUTH_TOKEN`, following the same pattern used for `REDB_DASHBOARD_USER` and `REDB_DASHBOARD_PASS`.
#### Configuration
- Added `internal/helper/env.go` with `GetEnvInt(key, fallback)` for safely reading integer environment variables.
- Added the following server configuration options:
  - `REDB_MAX_CONNECTIONS` (default: **500**).
  - `REDB_MAX_PAYLOAD_BYTES` (default: **4 MB**).
  - Payload limits are applied through `wire.SetMaxFieldLen`.
- `internal/wire/protocol.go`
  - Replaced the fixed `maxFieldLen` constant with a runtime-configurable value via `wire.SetMaxFieldLen(n)`.
  - The default remains **64 MB** (`defaultMaxFieldLen`) when unset or configured with `0`.
#### Networking
- Added connection limiting using a semaphore (`connSem`) in the accept loop.
  - Connections exceeding the configured limit are rejected immediately with an error response before being closed.
  - The `Accept()` loop remains non-blocking.
- Added startup logs displaying the configured connection limit and maximum payload size.
- Added a centralized `writeResponse(conn, resp)` helper that applies `conn.SetWriteDeadline` before every response write.
- Added a new `connWriteTimeout` constant (15 seconds).
#### Snapshot Indexing
- `internal/engine/index.go`
  - Added snapshot indexing through `snapshotOffsets`.
  - Introduced `AddSnapshot(kind, id, offset)` and `SnapshotOffsets(kind, id)`.
- Snapshot index rebuilding at startup.
  - `Rebuild()` now scans `snapshots.redb` during boot to populate the snapshot index.
  - Separate statistics are logged for aggregates, snapshots, and corrupt entries.
- `internal/engine/appender.go`
  - Added `nextSnapshotOffset`, initialized from the current size of `snapshots.redb`.
  - Every successful snapshot write automatically updates the snapshot index.
- `internal/engine/replay.go`
  - Added `latestIndexedSnapshot`, allowing replay to seek directly to indexed snapshot offsets instead of scanning the entire snapshot file.
#### Archiving
- Added `internal/engine/archiver.go`.
  - Introduced an `Archiver` that periodically mirrors appended data from `events.redb` and `snapshots.redb` into a separate archive directory.
  - The archiver is append-only and never modifies, truncates, or deletes live data.
  - Resumes automatically from the destination file size, making interrupted runs safe without requiring additional metadata.
- Added archive configuration:
  - `REDB_ARCHIVE_DIR`
  - `REDB_ARCHIVE_INTERVAL` (Go duration format, e.g. `6h`, `24h`)
  - When configured, the archiver starts automatically, performs an immediate synchronization, and continues at the configured interval.
- Added graceful shutdown support.
  - On `SIGINT` or `SIGTERM`, one final archive cycle is executed before the process exits.
- Added boot logging indicating whether archiving is enabled and showing the configured archive interval.

### Changed

#### Security
- `handleConnection` now accepts an additional `authToken string` parameter.
#### Configuration
- Reduced the default maximum payload size from **64 MB** to **4 MB** to lower the per-connection memory footprint.
- Payload size remains fully configurable and can be increased beyond **64 MB** if required.
#### Networking
- All responses (`OpAppend`, `OpReplay`, `OpSnapshot`, and error responses) are now written through `writeResponse`, ensuring every write operation is protected by a write deadline.
- Write failures are now logged instead of being silently ignored, improving visibility into slow or disconnected clients.
#### Replay
- `ReplayStateAt` now prefers indexed snapshot lookups whenever snapshot offsets are available.
- The previous full scan of `snapshots.redb` is retained as a fallback when no snapshot index exists.

### Documentation
- Updated `.env.example` to document:
  - `REDB_AUTH_TOKEN`
  - `REDB_MAX_CONNECTIONS`
  - `REDB_MAX_PAYLOAD_BYTES`
  - `REDB_ARCHIVE_DIR`
  - `REDB_ARCHIVE_INTERVAL`
- Archive configuration documentation explicitly states that the feature is intended for backup purposes only and never modifies the source files.
- **Recommendation:** Update the README to advise configuring `REDB_AUTH_TOKEN` before exposing `REDB_PORT` outside localhost or another trusted network.

---
## [1.0.0] - 2026-07-03 — Stable Release

### Added
- `internal/domain`: generic `Aggregate` interface and `Registry`, so ReplayDB no longer assumes a specific domain (e.g. `Order`) and any consumer can register their own.
- `internal/wire`: binary, length-prefixed TCP wire protocol, replacing the previous pipe-delimited text protocol (which broke on payloads containing `|`).
- `internal/engine/index.go`: in-memory `(kind, id) → offsets` index. `ReplayStateAt` seeks directly to an aggregate's events instead of scanning the entire log; falls back to a full scan when no index is available.
- `internal/server`: HTTP dashboard with optional HTTP Basic Auth (`REDB_DASHBOARD_USER` / `REDB_DASHBOARD_PASS`), request timeouts, and the HTML template extracted to `templates/dashboard.html` (embedded via `go:embed`, keeping the binary self-contained).
- `cmd/redb`: graceful shutdown on `SIGINT`/`SIGTERM` — waits for in-flight connections before exiting instead of dropping them.
- `cmd/redb`: per-connection read deadline (30s) on the TCP wire server, preventing a slow or idle client from holding a goroutine/file descriptor open indefinitely.
- `internal/storage`: every `EventRecord`/`SnapshotRecord` now carries a CRC32 checksum; corrupted records are detected and skipped during replay instead of silently propagating garbage.
- `internal/engine/appender.go`: explicit `fsync()` after every write, so a committed event is durable on disk before the caller gets an `OK`.
- `internal/metrics`: hand-written Prometheus text-exposition format, zero dependencies (no client library). Exposed at `/metrics` on the same HTTP server as the dashboard. Tracks `Append`/`ReplayStateAt`/`SaveSnapshot` counts, errors, and average latency; which replay path was taken (indexed vs. full-scan); active/total TCP connections; on-disk log sizes; and the number of distinct aggregates tracked by the in-memory index.
- `cmd/redb`, `internal/engine`: structured terminal logging (`[boot]`, `[conn]`, `[APPEND]`, `[TRAVEL]`, `[SNAPSHOT]`, `[INDEX]`) for every core operation — connection lifecycle, each append/replay/snapshot with duration and outcome, and index rebuild summary at startup.

### Fixed
- Dashboard error/state output goes through `html/template`'s automatic escaping (no reflected-XSS surface via the `kind`/`id` query params).
- Basic Auth credential comparison uses `crypto/subtle.ConstantTimeCompare` to avoid leaking credential length/content via timing.
- `/metrics` is unauthenticated by default (Prometheus convention, scraped from a trusted network) — put it behind a proxy/firewall the same way as `REDB_DASHBOARD_*` if exposing beyond localhost.

### Documentation
- `docs/DOCUMENTATION.md`: explicit "Guaranteed / Not guaranteed" tables replacing implied behavior — durability, corruption detection, per-aggregate ordering, and time-travel correctness on one side; no cross-aggregate transactions, no tamper resistance, no HA/replication, no retention, and no rate limiting on the other.

### Known limitations
- `Index.Rebuild()` is a synchronous full-log scan at startup; on very large logs this adds to boot time. No persisted index format yet.
- No connection/request rate-limiting at the application layer.
- CRC32 protects against accidental corruption, not deliberate tampering.

---
## [0.1.0] - 2026-07-02 — Initial event store core and Release of ReplayDB

### Added

- `domain`: introduced the `Aggregate` interface and `Registry` — the engine no longer assumes a specific aggregate type; consumers register their own. `order.go` rewritten as a reference implementation instead of a hardcoded case.
- `storage`: added CRC32 checksums and magic-byte tagging to every record in `log.go` and `snapshot.go`, so on-disk corruption is detected rather than silently replayed as valid data.
- `engine`: added explicit `fsync()` after every write in `appender.go` for durability. Added an in-memory `(kind, id) → []offset` index (`index.go`), built once at boot (`Rebuild`) and maintained incrementally on every `Append`; `ReplayStateAt` now seeks directly via the index instead of a full-log scan, with a safe full-scan fallback when no index is present.
- `server`: added opt-in HTTP Basic Auth on the dashboard via `REDB_DASHBOARD_USER`/`REDB_DASHBOARD_PASS` (constant-time comparison), inactive by default so local dev stays frictionless.
- `internal/tests`: centralized test suite as an external package (`package tests`) exercising only the public API — binary roundtrip, CRC32 corruption handling, index-vs-full-scan consistency, exact time-travel semantics, and unknown kind/aggregate error paths.
- `internal/benchmarks`: centralized benchmark suite, same external-package convention as `internal/tests`. Covers event/snapshot encode-decode cost (including a 64KB-payload case), real `Append` throughput (fsync included, not skipped), index add/lookup, indexed-vs-full-scan replay under load, and wire protocol framing cost.
- `repo`: added `.env.example` documenting every environment variable the codebase actually reads (`REDB_PORT`, `REDB_HTTP_PORT`, `REDB_DATA_DIR`, `REDB_DASHBOARD_USER`, `REDB_DASHBOARD_PASS`); added `.devcontainer/` (Go 1.26 Alpine image matching TinyMQ's dev-environment pattern, ports 7800/8080 forwarded).
- `repo`: added `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, and `SECURITY.md`, adapted to ReplayDB's actual architecture and scope (event/snapshot integrity, wire protocol parsing, dashboard auth, time-travel correctness as a data-integrity concern) rather than copied verbatim from TinyMQ.

### Changed

- `wire`: introduced a length-prefixed binary TCP protocol (`[uint32 len][bytes]`) replacing the old `"APPEND|id|type|payload"` text format, which broke whenever a JSON payload contained a `|`. Frame and field lengths are bounded by `maxFieldLen` (64MB) to prevent unbounded allocation from a malformed frame.
- `server`: extracted the dashboard HTML into `templates/dashboard.html`, still embedded into the binary via `//go:embed` — single-binary distribution preserved.
- `cmd/recli`: restructured the CLI into subpackages (`main.go` dispatcher, `helper/client.go` shared wire client, `helper/travel.go` for reads, `db/append.go` and `db/snapshot.go` for writes), with named flags (`--kind`, `--id`, `--type`, `--payload`) replacing positional args.
- `docker-compose`: added optional `env_file: ./.env` support (values in `environment:` still take precedence) and documented the dashboard-auth variables as a commented-out opt-in block.

### Fixed

- `storage`: bound-checked all length-prefixed fields (`kindLen`, `idLen`, `typeLen`/`eventLen`, `payloadLen`, `stateLen`) before allocation in `log.go` and `snapshot.go`, so a corrupted or truncated file can no longer force an unbounded `make([]byte, n)` before the CRC32 check runs. All `binary.Read`/`binary.Write` calls now return checked errors instead of being silently ignored.
- `server`: replaced the bare `http.ListenAndServe` with an explicit `http.Server` carrying `ReadTimeout`/`ReadHeaderTimeout`/`WriteTimeout`/`IdleTimeout`, closing a slowloris-style DoS window. Template execution errors are now logged instead of swallowed.

### Documentation

- `repo`: started `README.md`; added `docs/DOCUMENTATION.md` with the deep-dive architecture reference (on-disk binary format, per-field size limits, exact time-travel/snapshot semantics, benchmark rationale) moved out of the README to keep it as a short landing page.

### CI/CD

- added `.github/workflows/entry.yaml` (build/test/vet/fmt gate, cross-platform builds for Linux/Windows/macOS amd64+arm64, benchmark run against `internal/benchmarks`, Docker image size check capped at 50MB) and `.github/workflows/release.yaml` (CHANGELOG-driven GitHub Release + multi-arch image push to GHCR and Docker Hub), modeled on TinyMQ's release pipeline.

### Community & Repository Management

- **Contributing Guidelines:** Added `CONTRIBUTING.md` to establish clear workflows for local development, testing, and PR submissions.
- **Code of Conduct:** Added a pragmatically tailored `CODE_OF_CONDUCT.md` to protect maintainer bandwidth and ensure technical discussions remain respectful and productive.
- **Security Policy:** Added `SECURITY.md` describing the project's vulnerability reporting process, supported versions, disclosure expectations, and response policy.