package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// frameBuffer accumulates bytes for an outgoing frame body.
type frameBuffer struct {
	buf bytes.Buffer
}

func (f *frameBuffer) WriteByte(b byte) {
	f.buf.WriteByte(b)
}

func (f *frameBuffer) WriteField(data []byte) {
	var lenBytes [4]byte
	binary.LittleEndian.PutUint32(lenBytes[:], uint32(len(data)))
	f.buf.Write(lenBytes[:])
	f.buf.Write(data)
}

func (f *frameBuffer) WriteInt64(v int64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(v))
	f.buf.Write(b[:])
}

func (f *frameBuffer) Bytes() []byte {
	return f.buf.Bytes()
}

type frameReader struct {
	data []byte
	pos  int
}

func newFrameReader(data []byte) *frameReader {
	return &frameReader{data: data}
}

func (r *frameReader) ReadByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("wire: unexpected end of frame reading opcode/status byte")
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *frameReader) ReadField() ([]byte, error) {
	if r.pos+4 > len(r.data) {
		return nil, fmt.Errorf("wire: unexpected end of frame reading field length")
	}
	l := binary.LittleEndian.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	if l > maxFieldLen {
		return nil, fmt.Errorf("wire: field length %d exceeds max %d", l, maxFieldLen)
	}
	if r.pos+int(l) > len(r.data) {
		return nil, fmt.Errorf("wire: field length %d exceeds remaining frame data", l)
	}
	val := r.data[r.pos : r.pos+int(l)]
	r.pos += int(l)
	return val, nil
}

func (r *frameReader) ReadInt64() (int64, error) {
	if r.pos+8 > len(r.data) {
		return 0, fmt.Errorf("wire: unexpected end of frame reading int64")
	}
	v := int64(binary.LittleEndian.Uint64(r.data[r.pos : r.pos+8]))
	r.pos += 8
	return v, nil
}
