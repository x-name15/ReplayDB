// Package tests holds ReplayDB's test suite, kept separate from the
// packages it exercises. Since these are external test packages, they can
// only reach exported identifiers — which is fine here, everything worth
// testing in storage/engine is already public API.
package tests

import (
	"bytes"
	"testing"
	"time"

	"github.com/x-name15/replaydb/internal/storage"
)

func TestEventRecord_EncodeDecode_Roundtrip(t *testing.T) {
	original := &storage.EventRecord{
		Timestamp:     time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		AggregateKind: "order",
		AggregateID:   "order-123",
		EventType:     "OrderCreated",
		Payload:       []byte(`{"total":42.5,"currency":"EUR"}`),
	}

	encoded, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := storage.DecodeNext(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeNext failed: %v", err)
	}

	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	if decoded.AggregateKind != original.AggregateKind {
		t.Errorf("AggregateKind mismatch: got %q, want %q", decoded.AggregateKind, original.AggregateKind)
	}
	if decoded.AggregateID != original.AggregateID {
		t.Errorf("AggregateID mismatch: got %q, want %q", decoded.AggregateID, original.AggregateID)
	}
	if decoded.EventType != original.EventType {
		t.Errorf("EventType mismatch: got %q, want %q", decoded.EventType, original.EventType)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload mismatch: got %q, want %q", decoded.Payload, original.Payload)
	}
}

func TestEventRecord_PayloadWithPipeCharacter(t *testing.T) {
	// Regression test: the old text-based wire protocol broke on '|' bytes
	// inside a payload. The binary log format never had that bug, but this
	// guards against reintroducing a delimiter-based scheme here.
	record := &storage.EventRecord{
		Timestamp:     time.Now().UTC(),
		AggregateKind: "order",
		AggregateID:   "order-1",
		EventType:     "Note",
		Payload:       []byte(`{"text":"price|discount|total breakdown"}`),
	}

	encoded, err := record.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := storage.DecodeNext(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeNext failed: %v", err)
	}
	if !bytes.Equal(decoded.Payload, record.Payload) {
		t.Errorf("Payload with pipe bytes corrupted: got %q, want %q", decoded.Payload, record.Payload)
	}
}

func TestEventRecord_DetectsChecksumCorruption(t *testing.T) {
	record := &storage.EventRecord{
		Timestamp:     time.Now().UTC(),
		AggregateKind: "order",
		AggregateID:   "order-1",
		EventType:     "OrderCreated",
		Payload:       []byte(`{"total":10}`),
	}

	encoded, err := record.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	corrupted := make([]byte, len(encoded))
	copy(corrupted, encoded)
	corrupted[len(corrupted)-5] ^= 0xFF

	_, err = storage.DecodeNext(bytes.NewReader(corrupted))
	if err != storage.ErrChecksumMismatch {
		t.Errorf("expected ErrChecksumMismatch on corrupted record, got: %v", err)
	}
}

func TestEventRecord_DetectsBadMagicBytes(t *testing.T) {
	garbage := []byte{0x00, 0x00, 0x01, 0x02, 0x03}
	_, err := storage.DecodeNext(bytes.NewReader(garbage))
	if err != storage.ErrCorruptData {
		t.Errorf("expected ErrCorruptData for bad magic bytes, got: %v", err)
	}
}

func TestDecodeNext_MultipleRecordsSequentially(t *testing.T) {
	var buf bytes.Buffer
	want := []string{"evt-a", "evt-b", "evt-c"}

	for _, evtType := range want {
		r := &storage.EventRecord{
			Timestamp:     time.Now().UTC(),
			AggregateKind: "order",
			AggregateID:   "order-seq",
			EventType:     evtType,
			Payload:       []byte("{}"),
		}
		encoded, err := r.Encode()
		if err != nil {
			t.Fatalf("Encode failed: %v", err)
		}
		buf.Write(encoded)
	}

	reader := bytes.NewReader(buf.Bytes())
	for i, wantType := range want {
		record, err := storage.DecodeNext(reader)
		if err != nil {
			t.Fatalf("DecodeNext record %d failed: %v", i, err)
		}
		if record.EventType != wantType {
			t.Errorf("record %d: got EventType %q, want %q", i, record.EventType, wantType)
		}
	}

	if _, err := storage.DecodeNext(reader); err != storage.ErrEOF {
		t.Errorf("expected ErrEOF after last record, got: %v", err)
	}
}

func TestSnapshotRecord_EncodeDecode_Roundtrip(t *testing.T) {
	original := &storage.SnapshotRecord{
		Timestamp:     time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
		Version:       7,
		AggregateKind: "order",
		AggregateID:   "order-123",
		StateJSON:     []byte(`{"status":"PAID","total":42.5}`),
	}

	encoded, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := storage.DecodeNextSnapshot(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeNextSnapshot failed: %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("Version mismatch: got %d, want %d", decoded.Version, original.Version)
	}
	if decoded.AggregateKind != original.AggregateKind {
		t.Errorf("AggregateKind mismatch: got %q, want %q", decoded.AggregateKind, original.AggregateKind)
	}
	if decoded.AggregateID != original.AggregateID {
		t.Errorf("AggregateID mismatch: got %q, want %q", decoded.AggregateID, original.AggregateID)
	}
	if !bytes.Equal(decoded.StateJSON, original.StateJSON) {
		t.Errorf("StateJSON mismatch: got %q, want %q", decoded.StateJSON, original.StateJSON)
	}
}

func TestSnapshotRecord_DetectsChecksumCorruption(t *testing.T) {
	record := &storage.SnapshotRecord{
		Timestamp:     time.Now().UTC(),
		Version:       1,
		AggregateKind: "order",
		AggregateID:   "order-1",
		StateJSON:     []byte(`{"status":"CREATED"}`),
	}

	encoded, err := record.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	corrupted := make([]byte, len(encoded))
	copy(corrupted, encoded)
	corrupted[len(corrupted)-3] ^= 0xFF

	_, err = storage.DecodeNextSnapshot(bytes.NewReader(corrupted))
	if err != storage.ErrSnapshotChecksumMismatch {
		t.Errorf("expected ErrSnapshotChecksumMismatch, got: %v", err)
	}
}
