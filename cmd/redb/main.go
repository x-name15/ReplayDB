package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/x-name15/replaydb/internal/engine"
	"github.com/x-name15/replaydb/internal/helper"
	"github.com/x-name15/replaydb/internal/server"
)

func main() {
	if err := helper.Load(".env"); err != nil {
		log.Printf("⚠️ Advertencia: Error leyendo .env: %v\n", err)
	}

	port := helper.GetEnv("REDB_PORT", "7800")
	httpPort := helper.GetEnv("REDB_HTTP_PORT", ":8080") // 👈 Puerto HTTP por defecto
	dirPath := helper.GetEnv("REDB_DATA_DIR", "data")
	
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	if !strings.HasPrefix(httpPort, ":") {
		httpPort = ":" + httpPort
	}

	os.MkdirAll(dirPath, 0755)

	appender, err := engine.NewAppender(dirPath)
	if err != nil {
		log.Fatalf("Critical: Failed to initialize storage engine: %v", err)
	}
	defer appender.Close()

	go server.StartHTTPServer(httpPort, dirPath)

	listener, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Critical: TCP binding failure on port %s: %v", port, err)
	}
	defer listener.Close()

	fmt.Printf("🚀 ReplayDB Server online. Listening on traditional TCP port %s\n", port)
	fmt.Printf("📦 Storage path bound to directory: %s\n", dirPath)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("⚠️ Network connection expansion error: %v\n", err)
			continue
		}
		go handleConnection(conn, appender, dirPath)
	}
}

func handleConnection(conn net.Conn, appender *engine.Appender, dirPath string) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 4)
		command := parts[0]

		switch command {
		case "APPEND":
			if len(parts) < 4 {
				conn.Write([]byte("ERR|Invalid APPEND wire protocol format\n"))
				continue
			}
			id := parts[1]
			evtType := parts[2]
			payload := []byte(parts[3])

			if err := appender.Append(id, evtType, payload); err != nil {
				conn.Write([]byte(fmt.Sprintf("ERR|Storage engine error: %v\n", err)))
				continue
			}
			conn.Write([]byte("OK|Event logged\n"))

		case "REPLAY":
			if len(parts) < 3 {
				conn.Write([]byte("ERR|Invalid REPLAY wire protocol format\n"))
				continue
			}
			id := parts[1]
			timeStr := parts[2]

			targetTime, err := time.Parse(time.RFC3339, timeStr)
			if err != nil {
				conn.Write([]byte("ERR|Invalid time layout, must follow strict RFC3339 specifications\n"))
				continue
			}

			state, err := engine.ReplayStateAt(dirPath, id, targetTime)
			if err != nil {
				conn.Write([]byte(fmt.Sprintf("ERR|Time travel processing failure: %v\n", err)))
				continue
			}

			stateBytes, _ := json.Marshal(state)
			conn.Write([]byte(fmt.Sprintf("OK|%s\n", string(stateBytes))))

		case "SNAPSHOT":
			if len(parts) < 2 {
				conn.Write([]byte("ERR|Invalid SNAPSHOT wire protocol format\n"))
				continue
			}
			id := parts[1]
			
			// 1. Reconstruimos el estado actual hasta AHORA
			state, err := engine.ReplayStateAt(dirPath, id, time.Now().UTC())
			if err != nil {
				conn.Write([]byte(fmt.Sprintf("ERR|Failed to reconstruct state for snapshot: %v\n", err)))
				continue
			}

			stateBytes, _ := json.Marshal(state)
			
			// 2. Guardamos la foto en disco. 
			// NOTA: Temporalmente pasamos Version '0'. En la próxima fase lo haremos dinámico.
			if err := appender.SaveSnapshot(id, state.Version, stateBytes); err != nil {
				conn.Write([]byte(fmt.Sprintf("ERR|Snapshot dump failure: %v\n", err)))
				continue
			}
			
			conn.Write([]byte("OK|Snapshot manually generated and persisted\n"))

		default:
			conn.Write([]byte("ERR|Unknown system command instruction\n"))
		}
	}
}