package engine

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/x-name15/replaydb/internal/storage"
)

type compactionReader struct {
	r io.Reader
	n int64
}
type CompactionInfo struct {
	At        time.Time
	Duration  time.Duration
	Kept      int
	Discarded int
	Corrupted int
	Err       string
}

func (cr *compactionReader) Read(p []byte) (n int, err error) {
	n, err = cr.r.Read(p)
	cr.n += int64(n)
	return
}

func (a *Appender) Compact(dataDir string) (err error) {
	if !a.compactMu.TryLock() {
		return fmt.Errorf("engine: a compaction is already in progress")
	}
	defer a.compactMu.Unlock()
	log.Println("[COMPACTOR] Starting background Log Compaction...")
	start := time.Now()
	var kept, discarded, corrupted int
	defer func() {
		info := &CompactionInfo{At: start, Duration: time.Since(start), Kept: kept, Discarded: discarded, Corrupted: corrupted}
		if err != nil {
			info.Err = err.Error()
		}
		a.lastCompaction.Store(info)
	}()
	a.mutex.Lock()
	cutoffOffset := a.nextOffset
	a.mutex.Unlock()
	eventsPath := filepath.Join(dataDir, "events.redb")
	tmpPath := filepath.Join(dataDir, "events.tmp.redb")
	snapshotsPath := filepath.Join(dataDir, "snapshots.redb")
	reader, err := os.Open(eventsPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	cr := &compactionReader{r: reader}
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	snapshotMap := make(map[string]time.Time)
	snapReader, err := os.Open(snapshotsPath)
	if err == nil {
		defer snapReader.Close()
		for {
			rec, err := storage.DecodeNextSnapshot(snapReader)
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			key := rec.AggregateKind + ":" + rec.AggregateID
			if current, exists := snapshotMap[key]; !exists || rec.Timestamp.After(current) {
				snapshotMap[key] = rec.Timestamp
			}
		}
	}
	for cr.n < cutoffOffset {
		rec, err := storage.DecodeNext(cr)
		if err == io.EOF {
			break
		}
		if err == storage.ErrChecksumMismatch {
			log.Printf("[COMPACTOR] ⚠ corrupt record skipped at byte offset ~%d\n", cr.n)
			corrupted++
			continue
		}
		if err != nil {
			tmpFile.Close()
			return err
		}
		key := rec.AggregateKind + ":" + rec.AggregateID
		latestSnapTS, hasSnapshot := snapshotMap[key]
		if hasSnapshot && !rec.Timestamp.After(latestSnapTS) {
			discarded++
			continue
		}
		encoded, err := rec.Encode()
		if err != nil {
			tmpFile.Close()
			return err
		}
		if _, err := tmpFile.Write(encoded); err != nil {
			tmpFile.Close()
			return err
		}
		kept++
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	for {
		rec, err := storage.DecodeNext(cr)
		if err == io.EOF {
			break
		}
		if err == storage.ErrChecksumMismatch {
			log.Printf("[COMPACTOR] ⚠ corrupt record skipped at byte offset ~%d\n", cr.n)
			corrupted++
			continue
		}
		if err != nil {
			tmpFile.Close()
			return err
		}
		encoded, err := rec.Encode()
		if err != nil {
			tmpFile.Close()
			return err
		}
		if _, err := tmpFile.Write(encoded); err != nil {
			tmpFile.Close()
			return err
		}
		kept++
	}
	a.eventsFile.Close()
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()
	if err := os.Rename(tmpPath, eventsPath); err != nil {
		return err
	}
	if dirFile, err := os.Open(dataDir); err == nil {
		if err := dirFile.Sync(); err != nil {
			log.Printf("[COMPACTOR] Warning: failed to fsync data directory after rename: %v", err)
		}
		dirFile.Close()
	} else {
		log.Printf("[COMPACTOR] Warning: failed to open data directory for fsync: %v", err)
	}
	a.eventsFile, err = os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	stat, _ := a.eventsFile.Stat()
	a.nextOffset = stat.Size()
	if a.index != nil {
		if err := a.index.Rebuild(dataDir); err != nil {
			log.Printf("[COMPACTOR] Warning: index rebuild failed: %v", err)
		}
	}
	log.Printf("[COMPACTOR] Completed in %v. Kept: %d, Discarded: %d, Corrupted (skipped): %d", time.Since(start), kept, discarded, corrupted)
	return nil
}
