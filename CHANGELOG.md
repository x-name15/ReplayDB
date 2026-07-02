# Changelog

All notable changes to ReplayDB are documented in this file.

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