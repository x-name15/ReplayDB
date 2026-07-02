package benchmarks

import (
	"fmt"
	"testing"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
)

type counterState struct {
	Count   int
	version uint32
}

func newCounterState(id string) domain.Aggregate {
	return &counterState{}
}

func (c *counterState) Version() uint32 { return c.version }

func (c *counterState) Apply(eventType string, payload []byte, timestamp time.Time) error {
	c.Count++
	c.version++
	return nil
}

func benchRegistry() *domain.Registry {
	r := domain.NewRegistry()
	r.Register("counter", newCounterState)
	return r
}

func BenchmarkAppender_Append(b *testing.B) {
	dir := b.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		b.Fatalf("NewAppender failed: %v", err)
	}
	defer appender.Close()

	payload := []byte(`{}`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := appender.Append("counter", "bench-agg", "Increment", payload); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}
}

func BenchmarkIndex_Add(b *testing.B) {
	idx := engine.NewIndex()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Add("counter", "bench-agg", int64(i))
	}
}

func BenchmarkIndex_Offsets(b *testing.B) {
	idx := engine.NewIndex()
	for i := 0; i < 1000; i++ {
		idx.Add("counter", "bench-agg", int64(i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Offsets("counter", "bench-agg")
	}
}

func BenchmarkReplayStateAt_Indexed(b *testing.B) {
	dir, index := seedReplayLog(b, 5000)
	registry := benchRegistry()
	target := time.Now().UTC().Add(time.Hour)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.ReplayStateAt(dir, "counter", "target-agg", target, registry, index); err != nil {
			b.Fatalf("ReplayStateAt (indexed) failed: %v", err)
		}
	}
}

func BenchmarkReplayStateAt_FullScan(b *testing.B) {
	dir, _ := seedReplayLog(b, 5000)
	registry := benchRegistry()
	target := time.Now().UTC().Add(time.Hour)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.ReplayStateAt(dir, "counter", "target-agg", target, registry, nil); err != nil {
			b.Fatalf("ReplayStateAt (full-scan) failed: %v", err)
		}
	}
}

func seedReplayLog(b *testing.B, noise int) (string, *engine.Index) {
	b.Helper()
	dir := b.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		b.Fatalf("NewAppender failed: %v", err)
	}

	for i := 0; i < noise; i++ {
		id := fmt.Sprintf("noise-agg-%d", i%50)
		if err := appender.Append("counter", id, "Increment", []byte("{}")); err != nil {
			b.Fatalf("seed Append failed: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := appender.Append("counter", "target-agg", "Increment", []byte("{}")); err != nil {
			b.Fatalf("seed Append (target) failed: %v", err)
		}
	}
	appender.Close()

	return dir, index
}
