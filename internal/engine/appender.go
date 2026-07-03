package engine

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/x-name15/replaydb/internal/metrics"
	"github.com/x-name15/replaydb/internal/storage"
)

// Appender is the thread-safe gateway to both the event log and the
// snapshot store. It also keeps an Index in sync incrementally, so lookups
// never need to re-scan the whole log after the initial Rebuild.
type Appender struct {
	eventsFile    *os.File
	snapshotsFile *os.File
	mutex         sync.Mutex
	index         *Index
	nextOffset    int64 // byte offset where the next event write will land
}

// NewAppender initializes the storage engine, prepares both file
// descriptors in append-only mode, and wires up index.
func NewAppender(dataDir string, index *Index) (*Appender, error) {
	eventsPath := filepath.Join(dataDir, "events.redb")
	snapshotsPath := filepath.Join(dataDir, "snapshots.redb")

	eFile, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	sFile, err := os.OpenFile(snapshotsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		eFile.Close()
		return nil, err
	}

	info, err := eFile.Stat()
	if err != nil {
		eFile.Close()
		sFile.Close()
		return nil, err
	}

	log.Printf("[engine] appender ready — events.redb=%s (%d bytes) snapshots.redb=%s\n", eventsPath, info.Size(), snapshotsPath)

	return &Appender{
		eventsFile:    eFile,
		snapshotsFile: sFile,
		index:         index,
		nextOffset:    info.Size(),
	}, nil
}

// Append encodes and durably writes a new event to the log, then records
// its offset in the Index so future replays can find it directly.
func (a *Appender) Append(kind, aggregateID, eventType string, payload []byte) error {
	start := time.Now()

	record := storage.EventRecord{
		Timestamp:     time.Now().UTC(),
		AggregateKind: kind,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       payload,
	}

	encoded, err := record.Encode()
	if err != nil {
		log.Printf("[APPEND] ✗ %s/%s %s — encode failed: %v\n", kind, aggregateID, eventType, err)
		metrics.RecordAppend(time.Since(start), err)
		return err
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	offset := a.nextOffset

	if _, err := a.eventsFile.Write(encoded); err != nil {
		log.Printf("[APPEND] ✗ %s/%s %s — write failed: %v\n", kind, aggregateID, eventType, err)
		metrics.RecordAppend(time.Since(start), err)
		return err
	}
	if err := a.eventsFile.Sync(); err != nil {
		log.Printf("[APPEND] ✗ %s/%s %s — fsync failed: %v\n", kind, aggregateID, eventType, err)
		metrics.RecordAppend(time.Since(start), err)
		return err
	}

	a.nextOffset += int64(len(encoded))

	if a.index != nil {
		a.index.Add(kind, aggregateID, offset)
	}

	log.Printf("[APPEND] ✓ %s/%s %s (offset=%d, %d bytes, %s)\n",
		kind, aggregateID, eventType, offset, len(encoded), time.Since(start))
	metrics.RecordAppend(time.Since(start), nil)

	return nil
}

// SaveSnapshot writes a materialized view of the state to disk.
func (a *Appender) SaveSnapshot(kind, aggregateID string, version uint32, stateJSON []byte) error {
	start := time.Now()

	record := storage.SnapshotRecord{
		Timestamp:     time.Now().UTC(),
		Version:       version,
		AggregateKind: kind,
		AggregateID:   aggregateID,
		StateJSON:     stateJSON,
	}

	encoded, err := record.Encode()
	if err != nil {
		log.Printf("[SNAPSHOT] ✗ %s/%s — encode failed: %v\n", kind, aggregateID, err)
		metrics.RecordSnapshot(err)
		return err
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	if _, err := a.snapshotsFile.Write(encoded); err != nil {
		log.Printf("[SNAPSHOT] ✗ %s/%s — write failed: %v\n", kind, aggregateID, err)
		metrics.RecordSnapshot(err)
		return err
	}
	if err := a.snapshotsFile.Sync(); err != nil {
		log.Printf("[SNAPSHOT] ✗ %s/%s — fsync failed: %v\n", kind, aggregateID, err)
		metrics.RecordSnapshot(err)
		return err
	}

	log.Printf("[SNAPSHOT] ✓ %s/%s @version=%d (%d bytes, %s)\n",
		kind, aggregateID, version, len(encoded), time.Since(start))
	metrics.RecordSnapshot(nil)

	return nil
}

// Close gracefully releases both file descriptors.
func (a *Appender) Close() error {
	log.Println("[engine] closing storage files...")
	a.eventsFile.Close()
	return a.snapshotsFile.Close()
}
