package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
	"github.com/x-name15/replaydb/internal/helper"
	"github.com/x-name15/replaydb/internal/server"
	"github.com/x-name15/replaydb/internal/wire"
)

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
	index := engine.NewIndex()
	log.Println("☑ Building event index from existing log...")
	indexStart := time.Now()
	if err := index.Rebuild(dirPath); err != nil {
		log.Fatalf("Critical: Failed to build event index: %v", err)
	}
	log.Printf("☑ Index built in %s\n", time.Since(indexStart))

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
	defer listener.Close()

	fmt.Printf("ReplayDB Server online. Listening on TCP port %s\n", port)
	fmt.Printf("Storage path bound to directory: %s\n", dirPath)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("⚠ Network connection error: %v\n", err)
			continue
		}
		go handleConnection(conn, appender, dirPath, registry, index)
	}
}

func handleConnection(conn net.Conn, appender *engine.Appender, dirPath string, registry *domain.Registry, index *engine.Index) {
	defer conn.Close()

	for {
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
	wire.WriteResponse(conn, &wire.Response{Status: wire.StatusErr, Message: msg})
}
