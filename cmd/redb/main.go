package main

import (
	"context"
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
	"github.com/x-name15/replaydb/internal/wireserver"
	"github.com/x-name15/replaydb/pkg/wire"
)

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
			wireserver.WriteErr(conn, "server at max connection capacity, try again later")
			conn.Close()
			continue
		}
		log.Printf("[conn] ↳ opened %s\n", remote)
		metrics.ConnOpened()
		activeConns.Add(1)
		go func() {
			defer activeConns.Done()
			defer func() { <-connSem }()
			wireserver.HandleConnection(conn, appender, dirPath, registry, index, authToken)
			metrics.ConnClosed()
			log.Printf("[conn] ↲ closed %s\n", remote)
		}()
	}
}
