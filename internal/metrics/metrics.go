// Package metrics tracks ReplayDB's operational counters and exposes them
// in the Prometheus text exposition format — hand-written, not via the
// official client library, so this stays zero-dependency like the rest of
// ReplayDB while remaining scrape-compatible with real Prometheus/Grafana.
//
// The format itself is a simple, stable text spec:
// https://prometheus.io/docs/instrumenting/exposition_formats/
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

var (
	appendTotal         atomic.Int64
	appendErrors        atomic.Int64
	appendDurationNanos atomic.Int64

	travelTotal         atomic.Int64
	travelErrors        atomic.Int64
	travelDurationNanos atomic.Int64
	travelIndexedTotal  atomic.Int64
	travelFullScanTotal atomic.Int64

	snapshotTotal  atomic.Int64
	snapshotErrors atomic.Int64

	connectionsOpened atomic.Int64
	connectionsActive atomic.Int64
)

func RecordAppend(duration time.Duration, err error) {
	appendTotal.Add(1)
	appendDurationNanos.Add(duration.Nanoseconds())
	if err != nil {
		appendErrors.Add(1)
	}
}

func RecordTravel(duration time.Duration, indexed bool, err error) {
	travelTotal.Add(1)
	travelDurationNanos.Add(duration.Nanoseconds())
	if err != nil {
		travelErrors.Add(1)
		return
	}
	if indexed {
		travelIndexedTotal.Add(1)
	} else {
		travelFullScanTotal.Add(1)
	}
}

func RecordSnapshot(err error) {
	snapshotTotal.Add(1)
	if err != nil {
		snapshotErrors.Add(1)
	}
}

func ConnOpened() {
	connectionsOpened.Add(1)
	connectionsActive.Add(1)
}

func ConnClosed() {
	connectionsActive.Add(-1)
}

type IndexSizeFunc func() int

func Handler(dataDir string, indexSize IndexSizeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writePrometheusMetrics(w, dataDir, indexSize)
	}
}

func writePrometheusMetrics(w io.Writer, dataDir string, indexSize IndexSizeFunc) {
	counter(w, "replaydb_append_total", "Total number of Append calls.", float64(appendTotal.Load()))
	counter(w, "replaydb_append_errors_total", "Total number of failed Append calls.", float64(appendErrors.Load()))
	gauge(w, "replaydb_append_duration_seconds_avg", "Average Append latency, including fsync.", avgSeconds(appendDurationNanos.Load(), appendTotal.Load()))

	counter(w, "replaydb_travel_total", "Total number of ReplayStateAt (time-travel) calls.", float64(travelTotal.Load()))
	counter(w, "replaydb_travel_errors_total", "Total number of failed ReplayStateAt calls.", float64(travelErrors.Load()))
	gauge(w, "replaydb_travel_duration_seconds_avg", "Average ReplayStateAt latency.", avgSeconds(travelDurationNanos.Load(), travelTotal.Load()))
	counter(w, "replaydb_travel_indexed_total", "Time-travel replays that used the in-memory index.", float64(travelIndexedTotal.Load()))
	counter(w, "replaydb_travel_fullscan_total", "Time-travel replays that fell back to a full log scan.", float64(travelFullScanTotal.Load()))

	counter(w, "replaydb_snapshot_total", "Total number of SaveSnapshot calls.", float64(snapshotTotal.Load()))
	counter(w, "replaydb_snapshot_errors_total", "Total number of failed SaveSnapshot calls.", float64(snapshotErrors.Load()))

	counter(w, "replaydb_connections_opened_total", "Total TCP wire connections accepted since boot.", float64(connectionsOpened.Load()))
	gauge(w, "replaydb_connections_active", "TCP wire connections currently open.", float64(connectionsActive.Load()))

	gauge(w, "replaydb_events_log_bytes", "Size of events.redb in bytes.", float64(fileSize(filepath.Join(dataDir, "events.redb"))))
	gauge(w, "replaydb_snapshots_log_bytes", "Size of snapshots.redb in bytes.", float64(fileSize(filepath.Join(dataDir, "snapshots.redb"))))

	if indexSize != nil {
		gauge(w, "replaydb_index_aggregates", "Distinct aggregates currently tracked by the in-memory index.", float64(indexSize()))
	}
}

func avgSeconds(sumNanos, count int64) float64 {
	if count == 0 {
		return 0
	}
	return (float64(sumNanos) / float64(count)) / 1e9
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func counter(w io.Writer, name, help string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %v\n", name, help, name, name, value)
}

func gauge(w io.Writer, name, help string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", name, help, name, name, value)
}
