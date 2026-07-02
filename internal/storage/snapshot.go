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

var ErrCorruptSnapshot = errors.New("storage: invalid snapshot magic bytes detected")
var ErrSnapshotChecksumMismatch = errors.New("storage: snapshot CRC32 checksum mismatch")
var SnapshotMagicBytes = [2]byte{'S', 'N'}

const (
	maxSnapshotKindLen  = 1 << 12
	maxSnapshotIDLen    = 1 << 12
	maxSnapshotStateLen = 128 * 1 << 20
)

type SnapshotRecord struct {
	Timestamp     time.Time
	Version       uint32
	AggregateKind string
	AggregateID   string
	StateJSON     []byte
}

func (s *SnapshotRecord) Encode() ([]byte, error) {
	kindBytes := []byte(s.AggregateKind)
	idBytes := []byte(s.AggregateID)

	if len(kindBytes) > maxSnapshotKindLen {
		return nil, fmt.Errorf("%w: aggregate kind is %d bytes (max %d)", ErrFieldTooLarge, len(kindBytes), maxSnapshotKindLen)
	}
	if len(idBytes) > maxSnapshotIDLen {
		return nil, fmt.Errorf("%w: aggregate id is %d bytes (max %d)", ErrFieldTooLarge, len(idBytes), maxSnapshotIDLen)
	}
	if len(s.StateJSON) > maxSnapshotStateLen {
		return nil, fmt.Errorf("%w: state JSON is %d bytes (max %d)", ErrFieldTooLarge, len(s.StateJSON), maxSnapshotStateLen)
	}

	body := new(bytes.Buffer)

	if err := binary.Write(body, binary.LittleEndian, s.Timestamp.UnixNano()); err != nil {
		return nil, fmt.Errorf("storage: encode timestamp: %w", err)
	}
	if err := binary.Write(body, binary.LittleEndian, s.Version); err != nil {
		return nil, fmt.Errorf("storage: encode version: %w", err)
	}

	kindLen := uint16(len(kindBytes))
	idLen := uint16(len(idBytes))
	stateLen := uint32(len(s.StateJSON))

	if err := binary.Write(body, binary.LittleEndian, kindLen); err != nil {
		return nil, fmt.Errorf("storage: encode kindLen: %w", err)
	}
	if err := binary.Write(body, binary.LittleEndian, idLen); err != nil {
		return nil, fmt.Errorf("storage: encode idLen: %w", err)
	}
	if err := binary.Write(body, binary.LittleEndian, stateLen); err != nil {
		return nil, fmt.Errorf("storage: encode stateLen: %w", err)
	}

	body.Write(kindBytes)
	body.Write(idBytes)
	body.Write(s.StateJSON)

	checksum := crc32.ChecksumIEEE(body.Bytes())

	out := new(bytes.Buffer)
	if err := binary.Write(out, binary.LittleEndian, SnapshotMagicBytes); err != nil {
		return nil, fmt.Errorf("storage: encode magic bytes: %w", err)
	}
	out.Write(body.Bytes())
	if err := binary.Write(out, binary.LittleEndian, checksum); err != nil {
		return nil, fmt.Errorf("storage: encode checksum: %w", err)
	}

	return out.Bytes(), nil
}

func DecodeNextSnapshot(r io.Reader) (*SnapshotRecord, error) {
	var magic [2]byte
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}
	if magic != SnapshotMagicBytes {
		return nil, ErrCorruptSnapshot
	}

	body := new(bytes.Buffer)
	tee := io.TeeReader(r, body)

	var tsNano int64
	var version uint32
	if err := binary.Read(tee, binary.LittleEndian, &tsNano); err != nil {
		return nil, fmt.Errorf("storage: read timestamp: %w", err)
	}
	if err := binary.Read(tee, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("storage: read version: %w", err)
	}

	var kindLen, idLen uint16
	var stateLen uint32
	if err := binary.Read(tee, binary.LittleEndian, &kindLen); err != nil {
		return nil, fmt.Errorf("storage: read kindLen: %w", err)
	}
	if err := binary.Read(tee, binary.LittleEndian, &idLen); err != nil {
		return nil, fmt.Errorf("storage: read idLen: %w", err)
	}
	if err := binary.Read(tee, binary.LittleEndian, &stateLen); err != nil {
		return nil, fmt.Errorf("storage: read stateLen: %w", err)
	}

	if int(kindLen) > maxSnapshotKindLen {
		return nil, fmt.Errorf("%w: kindLen %d exceeds max %d", ErrFieldTooLarge, kindLen, maxSnapshotKindLen)
	}
	if int(idLen) > maxSnapshotIDLen {
		return nil, fmt.Errorf("%w: idLen %d exceeds max %d", ErrFieldTooLarge, idLen, maxSnapshotIDLen)
	}
	if stateLen > maxSnapshotStateLen {
		return nil, fmt.Errorf("%w: stateLen %d exceeds max %d", ErrFieldTooLarge, stateLen, maxSnapshotStateLen)
	}

	kindBytes := make([]byte, kindLen)
	if _, err := io.ReadFull(tee, kindBytes); err != nil {
		return nil, fmt.Errorf("storage: read kind: %w", err)
	}
	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(tee, idBytes); err != nil {
		return nil, fmt.Errorf("storage: read id: %w", err)
	}
	stateBytes := make([]byte, stateLen)
	if _, err := io.ReadFull(tee, stateBytes); err != nil {
		return nil, fmt.Errorf("storage: read state: %w", err)
	}

	var storedChecksum uint32
	if err := binary.Read(r, binary.LittleEndian, &storedChecksum); err != nil {
		return nil, fmt.Errorf("storage: read checksum: %w", err)
	}
	if crc32.ChecksumIEEE(body.Bytes()) != storedChecksum {
		return nil, ErrSnapshotChecksumMismatch
	}

	return &SnapshotRecord{
		Timestamp:     time.Unix(0, tsNano),
		Version:       version,
		AggregateKind: string(kindBytes),
		AggregateID:   string(idBytes),
		StateJSON:     stateBytes,
	}, nil
}
