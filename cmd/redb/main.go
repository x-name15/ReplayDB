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
	"github.com/x-name15/replaydb/internal/wire"
)

// connReadTimeout bounds how long the server waits for a client to send a
// request before dropping the connection.
const connReadTimeout = 30 * time.Second

func main() {
	if err := helper.Load(".env"); err != nil {
		log.Printf("⚠ Warning: Error reading .env file: %v\n", err)
	}

	port := helper.GetEnv("REDB_PORT", "7800")
	httpPort := helper.GetEnv("REDB_HTTP_PORT", ":8080")
	dirPath := helper.GetEnv("REDB_DATA_DIR", "data")

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

	// index.Rebuild logs its own summary (aggregates/events/corrupt count).
	index := engine.NewIndex()
	if err := index.Rebuild(dirPath); err != nil {
		log.Fatalf("Critical: Failed to build event index: %v", err)
	}

	appender, err := engine.NewAppender(dirPath, index)
	if err != nil {
		log.Fatalf("Critical: Failed to initialize storage engine: %v", err)
	}
	defer appender.Close()

	go server.StartHTTPServer(httpPort, dirPath, registry, index)

	listener, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Critical: TCP binding failure on port %s: %v", port, err)
	}

	log.Printf("[boot] ReplayDB online — TCP %s | data dir %s\n", port, dirPath)

	var activeConns sync.WaitGroup

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("[shutdown] signal received, closing TCP listener...")
		listener.Close()
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
		log.Printf("[conn] ↳ opened %s\n", remote)
		metrics.ConnOpened()

		activeConns.Add(1)
		go func() {
			defer activeConns.Done()
			handleConnection(conn, appender, dirPath, registry, index)
			metrics.ConnClosed()
			log.Printf("[conn] ↲ closed %s\n", remote)
		}()
	}
}

func handleConnection(conn net.Conn, appender *engine.Appender, dirPath string, registry *domain.Registry, index *engine.Index) {
	defer conn.Close()

	for {
		if err := conn.SetReadDeadline(time.Now().Add(connReadTimeout)); err != nil {
			return
		}

		req, err := wire.ReadRequest(conn)
		if err != nil {
			return
		}

		switch req.Op {
		case wire.OpAppend:
			if err := appender.Append(req.Kind, req.ID, req.EventType, req.Payload); err != nil {
				writeErr(conn, fmt.Sprintf("storage engine error: %v", err))
				continue
			}
			wire.WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "event logged"})

		case wire.OpReplay:
			targetTime := time.Unix(0, req.TargetTS)
			state, err := engine.ReplayStateAt(dirPath, req.Kind, req.ID, targetTime, registry, index)
			if err != nil {
				writeErr(conn, fmt.Sprintf("time travel processing failure: %v", err))
				continue
			}
			stateBytes, _ := json.Marshal(state)
			wire.WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Body: stateBytes})

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
			wire.WriteResponse(conn, &wire.Response{Status: wire.StatusOK, Message: "snapshot persisted"})

		default:
			writeErr(conn, "unknown opcode")
		}
	}
}

func writeErr(conn net.Conn, msg string) {
	log.Printf("[wire] ✗ %v\n", msg)
	wire.WriteResponse(conn, &wire.Response{Status: wire.StatusErr, Message: msg})
}
