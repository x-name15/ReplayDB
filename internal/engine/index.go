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

// Index maps (kind, id) to the ordered byte offsets in events.redb where
// that aggregate's events live.
type Index struct {
	mu      sync.RWMutex
	offsets map[string][]int64
}

// NewIndex returns an empty index. Call Rebuild to populate it from an
// existing log on startup.
func NewIndex() *Index {
	return &Index{offsets: make(map[string][]int64)}
}

func indexKey(kind, id string) string {
	return kind + "\x00" + id
}

// Add records that aggregate (kind, id) has an event starting at offset.
func (idx *Index) Add(kind, id string, offset int64) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	key := indexKey(kind, id)
	idx.offsets[key] = append(idx.offsets[key], offset)
}

// Offsets returns the known offsets for an aggregate, oldest first.
func (idx *Index) Offsets(kind, id string) []int64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	src := idx.offsets[indexKey(kind, id)]
	out := make([]int64, len(src))
	copy(out, src)
	return out
}

// Len returns how many distinct aggregates the index currently tracks.
// Used for observability (see internal/metrics).
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

// Rebuild scans events.redb from scratch and populates the index. Meant to
// run once at server startup — after that, the Appender keeps the index in
// sync incrementally.
func (idx *Index) Rebuild(dataDir string) error {
	start := time.Now()
	eventsPath := filepath.Join(dataDir, "events.redb")

	file, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("[INDEX] no existing events.redb — starting with an empty index")
			return nil
		}
		return err
	}
	defer file.Close()

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.offsets = make(map[string][]int64)

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
			log.Printf("[INDEX] ⚠ stopped scanning at offset %d: %v\n", offset, err)
			break
		}
		key := indexKey(record.AggregateKind, record.AggregateID)
		idx.offsets[key] = append(idx.offsets[key], offset)
		totalEvents++
	}

	log.Printf("[INDEX] ✓ rebuilt: %d aggregates, %d events indexed, %d corrupt records skipped (%s)\n",
		len(idx.offsets), totalEvents, corrupted, time.Since(start))

	return nil
}
