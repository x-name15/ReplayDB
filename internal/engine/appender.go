package engine

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/x-name15/replaydb/internal/metrics"
	"github.com/x-name15/replaydb/internal/storage"
	"github.com/x-name15/replaydb/pkg/wire"
)

type Appender struct {
	eventsFile         *os.File
	snapshotsFile      *os.File
	mutex              sync.Mutex
	index              *Index
	nextOffset         int64
	nextSnapshotOffset int64
	muWatchers         sync.RWMutex
	watchers           []chan wire.BatchEvent
	compactMu          sync.Mutex
	lastCompaction     atomic.Pointer[CompactionInfo]
}

func (a *Appender) LastCompaction() *CompactionInfo {
	return a.lastCompaction.Load()
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

func (a *Appender) RegisterWatcher(ch chan wire.BatchEvent) {
	a.muWatchers.Lock()
	a.watchers = append(a.watchers, ch)
	a.muWatchers.Unlock()
}

func (a *Appender) RemoveWatcher(ch chan wire.BatchEvent) {
	a.muWatchers.Lock()
	defer a.muWatchers.Unlock()
	for i, w := range a.watchers {
		if w == ch {
			a.watchers = append(a.watchers[:i], a.watchers[i+1:]...)
			close(ch)
			break
		}
	}
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

func (a *Appender) AppendBatch(events []wire.BatchEvent) error {
	if len(events) == 0 {
		return nil
	}
	start := time.Now()
	a.mutex.Lock()
	defer a.mutex.Unlock()
	for _, ev := range events {
		record := storage.EventRecord{
			Timestamp:     time.Now().UTC(),
			AggregateKind: ev.Kind,
			AggregateID:   ev.ID,
			EventType:     ev.EventType,
			Payload:       ev.Payload,
		}
		encoded, err := record.Encode()
		if err != nil {
			log.Printf("[APPEND_BATCH] ✗ encode failed for %s/%s: %v\n", ev.Kind, ev.ID, err)
			return err
		}
		offset := a.nextOffset
		n, err := a.eventsFile.Write(encoded)
		if err != nil {
			log.Printf("[APPEND_BATCH] ✗ write failed: %v\n", err)
			return err
		}
		a.nextOffset += int64(n)
		if a.index != nil {
			a.index.Add(ev.Kind, ev.ID, offset)
		}
	}
	if err := a.eventsFile.Sync(); err != nil {
		log.Printf("[APPEND_BATCH] ✗ batch sync failed: %v\n", err)
		return err
	}
	a.muWatchers.RLock()
	if len(a.watchers) > 0 {
		for _, ev := range events {
			for _, ch := range a.watchers {
				select {
				case ch <- ev:
				default:
				}
			}
		}
	}
	a.muWatchers.RUnlock()
	log.Printf("[APPEND_BATCH] ✓ %d events committed in %v\n", len(events), time.Since(start))
	return nil
}
func (a *Appender) Close() error {
	log.Println("[engine] closing storage files...")
	a.eventsFile.Close()
	return a.snapshotsFile.Close()
}
