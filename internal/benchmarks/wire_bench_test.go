package benchmarks

import (
	"bytes"
	"testing"

	"github.com/x-name15/replaydb/internal/wire"
)

func BenchmarkWire_WriteRequest_Append(b *testing.B) {
	req := &wire.Request{
		Op:        wire.OpAppend,
		Kind:      "order",
		ID:        "order-123",
		EventType: "OrderCreated",
		Payload:   []byte(`{"total":42.5,"currency":"EUR"}`),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := wire.WriteRequest(&buf, req); err != nil {
			b.Fatalf("WriteRequest failed: %v", err)
		}
	}
}

func BenchmarkWire_ReadRequest_Append(b *testing.B) {
	req := &wire.Request{
		Op:        wire.OpAppend,
		Kind:      "order",
		ID:        "order-123",
		EventType: "OrderCreated",
		Payload:   []byte(`{"total":42.5,"currency":"EUR"}`),
	}
	var buf bytes.Buffer
	if err := wire.WriteRequest(&buf, req); err != nil {
		b.Fatalf("WriteRequest failed: %v", err)
	}
	encoded := buf.Bytes()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := wire.ReadRequest(bytes.NewReader(encoded)); err != nil {
			b.Fatalf("ReadRequest failed: %v", err)
		}
	}
}

func BenchmarkWire_WriteResponse(b *testing.B) {
	resp := &wire.Response{
		Status:  wire.StatusOK,
		Message: "event logged",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := wire.WriteResponse(&buf, resp); err != nil {
			b.Fatalf("WriteResponse failed: %v", err)
		}
	}
}
