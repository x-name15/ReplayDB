package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/storage"
)

// ReplayStateAt reconstructs the state of an entity, optimizing the load via Snapshots
func ReplayStateAt(dataDir string, aggregateID string, targetTime time.Time) (*domain.OrderState, error) {
	eventsPath := filepath.Join(dataDir, "events.redb")
	snapshotsPath := filepath.Join(dataDir, "snapshots.redb")

	// 1. PHASE 1: Attempt to load the most recent valid Snapshot
	state := domain.NewOrderState(aggregateID)
	var eventsToSkip uint32 = 0

	snapFile, err := os.Open(snapshotsPath)
	if err == nil {
		defer snapFile.Close()
		for {
			snap, err := storage.DecodeNextSnapshot(snapFile)
			if err == storage.ErrCorruptSnapshot {
				break // If corrupt or empty, we just fallback to full event replay
			}
			if err != nil {
				break // EOF
			}

			// Keep updating our state with the latest snapshot that happened BEFORE our targetTime
			if snap.AggregateID == aggregateID && !snap.Timestamp.After(targetTime) {
				json.Unmarshal(snap.StateJSON, state)
				eventsToSkip = snap.Version
			}
		}
	}

	// 2. PHASE 2: Open Event Log and resume stream from where the snapshot left off
	file, err := os.Open(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("engine failed to open storage log: %w", err)
	}
	defer file.Close()

	var eventsSkipped uint32 = 0
	eventsApplied := 0

	for {
		record, err := storage.DecodeNext(file)
		if err == storage.ErrEOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("engine stream decode error: %w", err)
		}

		if record.AggregateID != aggregateID {
			continue
		}

		if record.Timestamp.After(targetTime) {
			break // Reached the Time-Travel target
		}

		// Fast-Forward Optimization: Skip events already processed by the snapshot
		if eventsSkipped < eventsToSkip {
			eventsSkipped++
			continue
		}

		// Apply fresh historical events
		if err := state.Apply(record.EventType, record.Payload, record.Timestamp); err != nil {
			return nil, fmt.Errorf("engine domain apply panic on event %s: %w", record.EventType, err)
		}
		eventsApplied++
	}

	if eventsApplied == 0 && eventsToSkip == 0 {
		return nil, fmt.Errorf("no historical records found for entity '%s'", aggregateID)
	}

	return state, nil
}