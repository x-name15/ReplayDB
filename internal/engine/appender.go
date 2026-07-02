package engine

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/x-name15/replaydb/internal/storage"
)

// Appender acts as the thread-safe gateway to both the event log and the snapshot store.
type Appender struct {
	eventsFile    *os.File
	snapshotsFile *os.File
	mutex         sync.Mutex
}

// NewAppender initializes the storage engine locks and prepares both file descriptors.
func NewAppender(dataDir string) (*Appender, error) {
	eventsPath := filepath.Join(dataDir, "events.redb")
	snapshotsPath := filepath.Join(dataDir, "snapshots.redb")

	eFile, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	sFile, err := os.OpenFile(snapshotsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		eFile.Close() // Rollback if second file fails
		return nil, err
	}

	return &Appender{
		eventsFile:    eFile,
		snapshotsFile: sFile,
	}, nil
}

// Append securely encodes and writes a new event to the binary log stream.
func (a *Appender) Append(aggregateID, eventType string, payload []byte) error {
	record := storage.EventRecord{
		Timestamp:   time.Now().UTC(),
		AggregateID: aggregateID,
		EventType:   eventType,
		Payload:     payload,
	}

	bytes, err := record.Encode()
	if err != nil {
		return err
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	if _, err := a.eventsFile.Write(bytes); err != nil {
		return err
	}

	return nil
}

// SaveSnapshot securely writes a materialized view of the state to disk.
func (a *Appender) SaveSnapshot(aggregateID string, version uint32, stateJSON []byte) error {
	record := storage.SnapshotRecord{
		Timestamp:   time.Now().UTC(),
		Version:     version,
		AggregateID: aggregateID,
		StateJSON:   stateJSON,
	}

	bytes, err := record.Encode()
	if err != nil {
		return err
	}

	// We use the same mutex to ensure overall storage engine stability
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if _, err := a.snapshotsFile.Write(bytes); err != nil {
		return err
	}

	return nil
}

// Close gracefully releases both file descriptors.
func (a *Appender) Close() error {
	a.eventsFile.Close()
	return a.snapshotsFile.Close()
}