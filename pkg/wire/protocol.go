package wire

import (
	"encoding/binary"
	"fmt"
	"io"
)

type Opcode uint8

const (
	OpAppend      = 1
	OpReplay      = 2
	OpSnapshot    = 3
	OpAppendBatch = 4
)

type Status uint8

const (
	StatusOK Status = iota
	StatusErr
)

type Request struct {
	Op        Opcode
	Kind      string
	ID        string
	EventType string
	Payload   []byte
	TargetTS  int64
	Timestamp int64
	Batch     []BatchEvent
}

type Response struct {
	Status  Status
	Message string
	Body    []byte
}

type BatchEvent struct {
	Kind      string
	ID        string
	EventType string
	Payload   []byte
}

const defaultMaxFieldLen = 64 * 1024 * 1024

var maxFieldLen uint32 = defaultMaxFieldLen

func SetMaxFieldLen(n uint32) {
	if n == 0 {
		maxFieldLen = defaultMaxFieldLen
		return
	}
	maxFieldLen = n
}

func WriteRequest(w io.Writer, req *Request) error {
	body := new(frameBuffer)
	body.WriteByte(byte(req.Op))
	body.WriteField([]byte(req.Kind))
	body.WriteField([]byte(req.ID))

	switch req.Op {

	case OpAppend:
		body.WriteField([]byte(req.EventType))
		body.WriteField(req.Payload)

	case OpAppendBatch:
		body.WriteInt64(int64(len(req.Batch)))
		for _, ev := range req.Batch {
			body.WriteField([]byte(ev.Kind))
			body.WriteField([]byte(ev.ID))
			body.WriteField([]byte(ev.EventType))
			body.WriteField(ev.Payload)
		}

	case OpReplay:
		body.WriteInt64(req.TargetTS)

	case OpSnapshot:
	default:
		return fmt.Errorf("wire: unknown opcode %d", req.Op)
	}
	return writeFrame(w, body.Bytes())
}

func ReadRequest(r io.Reader) (*Request, error) {
	frame, err := readFrame(r)
	if err != nil {
		return nil, err
	}
	fr := newFrameReader(frame)
	opByte, err := fr.ReadByte()
	if err != nil {
		return nil, err
	}
	req := &Request{Op: Opcode(opByte)}

	kind, err := fr.ReadField()
	if err != nil {
		return nil, err
	}
	req.Kind = string(kind)

	id, err := fr.ReadField()
	if err != nil {
		return nil, err
	}
	req.ID = string(id)

	switch req.Op {

	case OpAppend:
		evtType, err := fr.ReadField()
		if err != nil {
			return nil, err
		}
		payload, err := fr.ReadField()
		if err != nil {
			return nil, err
		}
		req.EventType = string(evtType)
		req.Payload = payload

	case OpAppendBatch:
		count, err := fr.ReadInt64()
		if err != nil {
			return nil, err
		}
		req.Batch = make([]BatchEvent, count)
		for i := int64(0); i < count; i++ {
			evKind, err := fr.ReadField()
			if err != nil {
				return nil, err
			}
			evID, err := fr.ReadField()
			if err != nil {
				return nil, err
			}
			evType, err := fr.ReadField()
			if err != nil {
				return nil, err
			}
			evPayload, err := fr.ReadField()
			if err != nil {
				return nil, err
			}

			req.Batch[i] = BatchEvent{
				Kind:      string(evKind),
				ID:        string(evID),
				EventType: string(evType),
				Payload:   evPayload,
			}
		}

	case OpReplay:
		ts, err := fr.ReadInt64()
		if err != nil {
			return nil, err
		}
		req.TargetTS = ts

	case OpSnapshot:
	default:
		return nil, fmt.Errorf("wire: unknown opcode %d in frame", req.Op)
	}
	return req, nil
}

func WriteResponse(w io.Writer, resp *Response) error {
	body := new(frameBuffer)
	body.WriteByte(byte(resp.Status))
	body.WriteField([]byte(resp.Message))
	body.WriteField(resp.Body)
	return writeFrame(w, body.Bytes())
}

func ReadResponse(r io.Reader) (*Response, error) {
	frame, err := readFrame(r)
	if err != nil {
		return nil, err
	}
	fr := newFrameReader(frame)
	statusByte, err := fr.ReadByte()
	if err != nil {
		return nil, err
	}
	msg, err := fr.ReadField()
	if err != nil {
		return nil, err
	}
	body, err := fr.ReadField()
	if err != nil {
		return nil, err
	}
	return &Response{
		Status:  Status(statusByte),
		Message: string(msg),
		Body:    body,
	}, nil
}

func writeFrame(w io.Writer, body []byte) error {
	if err := binary.Write(w, binary.LittleEndian, uint32(len(body))); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var l uint32
	if err := binary.Read(r, binary.LittleEndian, &l); err != nil {
		return nil, err
	}
	if l > maxFieldLen {
		return nil, fmt.Errorf("wire: frame length %d exceeds max %d", l, maxFieldLen)
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
