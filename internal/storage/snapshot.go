package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

var (
	ErrCorruptSnapshot = errors.New(".redb snapshot corruption: invalid magic bytes detected")
)

// SnapshotMagicBytes ensures we are reading a valid snapshot file
var SnapshotMagicBytes = [2]byte{'S', 'N'}

// SnapshotRecord represents a materialized view of an entity at a specific version
type SnapshotRecord struct {
	Timestamp   time.Time // When this snapshot was taken
	Version     uint32    // The last event version applied to this state
	AggregateID string
	StateJSON   []byte    // The fully serialized OrderState
}

// Encode packs the snapshot into our binary specification format
func (s *SnapshotRecord) Encode() ([]byte, error) {
	buf := new(bytes.Buffer)

	// 1. Magic Bytes (2 bytes)
	if err := binary.Write(buf, binary.LittleEndian, SnapshotMagicBytes); err != nil {
		return nil, err
	}

	// 2. Timestamp (8 bytes)
	if err := binary.Write(buf, binary.LittleEndian, s.Timestamp.UnixNano()); err != nil {
		return nil, err
	}

	// 3. Version (4 bytes) - Critical to know where to resume the event stream
	if err := binary.Write(buf, binary.LittleEndian, s.Version); err != nil {
		return nil, err
	}

	idBytes := []byte(s.AggregateID)
	idLen := uint16(len(idBytes))
	stateLen := uint32(len(s.StateJSON))

	// 4. Header Lengths (2 + 4 = 6 bytes)
	binary.Write(buf, binary.LittleEndian, idLen)
	binary.Write(buf, binary.LittleEndian, stateLen)

	// 5. Payload
	buf.Write(idBytes)
	buf.Write(s.StateJSON)

	return buf.Bytes(), nil
}

// DecodeNextSnapshot extracts one SnapshotRecord from a streaming Reader
func DecodeNextSnapshot(r io.Reader) (*SnapshotRecord, error) {
	var magic [2]byte
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}
	if magic != SnapshotMagicBytes {
		return nil, ErrCorruptSnapshot
	}

	var tsNano int64
	if err := binary.Read(r, binary.LittleEndian, &tsNano); err != nil {
		return nil, err
	}

	var version uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, err
	}

	var idLen uint16
	var stateLen uint32
	binary.Read(r, binary.LittleEndian, &idLen)
	binary.Read(r, binary.LittleEndian, &stateLen)

	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(r, idBytes); err != nil {
		return nil, err
	}

	stateBytes := make([]byte, stateLen)
	if _, err := io.ReadFull(r, stateBytes); err != nil {
		return nil, err
	}

	return &SnapshotRecord{
		Timestamp:   time.Unix(0, tsNano),
		Version:     version,
		AggregateID: string(idBytes),
		StateJSON:   stateBytes,
	}, nil
}