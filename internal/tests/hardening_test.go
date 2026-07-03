package tests

import (
	"bytes"
	"github.com/x-name15/replaydb/internal/engine"
	"github.com/x-name15/replaydb/internal/wire"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Fix 1: wire auth handshake ---

func TestWireAuth_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := wire.WriteAuthToken(&buf, "s3cr3t"); err != nil {
		t.Fatalf("WriteAuthToken failed: %v", err)
	}
	got, err := wire.ReadAuthToken(&buf)
	if err != nil {
		t.Fatalf("ReadAuthToken failed: %v", err)
	}
	if got != "s3cr3t" {
		t.Errorf("got token %q, want %q", got, "s3cr3t")
	}
}

func TestWireAuth_TokensEqual(t *testing.T) {
	if !wire.TokensEqual("abc", "abc") {
		t.Error("expected equal tokens to match")
	}
	if wire.TokensEqual("abc", "abd") {
		t.Error("expected different tokens to not match")
	}
	if wire.TokensEqual("abc", "") {
		t.Error("expected empty token to not match a non-empty one")
	}
}

// --- Fix 2: configurable payload cap ---

func TestWireProtocol_MaxFieldLenConfigurable(t *testing.T) {
	defer wire.SetMaxFieldLen(0) // restore built-in default after the test
	req := &wire.Request{
		Op:        wire.OpAppend,
		Kind:      "order",
		ID:        "o-1",
		EventType: "OrderCreated",
		Payload:   []byte("this payload is definitely longer than eight bytes"),
	}
	var buf bytes.Buffer
	if err := wire.WriteRequest(&buf, req); err != nil {
		t.Fatalf("WriteRequest failed: %v", err)
	}
	wire.SetMaxFieldLen(8)
	if _, err := wire.ReadRequest(&buf); err == nil {
		t.Error("expected ReadRequest to reject a frame exceeding the configured max, got nil error")
	}
}

func TestWireProtocol_MaxFieldLenZeroRestoresDefault(t *testing.T) {
	wire.SetMaxFieldLen(8)
	wire.SetMaxFieldLen(0)
	req := &wire.Request{Op: wire.OpAppend, Kind: "order", ID: "o-1", EventType: "OrderCreated", Payload: []byte("this payload is definitely longer than eight bytes")}
	var buf bytes.Buffer
	if err := wire.WriteRequest(&buf, req); err != nil {
		t.Fatalf("WriteRequest failed: %v", err)
	}
	if _, err := wire.ReadRequest(&buf); err != nil {
		t.Errorf("expected default max field len to accept a normal-sized request, got error: %v", err)
	}
}

// --- Fix 4: snapshot index ---

func TestIndex_SnapshotOffsetsAndRebuild(t *testing.T) {
	dir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}
	defer appender.Close()
	if err := appender.SaveSnapshot("counter", "c-1", 3, []byte(`{"Count":3}`)); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}
	if err := appender.SaveSnapshot("counter", "c-1", 7, []byte(`{"Count":7}`)); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}
	if got := len(index.SnapshotOffsets("counter", "c-1")); got != 2 {
		t.Fatalf("expected 2 snapshot offsets, got %d", got)
	}
	rebuilt := engine.NewIndex()
	if err := rebuilt.Rebuild(dir); err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if got := len(rebuilt.SnapshotOffsets("counter", "c-1")); got != 2 {
		t.Errorf("expected 2 snapshot offsets after rebuild, got %d", got)
	}
}

func TestReplayStateAt_IndexedSnapshotMatchesFullScan(t *testing.T) {
	dir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}
	defer appender.Close()
	for i := 0; i < 3; i++ {
		if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}
	if err := appender.SaveSnapshot("counter", "c-1", 3, []byte(`{"Count":3}`)); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}
	registry := testRegistry()
	targetTime := time.Now().UTC().Add(time.Hour)
	indexed, err := engine.ReplayStateAt(dir, "counter", "c-1", targetTime, registry, index)
	if err != nil {
		t.Fatalf("indexed ReplayStateAt failed: %v", err)
	}
	fullScan, err := engine.ReplayStateAt(dir, "counter", "c-1", targetTime, registry, nil)
	if err != nil {
		t.Fatalf("full-scan ReplayStateAt failed: %v", err)
	}
	indexedCount := indexed.(*counterState).Count
	fullScanCount := fullScan.(*counterState).Count
	if indexedCount != 5 {
		t.Errorf("indexed replay with snapshot: got Count %d, want 5", indexedCount)
	}
	if indexedCount != fullScanCount {
		t.Errorf("indexed and full-scan replay disagree: indexed=%d, fullScan=%d", indexedCount, fullScanCount)
	}
}

// --- Fix 5: non-destructive archiver ---

func TestArchiver_MirrorsAppendedDataWithoutModifyingSource(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(srcDir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}
	defer appender.Close()
	if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	preArchiveSrcBytes, err := os.ReadFile(filepath.Join(srcDir, "events.redb"))
	if err != nil {
		t.Fatalf("failed to read source events.redb: %v", err)
	}
	archiver := engine.NewArchiver(srcDir, archiveDir, 20*time.Millisecond)
	go archiver.Run()
	time.Sleep(60 * time.Millisecond) // allow at least one cycle to run
	if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
		t.Fatalf("second Append failed: %v", err)
	}
	time.Sleep(60 * time.Millisecond) // allow a later cycle to pick up the new bytes
	archiver.Stop()
	time.Sleep(60 * time.Millisecond) // let the final cycle triggered by Stop complete
	srcBytes, err := os.ReadFile(filepath.Join(srcDir, "events.redb"))
	if err != nil {
		t.Fatalf("failed to read source events.redb: %v", err)
	}
	if !bytes.HasPrefix(srcBytes, preArchiveSrcBytes) {
		t.Fatalf("source events.redb was unexpectedly modified by the archiver (original bytes no longer a prefix)")
	}
	archivedBytes, err := os.ReadFile(filepath.Join(archiveDir, "events.redb"))
	if err != nil {
		t.Fatalf("failed to read archived events.redb: %v", err)
	}
	if !bytes.Equal(srcBytes, archivedBytes) {
		t.Errorf("archived events.redb does not match source: %d bytes vs %d bytes", len(archivedBytes), len(srcBytes))
	}
}

func TestArchiver_DisabledWhenIntervalIsZero(t *testing.T) {
	srcDir := t.TempDir()
	archiveDir := t.TempDir()
	archiver := engine.NewArchiver(srcDir, archiveDir, 0)
	done := make(chan struct{})
	go func() {
		archiver.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() with a zero interval should return immediately instead of starting the loop")
	}
	if _, err := os.Stat(filepath.Join(archiveDir, "events.redb")); !os.IsNotExist(err) {
		t.Errorf("expected no archive file to be created when archiving is disabled, stat err: %v", err)
	}
}
