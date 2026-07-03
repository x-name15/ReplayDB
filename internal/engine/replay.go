package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/metrics"
	"github.com/x-name15/replaydb/internal/storage"
)

// ReplayStateAt reconstructs the state of any registered aggregate kind at
// a target point in time. See replayIndexed vs replayFullScan for the two
// possible execution paths.
func ReplayStateAt(dataDir, kind, aggregateID string, targetTime time.Time, registry *domain.Registry, index *Index) (domain.Aggregate, error) {
	start := time.Now()
	eventsPath := filepath.Join(dataDir, "events.redb")
	snapshotsPath := filepath.Join(dataDir, "snapshots.redb")

	state, err := registry.New(kind, aggregateID)
	if err != nil {
		log.Printf("[TRAVEL] ✗ %s/%s — unknown kind: %v\n", kind, aggregateID, err)
		metrics.RecordTravel(time.Since(start), false, err)
		return nil, err
	}

	var eventsToSkip uint32 = 0
	usedSnapshot := false

	snapFile, err := os.Open(snapshotsPath)
	if err == nil {
		defer snapFile.Close()
		for {
			snap, err := storage.DecodeNextSnapshot(snapFile)
			if err == storage.ErrSnapshotChecksumMismatch {
				log.Printf("[TRAVEL] ⚠ %s/%s — corrupt snapshot skipped\n", kind, aggregateID)
				continue
			}
			if err != nil {
				break
			}
			if snap.AggregateKind == kind && snap.AggregateID == aggregateID && !snap.Timestamp.After(targetTime) {
				if err := json.Unmarshal(snap.StateJSON, state); err != nil {
					return nil, fmt.Errorf("engine: failed to unmarshal snapshot for %q: %w", aggregateID, err)
				}
				eventsToSkip = snap.Version
				usedSnapshot = true
			}
		}
	}

	var result domain.Aggregate
	path := "full-scan"

	if index != nil {
		if offsets := index.Offsets(kind, aggregateID); len(offsets) > 0 {
			path = "indexed"
			result, err = replayIndexed(eventsPath, offsets, state, targetTime, eventsToSkip)
		}
	}
	if result == nil && err == nil {
		result, err = replayFullScan(eventsPath, kind, aggregateID, state, targetTime, eventsToSkip)
	}

	if err != nil {
		log.Printf("[TRAVEL] ✗ %s/%s @%s (%s) — %v\n", kind, aggregateID, targetTime.Format(time.RFC3339), path, err)
		metrics.RecordTravel(time.Since(start), path == "indexed", err)
		return nil, err
	}

	log.Printf("[TRAVEL] ✓ %s/%s @%s — path=%s snapshot=%v version=%d (%s)\n",
		kind, aggregateID, targetTime.Format(time.RFC3339), path, usedSnapshot, result.Version(), time.Since(start))
	metrics.RecordTravel(time.Since(start), path == "indexed", nil)

	return result, nil
}

// replayIndexed applies events by seeking directly to each known offset,
// skipping everything else in the log entirely.
func replayIndexed(eventsPath string, offsets []int64, state domain.Aggregate, targetTime time.Time, eventsToSkip uint32) (domain.Aggregate, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("engine: failed to open storage log: %w", err)
	}
	defer file.Close()

	var eventsSkipped uint32
	eventsApplied := 0

	for _, offset := range offsets {
		if _, err := file.Seek(offset, os.SEEK_SET); err != nil {
			return nil, fmt.Errorf("engine: failed to seek to indexed offset %d: %w", offset, err)
		}

		record, err := storage.DecodeNext(file)
		if err == storage.ErrChecksumMismatch {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("engine: stream decode error at indexed offset %d: %w", offset, err)
		}

		if record.Timestamp.After(targetTime) {
			break
		}

		if eventsSkipped < eventsToSkip {
			eventsSkipped++
			continue
		}

		if err := state.Apply(record.EventType, record.Payload, record.Timestamp); err != nil {
			return nil, fmt.Errorf("engine: domain apply failed on event %q: %w", record.EventType, err)
		}
		eventsApplied++
	}

	if eventsApplied == 0 && eventsToSkip == 0 {
		return nil, fmt.Errorf("engine: no historical records found for indexed aggregate")
	}

	return state, nil
}

// replayFullScan is the linear-scan path, used as a correctness fallback
// for when no index is available yet.
func replayFullScan(eventsPath, kind, aggregateID string, state domain.Aggregate, targetTime time.Time, eventsToSkip uint32) (domain.Aggregate, error) {
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("engine: failed to open storage log: %w", err)
	}
	defer file.Close()

	var eventsSkipped uint32
	eventsApplied := 0

	for {
		record, err := storage.DecodeNext(file)
		if err == storage.ErrEOF {
			break
		}
		if err == storage.ErrChecksumMismatch {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("engine: stream decode error: %w", err)
		}

		if record.AggregateKind != kind || record.AggregateID != aggregateID {
			continue
		}
		if record.Timestamp.After(targetTime) {
			break
		}
		if eventsSkipped < eventsToSkip {
			eventsSkipped++
			continue
		}

		if err := state.Apply(record.EventType, record.Payload, record.Timestamp); err != nil {
			return nil, fmt.Errorf("engine: domain apply failed on event %q: %w", record.EventType, err)
		}
		eventsApplied++
	}

	if eventsApplied == 0 && eventsToSkip == 0 {
		return nil, fmt.Errorf("engine: no historical records found for %q/%q", kind, aggregateID)
	}

	return state, nil
}
