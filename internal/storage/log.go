package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

var (
	ErrCorruptData = errors.New(".redb storage corruption: invalid magic bytes detected")
	ErrEOF         = io.EOF
)

// MagicBytes enforces file format verification at byte index 0
var MagicBytes = [2]byte{'R', 'E'}

// EventRecord represents the raw layout written directly to disk
type EventRecord struct {
	Timestamp   time.Time
	AggregateID string
	EventType   string
	Payload     []byte
}

// Encode packs the event into our customized binary specification format
func (e *EventRecord) Encode() ([]byte, error) {
	buf := new(bytes.Buffer)

	// 1. Write Magic Bytes (2 bytes)
	if err := binary.Write(buf, binary.LittleEndian, MagicBytes); err != nil {
		return nil, err
	}

	// 2. Write Timestamp Unix Nano tracking (8 bytes - int64)
	if err := binary.Write(buf, binary.LittleEndian, e.Timestamp.UnixNano()); err != nil {
		return nil, err
	}

	idBytes := []byte(e.AggregateID)
	typeBytes := []byte(e.EventType)
	
	idLen := uint16(len(idBytes))
	typeLen := uint16(len(typeBytes))
	payloadLen := uint32(len(e.Payload))

	// 3. Write Explicit Header Offset Lengths (2 + 2 + 4 = 8 bytes)
	binary.Write(buf, binary.LittleEndian, idLen)
	binary.Write(buf, binary.LittleEndian, typeLen)
	binary.Write(buf, binary.LittleEndian, payloadLen)

	// 4. Append Variable Data Blocks
	buf.Write(idBytes)
	buf.Write(typeBytes)
	buf.Write(e.Payload)

	return buf.Bytes(), nil
}

// DecodeNext extracts exactly one EventRecord from a streaming Reader sequence
func DecodeNext(r io.Reader) (*EventRecord, error) {
	var magic [2]byte
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}
	if magic != MagicBytes {
		return nil, ErrCorruptData
	}

	var tsNano int64
	if err := binary.Read(r, binary.LittleEndian, &tsNano); err != nil {
		return nil, err
	}

	var idLen, typeLen uint16
	var payloadLen uint32
	binary.Read(r, binary.LittleEndian, &idLen)
	binary.Read(r, binary.LittleEndian, &typeLen)
	binary.Read(r, binary.LittleEndian, &payloadLen)

	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(r, idBytes); err != nil {
		return nil, err
	}

	typeBytes := make([]byte, typeLen)
	if _, err := io.ReadFull(r, typeBytes); err != nil {
		return nil, err
	}

	payloadBytes := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payloadBytes); err != nil {
		return nil, err
	}

	return &EventRecord{
		Timestamp:   time.Unix(0, tsNano),
		AggregateID: string(idBytes),
		EventType:   string(typeBytes),
		Payload:     payloadBytes,
	}, nil
}