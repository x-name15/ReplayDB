package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"time"
)

var (
	ErrCorruptData      = errors.New("storage: invalid magic bytes detected")
	ErrChecksumMismatch = errors.New("storage: CRC32 checksum mismatch, record is corrupt")
	ErrFieldTooLarge    = errors.New("storage: encoded field exceeds maximum allowed size")
	ErrEOF              = io.EOF
)

var MagicBytes = [2]byte{'R', 'E'}

const (
	maxKindLen    = 1 << 12      // 4 KiB
	maxIDLen      = 1 << 12      // 4 KiB
	maxEventLen   = 1 << 12      // 4 KiB
	maxPayloadLen = 64 * 1 << 20 // 64 MiB
)

type EventRecord struct {
	Timestamp     time.Time
	AggregateKind string
	AggregateID   string
	EventType     string
	Payload       []byte
}

func (e *EventRecord) Encode() ([]byte, error) {
	kindBytes := []byte(e.AggregateKind)
	idBytes := []byte(e.AggregateID)
	typeBytes := []byte(e.EventType)

	if len(kindBytes) > maxKindLen {
		return nil, fmt.Errorf("%w: aggregate kind is %d bytes (max %d)", ErrFieldTooLarge, len(kindBytes), maxKindLen)
	}
	if len(idBytes) > maxIDLen {
		return nil, fmt.Errorf("%w: aggregate id is %d bytes (max %d)", ErrFieldTooLarge, len(idBytes), maxIDLen)
	}
	if len(typeBytes) > maxEventLen {
		return nil, fmt.Errorf("%w: event type is %d bytes (max %d)", ErrFieldTooLarge, len(typeBytes), maxEventLen)
	}
	if len(e.Payload) > maxPayloadLen {
		return nil, fmt.Errorf("%w: payload is %d bytes (max %d)", ErrFieldTooLarge, len(e.Payload), maxPayloadLen)
	}

	body := new(bytes.Buffer)

	if err := binary.Write(body, binary.LittleEndian, e.Timestamp.UnixNano()); err != nil {
		return nil, fmt.Errorf("storage: encode timestamp: %w", err)
	}

	kindLen := uint16(len(kindBytes))
	idLen := uint16(len(idBytes))
	typeLen := uint16(len(typeBytes))
	payloadLen := uint32(len(e.Payload))

	if err := binary.Write(body, binary.LittleEndian, kindLen); err != nil {
		return nil, fmt.Errorf("storage: encode kindLen: %w", err)
	}
	if err := binary.Write(body, binary.LittleEndian, idLen); err != nil {
		return nil, fmt.Errorf("storage: encode idLen: %w", err)
	}
	if err := binary.Write(body, binary.LittleEndian, typeLen); err != nil {
		return nil, fmt.Errorf("storage: encode typeLen: %w", err)
	}
	if err := binary.Write(body, binary.LittleEndian, payloadLen); err != nil {
		return nil, fmt.Errorf("storage: encode payloadLen: %w", err)
	}

	body.Write(kindBytes)
	body.Write(idBytes)
	body.Write(typeBytes)
	body.Write(e.Payload)

	checksum := crc32.ChecksumIEEE(body.Bytes())

	out := new(bytes.Buffer)
	if err := binary.Write(out, binary.LittleEndian, MagicBytes); err != nil {
		return nil, fmt.Errorf("storage: encode magic bytes: %w", err)
	}
	out.Write(body.Bytes())
	if err := binary.Write(out, binary.LittleEndian, checksum); err != nil {
		return nil, fmt.Errorf("storage: encode checksum: %w", err)
	}

	return out.Bytes(), nil
}

func DecodeNext(r io.Reader) (*EventRecord, error) {
	var magic [2]byte
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}
	if magic != MagicBytes {
		return nil, ErrCorruptData
	}

	body := new(bytes.Buffer)
	tee := io.TeeReader(r, body)

	var tsNano int64
	if err := binary.Read(tee, binary.LittleEndian, &tsNano); err != nil {
		return nil, fmt.Errorf("storage: read timestamp: %w", err)
	}

	var kindLen, idLen, typeLen uint16
	var payloadLen uint32
	if err := binary.Read(tee, binary.LittleEndian, &kindLen); err != nil {
		return nil, fmt.Errorf("storage: read kindLen: %w", err)
	}
	if err := binary.Read(tee, binary.LittleEndian, &idLen); err != nil {
		return nil, fmt.Errorf("storage: read idLen: %w", err)
	}
	if err := binary.Read(tee, binary.LittleEndian, &typeLen); err != nil {
		return nil, fmt.Errorf("storage: read typeLen: %w", err)
	}
	if err := binary.Read(tee, binary.LittleEndian, &payloadLen); err != nil {
		return nil, fmt.Errorf("storage: read payloadLen: %w", err)
	}

	// Bound-check BEFORE allocating. A corrupt or maliciously crafted
	// length here must fail fast, not trigger a multi-GB make([]byte, n).
	if int(kindLen) > maxKindLen {
		return nil, fmt.Errorf("%w: kindLen %d exceeds max %d", ErrFieldTooLarge, kindLen, maxKindLen)
	}
	if int(idLen) > maxIDLen {
		return nil, fmt.Errorf("%w: idLen %d exceeds max %d", ErrFieldTooLarge, idLen, maxIDLen)
	}
	if int(typeLen) > maxEventLen {
		return nil, fmt.Errorf("%w: typeLen %d exceeds max %d", ErrFieldTooLarge, typeLen, maxEventLen)
	}
	if payloadLen > maxPayloadLen {
		return nil, fmt.Errorf("%w: payloadLen %d exceeds max %d", ErrFieldTooLarge, payloadLen, maxPayloadLen)
	}

	kindBytes := make([]byte, kindLen)
	if _, err := io.ReadFull(tee, kindBytes); err != nil {
		return nil, fmt.Errorf("storage: read kind: %w", err)
	}
	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(tee, idBytes); err != nil {
		return nil, fmt.Errorf("storage: read id: %w", err)
	}
	typeBytes := make([]byte, typeLen)
	if _, err := io.ReadFull(tee, typeBytes); err != nil {
		return nil, fmt.Errorf("storage: read event type: %w", err)
	}
	payloadBytes := make([]byte, payloadLen)
	if _, err := io.ReadFull(tee, payloadBytes); err != nil {
		return nil, fmt.Errorf("storage: read payload: %w", err)
	}

	var storedChecksum uint32
	if err := binary.Read(r, binary.LittleEndian, &storedChecksum); err != nil {
		return nil, fmt.Errorf("storage: read checksum: %w", err)
	}

	if crc32.ChecksumIEEE(body.Bytes()) != storedChecksum {
		return nil, ErrChecksumMismatch
	}

	return &EventRecord{
		Timestamp:     time.Unix(0, tsNano),
		AggregateKind: string(kindBytes),
		AggregateID:   string(idBytes),
		EventType:     string(typeBytes),
		Payload:       payloadBytes,
	}, nil
}
