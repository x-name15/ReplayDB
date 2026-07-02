package tests

import (
	"testing"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
)

// counterState is a minimal test Aggregate defined locally here rather than
// inside the engine package, since this file can only depend on engine's
// exported API (domain.Aggregate, engine.Index, engine.ReplayStateAt, ...).
type counterState struct {
	Count   int
	version uint32
}

func newCounterState(id string) domain.Aggregate {
	return &counterState{}
}

func (c *counterState) Version() uint32 { return c.version }

func (c *counterState) Apply(eventType string, payload []byte, timestamp time.Time) error {
	if eventType == "Increment" {
		c.Count++
	}
	c.version++
	return nil
}

func testRegistry() *domain.Registry {
	r := domain.NewRegistry()
	r.Register("counter", newCounterState)
	return r
}

func TestIndex_AddAndOffsets(t *testing.T) {
	idx := engine.NewIndex()
	idx.Add("counter", "c-1", 0)
	idx.Add("counter", "c-1", 50)
	idx.Add("counter", "c-2", 25)

	got := idx.Offsets("counter", "c-1")
	want := []int64{0, 50}
	if len(got) != len(want) {
		t.Fatalf("got %d offsets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("offset %d: got %d, want %d", i, got[i], want[i])
		}
	}

	if got := idx.Offsets("counter", "does-not-exist"); len(got) != 0 {
		t.Errorf("expected no offsets for unknown aggregate, got %v", got)
	}
}

func TestIndex_OffsetsReturnsIndependentCopy(t *testing.T) {
	idx := engine.NewIndex()
	idx.Add("counter", "c-1", 10)

	got := idx.Offsets("counter", "c-1")
	got[0] = 999

	fresh := idx.Offsets("counter", "c-1")
	if fresh[0] != 10 {
		t.Errorf("internal index state was mutated via returned slice: got %d, want 10", fresh[0])
	}
}

func TestIndex_Rebuild(t *testing.T) {
	dir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}

	if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := appender.Append("counter", "c-2", "Increment", []byte("{}")); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	appender.Close()

	rebuilt := engine.NewIndex()
	if err := rebuilt.Rebuild(dir); err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}

	if got := len(rebuilt.Offsets("counter", "c-1")); got != 2 {
		t.Errorf("expected 2 offsets for c-1 after rebuild, got %d", got)
	}
	if got := len(rebuilt.Offsets("counter", "c-2")); got != 1 {
		t.Errorf("expected 1 offset for c-2 after rebuild, got %d", got)
	}
}

func TestIndex_RebuildOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	idx := engine.NewIndex()
	if err := idx.Rebuild(dir); err != nil {
		t.Errorf("Rebuild on missing file should not error, got: %v", err)
	}
}

func TestReplayStateAt_IndexedAndFullScanAgree(t *testing.T) {
	dir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}
	defer appender.Close()

	for i := 0; i < 5; i++ {
		if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := appender.Append("counter", "c-2", "Increment", []byte("{}")); err != nil {
			t.Fatalf("Append (c-2) %d failed: %v", i, err)
		}
	}

	registry := testRegistry()
	targetTime := time.Now().UTC().Add(time.Hour)

	indexedResult, err := engine.ReplayStateAt(dir, "counter", "c-1", targetTime, registry, index)
	if err != nil {
		t.Fatalf("indexed ReplayStateAt failed: %v", err)
	}
	fullScanResult, err := engine.ReplayStateAt(dir, "counter", "c-1", targetTime, registry, nil)
	if err != nil {
		t.Fatalf("full-scan ReplayStateAt failed: %v", err)
	}

	indexedCount := indexedResult.(*counterState).Count
	fullScanCount := fullScanResult.(*counterState).Count

	if indexedCount != 5 {
		t.Errorf("indexed replay: got Count %d, want 5", indexedCount)
	}
	if indexedCount != fullScanCount {
		t.Errorf("indexed and full-scan replay disagree: indexed=%d, fullScan=%d", indexedCount, fullScanCount)
	}
}

func TestReplayStateAt_RespectsTimeTravel(t *testing.T) {
	dir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}
	defer appender.Close()

	if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	midpoint := time.Now().UTC()
	time.Sleep(2 * time.Millisecond)
	if err := appender.Append("counter", "c-1", "Increment", []byte("{}")); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	registry := testRegistry()

	state, err := engine.ReplayStateAt(dir, "counter", "c-1", midpoint, registry, index)
	if err != nil {
		t.Fatalf("ReplayStateAt at midpoint failed: %v", err)
	}
	if got := state.(*counterState).Count; got != 1 {
		t.Errorf("time-travel to midpoint: got Count %d, want 1 (should exclude the later event)", got)
	}

	stateNow, err := engine.ReplayStateAt(dir, "counter", "c-1", time.Now().UTC(), registry, index)
	if err != nil {
		t.Fatalf("ReplayStateAt at now failed: %v", err)
	}
	if got := stateNow.(*counterState).Count; got != 2 {
		t.Errorf("time-travel to now: got Count %d, want 2", got)
	}
}

func TestReplayStateAt_UnknownAggregateReturnsError(t *testing.T) {
	dir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}
	defer appender.Close()

	registry := testRegistry()
	_, err = engine.ReplayStateAt(dir, "counter", "never-existed", time.Now().UTC(), registry, index)
	if err == nil {
		t.Error("expected error replaying an aggregate ID with no events, got nil")
	}
}

func TestReplayStateAt_UnknownKindReturnsError(t *testing.T) {
	dir := t.TempDir()
	registry := testRegistry()
	_, err := engine.ReplayStateAt(dir, "not-registered", "id-1", time.Now().UTC(), registry, nil)
	if err == nil {
		t.Error("expected error for unregistered aggregate kind, got nil")
	}
}
