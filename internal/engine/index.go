package engine

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/x-name15/replaydb/internal/storage"
)

type Index struct {
	mu      sync.RWMutex
	offsets map[string][]int64
}

func NewIndex() *Index {
	return &Index{offsets: make(map[string][]int64)}
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
	eventsPath := filepath.Join(dataDir, "events.redb")

	file, err := os.Open(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.offsets = make(map[string][]int64)

	cr := &countingReader{r: file}
	for {
		offset := cr.count
		record, err := storage.DecodeNext(cr)
		if err == storage.ErrEOF {
			break
		}
		if err == storage.ErrChecksumMismatch {
			continue
		}
		if err != nil {
			break
		}
		key := indexKey(record.AggregateKind, record.AggregateID)
		idx.offsets[key] = append(idx.offsets[key], offset)
	}

	return nil
}
