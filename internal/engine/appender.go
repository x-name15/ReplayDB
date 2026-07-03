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

type Appender struct {
	eventsFile         *os.File
	snapshotsFile      *os.File
	mutex              sync.Mutex
	index              *Index
	nextOffset         int64
	nextSnapshotOffset int64
}

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
	snapInfo, err := sFile.Stat()
	if err != nil {
		eFile.Close()
		sFile.Close()
		return nil, err
	}
	log.Printf("[engine] appender ready — events.redb=%s (%d bytes) snapshots.redb=%s\n", eventsPath, info.Size(), snapshotsPath)
	return &Appender{
		eventsFile:         eFile,
		snapshotsFile:      sFile,
		index:              index,
		nextOffset:         info.Size(),
		nextSnapshotOffset: snapInfo.Size(),
	}, nil
}

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
	offset := a.nextSnapshotOffset
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
	a.nextSnapshotOffset += int64(len(encoded))
	if a.index != nil {
		a.index.AddSnapshot(kind, aggregateID, offset)
	}
	log.Printf("[SNAPSHOT] ✓ %s/%s @version=%d (%d bytes, %s)\n",
		kind, aggregateID, version, len(encoded), time.Since(start))
	metrics.RecordSnapshot(nil)
	return nil
}

func (a *Appender) Close() error {
	log.Println("[engine] closing storage files...")
	a.eventsFile.Close()
	return a.snapshotsFile.Close()
}
