package engine

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/x-name15/replaydb/internal/storage"
)

type Appender struct {
	eventsFile    *os.File
	snapshotsFile *os.File
	mutex         sync.Mutex
	index         *Index
	nextOffset    int64
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

	return &Appender{
		eventsFile:    eFile,
		snapshotsFile: sFile,
		index:         index,
		nextOffset:    info.Size(),
	}, nil
}

func (a *Appender) Append(kind, aggregateID, eventType string, payload []byte) error {
	record := storage.EventRecord{
		Timestamp:     time.Now().UTC(),
		AggregateKind: kind,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       payload,
	}

	encoded, err := record.Encode()
	if err != nil {
		return err
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	offset := a.nextOffset

	if _, err := a.eventsFile.Write(encoded); err != nil {
		return err
	}
	if err := a.eventsFile.Sync(); err != nil {
		return err
	}

	a.nextOffset += int64(len(encoded))

	if a.index != nil {
		a.index.Add(kind, aggregateID, offset)
	}

	return nil
}

func (a *Appender) SaveSnapshot(kind, aggregateID string, version uint32, stateJSON []byte) error {
	record := storage.SnapshotRecord{
		Timestamp:     time.Now().UTC(),
		Version:       version,
		AggregateKind: kind,
		AggregateID:   aggregateID,
		StateJSON:     stateJSON,
	}

	encoded, err := record.Encode()
	if err != nil {
		return err
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()

	if _, err := a.snapshotsFile.Write(encoded); err != nil {
		return err
	}
	return a.snapshotsFile.Sync()
}

func (a *Appender) Close() error {
	a.eventsFile.Close()
	return a.snapshotsFile.Close()
}
