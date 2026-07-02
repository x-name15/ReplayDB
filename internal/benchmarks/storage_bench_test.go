package benchmarks

import (
	"bytes"
	"testing"
	"time"

	"github.com/x-name15/replaydb/internal/storage"
)

func BenchmarkEventRecord_Encode(b *testing.B) {
	record := &storage.EventRecord{
		Timestamp:     time.Now().UTC(),
		AggregateKind: "order",
		AggregateID:   "order-123",
		EventType:     "OrderCreated",
		Payload:       []byte(`{"total":42.5,"currency":"EUR","items":[{"sku":"A1","qty":2},{"sku":"B7","qty":1}]}`),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := record.Encode(); err != nil {
			b.Fatalf("Encode failed: %v", err)
		}
	}
}

func BenchmarkEventRecord_DecodeNext(b *testing.B) {
	record := &storage.EventRecord{
		Timestamp:     time.Now().UTC(),
		AggregateKind: "order",
		AggregateID:   "order-123",
		EventType:     "OrderCreated",
		Payload:       []byte(`{"total":42.5,"currency":"EUR","items":[{"sku":"A1","qty":2},{"sku":"B7","qty":1}]}`),
	}
	encoded, err := record.Encode()
	if err != nil {
		b.Fatalf("Encode failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := storage.DecodeNext(bytes.NewReader(encoded)); err != nil {
			b.Fatalf("DecodeNext failed: %v", err)
		}
	}
}

func BenchmarkEventRecord_EncodeDecode_64KBPayload(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 64*1024)
	record := &storage.EventRecord{
		Timestamp:     time.Now().UTC(),
		AggregateKind: "order",
		AggregateID:   "order-123",
		EventType:     "BulkImport",
		Payload:       payload,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := record.Encode()
		if err != nil {
			b.Fatalf("Encode failed: %v", err)
		}
		if _, err := storage.DecodeNext(bytes.NewReader(encoded)); err != nil {
			b.Fatalf("DecodeNext failed: %v", err)
		}
	}
}

func BenchmarkSnapshotRecord_EncodeDecode_Roundtrip(b *testing.B) {
	record := &storage.SnapshotRecord{
		Timestamp:     time.Now().UTC(),
		Version:       42,
		AggregateKind: "order",
		AggregateID:   "order-123",
		StateJSON:     []byte(`{"status":"PAID","total":42.5,"currency":"EUR"}`),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, err := record.Encode()
		if err != nil {
			b.Fatalf("Encode failed: %v", err)
		}
		if _, err := storage.DecodeNextSnapshot(bytes.NewReader(encoded)); err != nil {
			b.Fatalf("DecodeNextSnapshot failed: %v", err)
		}
	}
}
