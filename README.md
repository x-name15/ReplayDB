# 🕰️ ReplayDB

[![Release](https://img.shields.io/badge/Release-v1.0.0-green?style=flat-square)](https://github.com/x-name15/ReplayDB/releases)
[![Go Version](https://img.shields.io/badge/Go-1.25.9-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-GPLv3-blue?style=flat-square)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/x-name15/ReplayDB/release.yaml?style=flat-square&logo=githubactions&logoColor=white)](https://github.com/x-name15/ReplayDB/actions)
[![Docker Image Size](https://img.shields.io/badge/Image-%3C50MB-informational?style=flat-square&logo=docker)](https://github.com/x-name15/replaydb/pkgs/container/replaydb)

**A single-binary, zero-dependency Event Store with Time-Travel replay.**

ReplayDB stores every event that ever happened to your aggregates and lets you ask: *"What did this look like at any point in time?"* No external database, no message broker, and no configuration files. Just one binary, sensible defaults, and everything else configured through environment variables.

---

## Why ReplayDB

- **Time-Travel replay** — reconstruct the exact state of any aggregate at any point in time, not just its current state.
- **Domain-agnostic** — the engine knows nothing about "Order" or any other business concept. Register your own aggregate types and ReplayDB will replay them.
- **Single binary, zero dependencies** — built entirely with the Go standard library. No Cobra, ORMs, or third-party wire protocols. Run `go build` and you're done.
- **Binary wire protocol** — length-prefixed TCP framing between `recli` and `redb`, avoiding fragile text parsing that breaks when JSON payloads contain `|`.
- **Corruption-safe storage** — every record is protected with magic bytes and a CRC32 checksum. Corruption is detected rather than silently replayed.
- **In-memory index** — `(kind, id) → offsets` is built at startup and updated incrementally, allowing direct seeks instead of full-log scans. If unavailable, ReplayDB safely falls back to a full scan.
- **Environment-driven configuration** — ports, data paths, and authentication are configured through environment variables instead of being hardcoded.

---

## Quick Start

### Docker (recommended)

```bash
docker run -d \
  --name replaydb \
  -p 7800:7800 \
  -p 8080:8080 \
  -v $(pwd)/data:/home/redb/data \
  ghcr.io/x-name15/replaydb:latest
```

Or pull from Docker Hub:

```bash
docker pull flez71/replaydb:latest
```

Or use Docker Compose (see [`docker-compose.yml`](./docker-compose.yml)):

```bash
docker compose up -d
```

### From source

```bash
git clone https://github.com/x-name15/replaydb.git
cd replaydb
make build
./bin/replaydb
```

Requires Go 1.25.9 or newer. No additional dependencies.

---

## Usage

ReplayDB provides two binaries:

- **`redb`** — the server (event store, TCP wire protocol, and optional HTTP dashboard).
- **`recli`** — the command-line client.

### Append an event

```bash
recli append \
  --kind order \
  --id order-123 \
  --type OrderCreated \
  --payload '{"total": 42.50, "currency": "USD"}'
```

### Replay state at a specific point in time

```bash
recli travel \
  --kind order \
  --id order-123 \
  --at "2026-06-15T10:00:00Z"
```

Omit `--at` (or use `now`) to retrieve the current state.

### Create a snapshot

```bash
recli snapshot \
  --kind order \
  --id order-123
```

Snapshots act as checkpoints that speed up future replays.

### Dashboard

Once `redb` is running, open:

```text
http://localhost:8080
```

The dashboard displays log and snapshot statistics and lets you inspect aggregate state by `kind` and `id`.

---

## Configuration

ReplayDB is configured entirely through environment variables.

| Variable | Default | Description |
|----------|---------|-------------|
| `REDB_PORT` | `:7800` | TCP port used by the binary wire protocol (`recli`). |
| `REDB_HTTP_PORT` | `:8080` | HTTP dashboard port. |
| `REDB_DATA_DIR` | `/home/redb/data` | Directory containing `events.redb` and `snapshots.redb`. |
| `REDB_DASHBOARD_USER` | *(unset)* | Enables HTTP Basic Authentication when set together with `REDB_DASHBOARD_PASS`. |
| `REDB_DASHBOARD_PASS` | *(unset)* | Password for dashboard authentication. If either variable is unset, the dashboard is public (acceptable for local development only). |

---

## Architecture

```text
cmd/
  redb/       → server entrypoint
  recli/      → CLI client (subpackages: db/, helper/)
internal/
  domain/       → Aggregate interface + Registry
  engine/       → append logic, in-memory index, time-travel replay
  storage/      → binary log + snapshot encoding, CRC32-checked
  wire/         → length-prefixed binary TCP protocol
  server/       → HTTP dashboard
  tests/        → centralized test suite (external package, public API only)
  benchmarks/   → centralized benchmark suite
```

For details about the on-disk format, integrity guarantees, replay semantics, and benchmark methodology, see [`docs/DOCUMENTATION.md`](./docs/DOCUMENTATION.md).

---

## Development

```bash
make fmt
make test
make bench
make build
make clean
```

CI runs:

- `go fmt`
- `go vet`
- race-detector tests
- cross-platform builds (Linux, macOS, Windows; amd64 and arm64)
- Docker image size verification

Releases are generated automatically from [`CHANGELOG.md`](./CHANGELOG.md).

---

## Status

ReplayDB is still in its early stages. The storage engine, wire protocol, indexing, and CLI are implemented and tested, but the project has not yet been validated under real production workloads.

Treat ReplayDB as **pre-1.0**. The on-disk format and wire protocol may evolve until the first stable `v1.0.0` release.

---

## License

ReplayDB is licensed under the GPL v3. See [`LICENSE`](./LICENSE) for details.

## Credits

**Author:** Mr Jacket / Felix Manrique