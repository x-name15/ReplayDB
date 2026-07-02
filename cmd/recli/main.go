package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/x-name15/replaydb/internal/helper"
)

func main() {
	// Carga silenciosa del archivo .env local
	helper.Load(".env")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Usamos tu helper en lugar de la función duplicada
	serverAddr := helper.GetEnv("REDB_SERVER_ADDR", "localhost:7800")

	command := os.Args[1]
	switch command {
	case "append":
		if len(os.Args) < 5 {
			fmt.Println("❌ Usage: recli append <aggregate_id> <event_type> <json_payload>")
			os.Exit(1)
		}
		executeAppend(serverAddr, os.Args[2], os.Args[3], os.Args[4])

	case "travel":
		if len(os.Args) < 4 {
			fmt.Println("❌ Usage: recli travel <aggregate_id> <RFC3339_timestamp>")
			os.Exit(1)
		}
		executeTimeTravel(serverAddr, os.Args[2], os.Args[3])

	case "snapshot":
		if len(os.Args) < 3 {
			fmt.Println("❌ Usage: recli snapshot <aggregate_id>")
			os.Exit(1)
		}
		executeSnapshot(serverAddr, os.Args[2])

	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown execution flag: '%s'\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("reCLI - Production Wire Client for ReplayDB")
	fmt.Println("\nUsage:")
	fmt.Println("  recli [command]")
	fmt.Println("\nAvailable Commands:")
	fmt.Println("  append    Ingest a live tracking event execution block over TCP network")
	fmt.Println("  travel    Request server state dynamic reconstruction at targeted timestamp")
	fmt.Println("  snapshot  Trigger a manual binary state snapshot for a specific aggregate")
	fmt.Println("  help      Display infrastructure documentation parameters")
}

func executeAppend(serverAddr, id, evtType, payload string) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("❌ Connection failure: ReplayDB engine offline at %s\n", serverAddr)
		return
	}
	defer conn.Close()

	wireCommand := fmt.Sprintf("APPEND|%s|%s|%s\n", id, evtType, payload)
	conn.Write([]byte(wireCommand))

	response, _ := bufio.NewReader(conn).ReadString('\n')
	response = strings.TrimSpace(response)

	if strings.HasPrefix(response, "OK") {
		fmt.Println("✅ Event successfully committed to ReplayDB over network stream.")
	} else {
		fmt.Printf("❌ Server Exception: %s\n", response)
	}
}

func executeTimeTravel(serverAddr, id, timeStr string) {
	_, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		fmt.Printf("❌ Invalid time format. Use strict RFC3339 layout (e.g., 2026-07-02T10:15:00Z)\n")
		return
	}

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("❌ Connection failure: ReplayDB engine offline at %s\n", serverAddr)
		return
	}
	defer conn.Close()

	wireCommand := fmt.Sprintf("REPLAY|%s|%s\n", id, timeStr)
	conn.Write([]byte(wireCommand))

	response, _ := bufio.NewReader(conn).ReadString('\n')
	response = strings.TrimSpace(response)

	parts := strings.SplitN(response, "|", 2)
	if parts[0] == "OK" {
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, []byte(parts[1]), "", "  "); err == nil {
			fmt.Println("✅ State successfully reconstructed via remote Replay Engine:")
			fmt.Println(prettyJSON.String())
		} else {
			fmt.Printf("✅ Reconstructed State: %s\n", parts[1])
		}
	} else {
		fmt.Printf("❌ Server Time-Travel Engine Panic: %s\n", response)
	}
}

func executeSnapshot(serverAddr, id string) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		fmt.Printf("❌ Connection failure: ReplayDB engine offline at %s\n", serverAddr)
		return
	}
	defer conn.Close()

	wireCommand := fmt.Sprintf("SNAPSHOT|%s\n", id)
	conn.Write([]byte(wireCommand))

	response, _ := bufio.NewReader(conn).ReadString('\n')
	response = strings.TrimSpace(response)

	if strings.HasPrefix(response, "OK") {
		fmt.Println("📸 ✅ Snapshot successfully generated and persisted on the server.")
	} else {
		fmt.Printf("❌ Server Error: %s\n", response)
	}
}