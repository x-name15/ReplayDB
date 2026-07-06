package engine

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type Archiver struct {
	dataDir    string
	archiveDir string
	interval   time.Duration
	stopCh     chan struct{}
	lastCycle  atomic.Pointer[ArchiveCycleInfo]
}

type ArchiveCycleInfo struct {
	At            time.Time
	Duration      time.Duration
	EventBytes    int64
	SnapshotBytes int64
	Err           string
}

func (a *Archiver) LastCycle() *ArchiveCycleInfo {
	return a.lastCycle.Load()
}

func NewArchiver(dataDir, archiveDir string, interval time.Duration) *Archiver {
	return &Archiver{
		dataDir:    dataDir,
		archiveDir: archiveDir,
		interval:   interval,
		stopCh:     make(chan struct{}),
	}
}

func (a *Archiver) Run() {
	if a.interval <= 0 || a.archiveDir == "" {
		log.Println("[archive] disabled (set REDB_ARCHIVE_DIR and REDB_ARCHIVE_INTERVAL to enable)")
		return
	}
	if err := os.MkdirAll(a.archiveDir, 0755); err != nil {
		log.Printf("[archive] ✗ failed to create archive dir %s: %v\n", a.archiveDir, err)
		return
	}
	log.Printf("[archive] enabled — every %s, mirroring new data from %s to %s (source files are never modified, truncated, or deleted)\n",
		a.interval, a.dataDir, a.archiveDir)
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	a.runCycle()
	for {
		select {
		case <-ticker.C:
			a.runCycle()
		case <-a.stopCh:
			log.Println("[archive] stopping — running final cycle before shutdown")
			a.runCycle()
			return
		}
	}
}

func (a *Archiver) Stop() {
	close(a.stopCh)
}

func (a *Archiver) runCycle() {
	start := time.Now()
	eCopied, eErr := mirrorAppendOnly(filepath.Join(a.dataDir, "events.redb"), filepath.Join(a.archiveDir, "events.redb"))
	if eErr != nil {
		log.Printf("[archive] ✗ events.redb mirror failed: %v\n", eErr)
	}
	sCopied, sErr := mirrorAppendOnly(filepath.Join(a.dataDir, "snapshots.redb"), filepath.Join(a.archiveDir, "snapshots.redb"))
	if sErr != nil {
		log.Printf("[archive] ✗ snapshots.redb mirror failed: %v\n", sErr)
	}
	info := &ArchiveCycleInfo{At: start, Duration: time.Since(start), EventBytes: eCopied, SnapshotBytes: sCopied}
	if eErr != nil {
		info.Err = eErr.Error()
	} else if sErr != nil {
		info.Err = sErr.Error()
	}
	a.lastCycle.Store(info)
	if eCopied > 0 || sCopied > 0 {
		log.Printf("[archive] ✓ cycle complete — %d new event bytes, %d new snapshot bytes mirrored (%s)\n",
			eCopied, sCopied, time.Since(start))
	}
}

func mirrorAppendOnly(src, dst string) (int64, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer srcFile.Close()
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return 0, err
	}
	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()
	dstInfo, err := dstFile.Stat()
	if err != nil {
		return 0, err
	}
	resumeAt := dstInfo.Size()
	if resumeAt >= srcInfo.Size() {
		return 0, nil
	}
	if _, err := srcFile.Seek(resumeAt, io.SeekStart); err != nil {
		return 0, err
	}
	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return n, err
	}
	if err := dstFile.Sync(); err != nil {
		return n, err
	}
	return n, nil
}
