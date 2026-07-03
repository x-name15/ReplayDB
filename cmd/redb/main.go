package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
	"github.com/x-name15/replaydb/internal/helper"
	"github.com/x-name15/replaydb/internal/metrics"
	"github.com/x-name15/replaydb/internal/server"
	"github.com/x-name15/replaydb/pkg/wire"
)

const connReadTimeout = 30 * time.Second
const connWriteTimeout = 15 * time.Second

func main() {
	if err := helper.Load(".env"); err != nil {
		log.Printf("⚠ Warning: Error reading .env file: %v\n", err)
	}
	port := helper.GetEnv("REDB_PORT", "7800")
	httpPort := helper.GetEnv("REDB_HTTP_PORT", ":8080")
	dirPath := helper.GetEnv("REDB_DATA_DIR", "data")
	authToken := helper.GetEnv("REDB_AUTH_TOKEN", "")
	maxConns := helper.GetEnvInt("REDB_MAX_CONNECTIONS", 500)
	maxPayloadBytes := helper.GetEnvInt("REDB_MAX_PAYLOAD_BYTES", 4*1024*1024)
	wire.SetMaxFieldLen(uint32(maxPayloadBytes))
	archiveDir := helper.GetEnv("REDB_ARCHIVE_DIR", "")
	archiveIntervalRaw := helper.GetEnv("REDB_ARCHIVE_INTERVAL", "")
	var archiveInterval time.Duration
	if archiveIntervalRaw != "" {
		parsed, err := time.ParseDuration(archiveIntervalRaw)
		if err != nil {
			log.Printf("⚠ [boot] invalid REDB_ARCHIVE_INTERVAL %q, archiving disabled: %v\n", archiveIntervalRaw, err)
		} else {
			archiveInterval = parsed
		}
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	if !strings.HasPrefix(httpPort, ":") {
		httpPort = ":" + httpPort
	}
	os.MkdirAll(dirPath, 0755)
	registry := domain.NewRegistry()
	registry.Register("order", func(id string) domain.Aggregate {
		return domain.NewOrderState(id)
	})
	log.Println("[boot] registered aggregate kinds: order")
	index := engine.NewIndex()
	if err := index.Rebuild(dirPath); err != nil {
		log.Fatalf("Critical: Failed to build event index: %v", err)
	}
	appender, err := engine.NewAppender(dirPath, index)
	if err != nil {
		log.Fatalf("Critical: Failed to initialize storage engine: %v", err)
	}
	defer appender.Close()
	var archiver *engine.Archiver
	if archiveDir != "" && archiveInterval > 0 {
		archiver = engine.NewArchiver(dirPath, archiveDir, archiveInterval)
		go archiver.Run()
	} else {
		log.Println("[archive] disabled (set REDB_ARCHIVE_DIR and REDB_ARCHIVE_INTERVAL to enable periodic, non-destructive backups)")
	}
	go server.StartHTTPServer(httpPort, dirPath, registry, index)
	listener, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Critical: TCP binding failure on port %s: %v", port, err)
	}
	if authToken == "" {
		log.Println("⚠ [boot] REDB_AUTH_TOKEN not set — TCP wire protocol is running WITHOUT authentication. Anyone reaching this port can read and write your event store. Set REDB_AUTH_TOKEN before exposing it beyond localhost/a trusted network.")
	} else {
		log.Println("[boot] TCP wire protocol authentication enabled")
	}
	log.Printf("[boot] ReplayDB online — TCP %s | data dir %s\n", port, dirPath)
	log.Printf("[boot] max concurrent connections: %d | max payload per field: %d bytes\n", maxConns, maxPayloadBytes)
	var activeConns sync.WaitGroup
	connSem := make(chan struct{}, maxConns)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Println("[shutdown] signal received, closing TCP listener...")
		listener.Close()
		if archiver != nil {
			archiver.Stop()
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				log.Println("[shutdown] listener closed, waiting for in-flight connections...")
				activeConns.Wait()
				log.Println("[shutdown] ReplayDB shut down cleanly.")
				return
			default:
				log.Printf("⚠ Network connection error: %v\n", err)
				continue
			}
		}
		remote := conn.RemoteAddr().String()
		select {
		case connSem <- struct{}{}:
		default:
			log.Printf("[conn] ✗ rejected %s — max concurrent connections (%d) reached\n", remote, maxConns)
			writeErr(conn, "server at max connection capacity, try again later")
			conn.Close()
			continue
		}
		log.Printf("[conn] ↳ opened %s\n", remote)
		metrics.ConnOpened()
		activeConns.Add(1)
		go func() {
			defer activeConns.Done()
			defer func() { <-connSem }()
			handleConnection(conn, appender, dirPath, registry, index, authToken)
			metrics.ConnClosed()
			log.Printf("[conn] ↲ closed %s\n", remote)
		}()
	}
}

func handleConnection(conn net.Conn, appender *engine.Appender, dirPath string, registry *domain.Registry, index *engine.Index, authToken string) {
	defer conn.Close()
	if authToken != "" {
		if err := conn.SetReadDeadline(time.Now().Add(connReadTimeout)); err != nil {
			return
		}
		got, err := wire.ReadAuthToken(conn)
		if err != nil {
			log.Printf("[auth] ✗ %s — handshake read failed: %v\n", conn.RemoteAddr(), err)
			return
		}
		if !wire.TokensEqual(got, authToken) {
			log.Printf("[auth] ✗ %s — invalid token\n", conn.RemoteAddr())
			writeErr(conn, "authentication failed")
			return
		}
	}
	for {
		if err := conn.SetReadDeadline(time.Now().Add(connReadTimeout)); err != nil {
			return
		}
		req, err := wire.ReadRequest(conn)
		if err != nil {
			return
		}
		switch req.Op {

		case wire.OpAppendBatch:
			if err := appender.AppendBatch(req.Batch); err != nil {
				writeErr(conn, fmt.Sprintf("storage engine error on batch: %v", err))
				continue
			}
			writeResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "batch logged successfully"})

		case wire.OpReplay:
			targetTime := time.Unix(0, req.TargetTS)
			state, err := engine.ReplayStateAt(dirPath, req.Kind, req.ID, targetTime, registry, index)
			if err != nil {
				writeErr(conn, fmt.Sprintf("time travel processing failure: %v", err))
				continue
			}
			stateBytes, _ := json.Marshal(state)
			writeResponse(conn, &wire.Response{Status: wire.StatusOK, Body: stateBytes})

		case wire.OpSnapshot:
			state, err := engine.ReplayStateAt(dirPath, req.Kind, req.ID, time.Now().UTC(), registry, index)
			if err != nil {
				writeErr(conn, fmt.Sprintf("failed to reconstruct state for snapshot: %v", err))
				continue
			}
			stateBytes, _ := json.Marshal(state)
			if err := appender.SaveSnapshot(req.Kind, req.ID, state.Version(), stateBytes); err != nil {
				writeErr(conn, fmt.Sprintf("snapshot dump failure: %v", err))
				continue
			}
			writeResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "snapshot persisted"})

		case wire.OpWatch:
			_ = conn.SetReadDeadline(time.Time{})
			ch := make(chan wire.BatchEvent, 128)
			appender.RegisterWatcher(ch)
			defer appender.RemoveWatcher(ch)
			writeResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "subscribed"})
			for ev := range ch {
				if req.Kind != "" && ev.Kind != req.Kind {
					continue
				}
				if req.ID != "" && ev.ID != req.ID {
					continue
				}

				evBytes, _ := json.Marshal(ev)
				if err := conn.SetWriteDeadline(time.Now().Add(connWriteTimeout)); err != nil {
					return
				}
				if err := wire.WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Body: evBytes}); err != nil {
					return
				}
			}
			return
		default:
			writeErr(conn, "unknown opcode")
		}
	}
}

func writeResponse(conn net.Conn, resp *wire.Response) {
	if err := conn.SetWriteDeadline(time.Now().Add(connWriteTimeout)); err != nil {
		return
	}
	if err := wire.WriteResponse(conn, resp); err != nil {
		log.Printf("[wire] ✗ write failed for %s: %v\n", conn.RemoteAddr(), err)
	}
}

func writeErr(conn net.Conn, msg string) {
	log.Printf("[wire] ✗ %v\n", msg)
	writeResponse(conn, &wire.Response{Status: wire.StatusErr, Message: msg})
}
