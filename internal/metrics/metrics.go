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
	snapshotTotal       atomic.Int64
	snapshotErrors      atomic.Int64
	connectionsOpened   atomic.Int64
	connectionsActive   atomic.Int64
)

type IndexSizeFunc func() int

type Stats struct {
	AppendTotal         int64
	AppendErrors        int64
	AppendAvgMs         float64
	TravelTotal         int64
	TravelErrors        int64
	TravelAvgMs         float64
	TravelIndexedTotal  int64
	TravelFullScanTotal int64
	SnapshotTotal       int64
	SnapshotErrors      int64
	ConnectionsOpened   int64
	ConnectionsActive   int64
	EventsLogBytes      int64
	SnapshotsLogBytes   int64
	IndexAggregates     int
}

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

func Snapshot(dataDir string, indexSize IndexSizeFunc) Stats {
	idxSize := 0
	if indexSize != nil {
		idxSize = indexSize()
	}
	return Stats{
		AppendTotal:         appendTotal.Load(),
		AppendErrors:        appendErrors.Load(),
		AppendAvgMs:         avgSeconds(appendDurationNanos.Load(), appendTotal.Load()) * 1000,
		TravelTotal:         travelTotal.Load(),
		TravelErrors:        travelErrors.Load(),
		TravelAvgMs:         avgSeconds(travelDurationNanos.Load(), travelTotal.Load()) * 1000,
		TravelIndexedTotal:  travelIndexedTotal.Load(),
		TravelFullScanTotal: travelFullScanTotal.Load(),
		SnapshotTotal:       snapshotTotal.Load(),
		SnapshotErrors:      snapshotErrors.Load(),
		ConnectionsOpened:   connectionsOpened.Load(),
		ConnectionsActive:   connectionsActive.Load(),
		EventsLogBytes:      fileSize(filepath.Join(dataDir, "events.redb")),
		SnapshotsLogBytes:   fileSize(filepath.Join(dataDir, "snapshots.redb")),
		IndexAggregates:     idxSize,
	}
}

func Handler(dataDir string, indexSize IndexSizeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writePrometheusMetrics(w, dataDir, indexSize)
	}
}

func writePrometheusMetrics(w io.Writer, dataDir string, indexSize IndexSizeFunc) {
	s := Snapshot(dataDir, indexSize)
	counter(w, "replaydb_append_total", "Total number of Append calls.", float64(s.AppendTotal))
	counter(w, "replaydb_append_errors_total", "Total number of failed Append calls.", float64(s.AppendErrors))
	gauge(w, "replaydb_append_duration_seconds_avg", "Average Append latency, including fsync.", s.AppendAvgMs/1000)
	counter(w, "replaydb_travel_total", "Total number of ReplayStateAt (time-travel) calls.", float64(s.TravelTotal))
	counter(w, "replaydb_travel_errors_total", "Total number of failed ReplayStateAt calls.", float64(s.TravelErrors))
	gauge(w, "replaydb_travel_duration_seconds_avg", "Average ReplayStateAt latency.", s.TravelAvgMs/1000)
	counter(w, "replaydb_travel_indexed_total", "Time-travel replays that used the in-memory index.", float64(s.TravelIndexedTotal))
	counter(w, "replaydb_travel_fullscan_total", "Time-travel replays that fell back to a full log scan.", float64(s.TravelFullScanTotal))
	counter(w, "replaydb_snapshot_total", "Total number of SaveSnapshot calls.", float64(s.SnapshotTotal))
	counter(w, "replaydb_snapshot_errors_total", "Total number of failed SaveSnapshot calls.", float64(s.SnapshotErrors))
	counter(w, "replaydb_connections_opened_total", "Total TCP wire connections accepted since boot.", float64(s.ConnectionsOpened))
	gauge(w, "replaydb_connections_active", "TCP wire connections currently open.", float64(s.ConnectionsActive))
	gauge(w, "replaydb_events_log_bytes", "Size of events.redb in bytes.", float64(s.EventsLogBytes))
	gauge(w, "replaydb_snapshots_log_bytes", "Size of snapshots.redb in bytes.", float64(s.SnapshotsLogBytes))
	if indexSize != nil {
		gauge(w, "replaydb_index_aggregates", "Distinct aggregates currently tracked by the in-memory index.", float64(s.IndexAggregates))
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
