package wireserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
	"github.com/x-name15/replaydb/pkg/wire"
)

const ReadTimeout = 30 * time.Second
const WriteTimeout = 15 * time.Second

func HandleConnection(conn net.Conn, appender *engine.Appender, dirPath string, registry *domain.Registry, index *engine.Index, authToken string) {
	defer conn.Close()
	if authToken != "" {
		if err := conn.SetReadDeadline(time.Now().Add(ReadTimeout)); err != nil {
			return
		}
		got, err := wire.ReadAuthToken(conn)
		if err != nil {
			log.Printf("[auth] ✗ %s — handshake read failed: %v\n", conn.RemoteAddr(), err)
			return
		}
		if !wire.TokensEqual(got, authToken) {
			log.Printf("[auth] ✗ %s — invalid token\n", conn.RemoteAddr())
			WriteErr(conn, "authentication failed")
			return
		}
	}
	for {
		if err := conn.SetReadDeadline(time.Now().Add(ReadTimeout)); err != nil {
			return
		}
		req, err := wire.ReadRequest(conn)
		if err != nil {
			return
		}
		switch req.Op {
		case wire.OpAppend:
			if err := appender.Append(req.Kind, req.ID, req.EventType, req.Payload); err != nil {
				WriteErr(conn, fmt.Sprintf("storage engine error: %v", err))
				continue
			}
			WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "event logged"})
		case wire.OpAppendBatch:
			if err := appender.AppendBatch(req.Batch); err != nil {
				WriteErr(conn, fmt.Sprintf("storage engine error on batch: %v", err))
				continue
			}
			WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "batch logged successfully"})
		case wire.OpReplay:
			targetTime := time.Unix(0, req.TargetTS)
			state, err := engine.ReplayStateAt(dirPath, req.Kind, req.ID, targetTime, registry, index)
			if err != nil {
				WriteErr(conn, fmt.Sprintf("time travel processing failure: %v", err))
				continue
			}
			stateBytes, _ := json.Marshal(state)
			WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Body: stateBytes})
		case wire.OpSnapshot:
			state, err := engine.ReplayStateAt(dirPath, req.Kind, req.ID, time.Now().UTC(), registry, index)
			if err != nil {
				WriteErr(conn, fmt.Sprintf("failed to reconstruct state for snapshot: %v", err))
				continue
			}
			stateBytes, _ := json.Marshal(state)
			if err := appender.SaveSnapshot(req.Kind, req.ID, state.Version(), stateBytes); err != nil {
				WriteErr(conn, fmt.Sprintf("snapshot dump failure: %v", err))
				continue
			}
			WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "snapshot persisted"})
		case wire.OpWatch:
			_ = conn.SetReadDeadline(time.Time{})
			ch := make(chan wire.BatchEvent, 128)
			appender.RegisterWatcher(ch)
			defer appender.RemoveWatcher(ch)
			WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "subscribed"})
			for ev := range ch {
				if req.Kind != "" && ev.Kind != req.Kind {
					continue
				}
				if req.ID != "" && ev.ID != req.ID {
					continue
				}
				evBytes, _ := json.Marshal(ev)
				if err := conn.SetWriteDeadline(time.Now().Add(WriteTimeout)); err != nil {
					return
				}
				if err := wire.WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Body: evBytes}); err != nil {
					return
				}
			}
			return
		case wire.OpCompact:
			log.Printf("[NETWORK] Received Compact request from %s\n", conn.RemoteAddr())
			if err := appender.Compact(dirPath); err != nil {
				WriteErr(conn, fmt.Sprintf("compaction failed: %v", err))
				continue
			}
			WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "compaction completed successfully"})
		
		default:
			WriteErr(conn, "unknown opcode")
		}
	}
}

func WriteResponse(conn net.Conn, resp *wire.Response) {
	if err := conn.SetWriteDeadline(time.Now().Add(WriteTimeout)); err != nil {
		return
	}
	if err := wire.WriteResponse(conn, resp); err != nil {
		log.Printf("[wire] ✗ write failed for %s: %v\n", conn.RemoteAddr(), err)
	}
}

func WriteErr(conn net.Conn, msg string) {
	log.Printf("[wire] ✗ %v\n", msg)
	WriteResponse(conn, &wire.Response{Status: wire.StatusErr, Message: msg})
}
