package engine

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/x-name15/replaydb/internal/storage"
)

type Index struct {
	mu              sync.RWMutex
	offsets         map[string][]int64
	snapshotOffsets map[string][]int64
}

func NewIndex() *Index {
	return &Index{
		offsets:         make(map[string][]int64),
		snapshotOffsets: make(map[string][]int64),
	}
}

func indexKey(kind, id string) string {
	return kind + "\x00" + id
}

func (idx *Index) Add(kind, id string, offset int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	key := indexKey(kind, id)
	idx.offsets[key] = append(idx.offsets[key], offset)
}

func (idx *Index) Offsets(kind, id string) []int64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	src := idx.offsets[indexKey(kind, id)]
	out := make([]int64, len(src))
	copy(out, src)
	return out
}

func (idx *Index) AddSnapshot(kind, id string, offset int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	key := indexKey(kind, id)
	idx.snapshotOffsets[key] = append(idx.snapshotOffsets[key], offset)
}

func (idx *Index) SnapshotOffsets(kind, id string) []int64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	src := idx.snapshotOffsets[indexKey(kind, id)]
	out := make([]int64, len(src))
	copy(out, src)
	return out
}

func (idx *Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.offsets)
}

type countingReader struct {
	r     io.Reader
	count int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.count += int64(n)
	return n, err
}

func (idx *Index) Rebuild(dataDir string) error {
	start := time.Now()
	eventsPath := filepath.Join(dataDir, "events.redb")
	file, err := os.Open(eventsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		log.Println("[INDEX] no existing events.redb — starting with an empty index")
	}
	idx.mu.Lock()
	idx.offsets = make(map[string][]int64)
	idx.snapshotOffsets = make(map[string][]int64)
	idx.mu.Unlock()
	if file != nil {
		defer file.Close()
		idx.mu.Lock()
		cr := &countingReader{r: file}
		totalEvents := 0
		corrupted := 0
		for {
			offset := cr.count
			record, err := storage.DecodeNext(cr)
			if err == storage.ErrEOF {
				break
			}
			if err == storage.ErrChecksumMismatch {
				corrupted++
				continue
			}
			if err != nil {
				log.Printf("[INDEX] ⚠ stopped scanning events at offset %d: %v\n", offset, err)
				break
			}
			key := indexKey(record.AggregateKind, record.AggregateID)
			idx.offsets[key] = append(idx.offsets[key], offset)
			totalEvents++
		}
		log.Printf("[INDEX] ✓ events rebuilt: %d aggregates, %d events indexed, %d corrupt records skipped (%s)\n",
			len(idx.offsets), totalEvents, corrupted, time.Since(start))
		idx.mu.Unlock()
	}
	return idx.rebuildSnapshots(dataDir)
}

func (idx *Index) rebuildSnapshots(dataDir string) error {
	start := time.Now()
	snapshotsPath := filepath.Join(dataDir, "snapshots.redb")
	file, err := os.Open(snapshotsPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("[INDEX] no existing snapshots.redb — starting with an empty snapshot index")
			return nil
		}
		return err
	}
	defer file.Close()
	idx.mu.Lock()
	defer idx.mu.Unlock()
	cr := &countingReader{r: file}
	total := 0
	corrupted := 0
	for {
		offset := cr.count
		snap, err := storage.DecodeNextSnapshot(cr)
		if err == storage.ErrEOF {
			break
		}
		if err == storage.ErrSnapshotChecksumMismatch {
			corrupted++
			continue
		}
		if err != nil {
			log.Printf("[INDEX] ⚠ stopped scanning snapshots at offset %d: %v\n", offset, err)
			break
		}
		key := indexKey(snap.AggregateKind, snap.AggregateID)
		idx.snapshotOffsets[key] = append(idx.snapshotOffsets[key], offset)
		total++
	}
	log.Printf("[INDEX] ✓ snapshots rebuilt: %d aggregates, %d snapshots indexed, %d corrupt records skipped (%s)\n",
		len(idx.snapshotOffsets), total, corrupted, time.Since(start))
	return nil
}
