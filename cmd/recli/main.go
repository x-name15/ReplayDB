package main

import (
	"fmt"
	"os"

	"github.com/x-name15/replaydb/cmd/recli/db"
	"github.com/x-name15/replaydb/cmd/recli/helper"
	appHelper "github.com/x-name15/replaydb/internal/helper"
)

func main() {
	appHelper.Load(".env")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	serverAddr := appHelper.GetEnv("REDB_SERVER_ADDR", "localhost:7800")
	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "append":
		db.RunAppend(serverAddr, args)
	case "travel":
		helper.RunTravel(serverAddr, args)
	case "snapshot":
		db.RunSnapshot(serverAddr, args)
	case "import":
		db.RunImport(serverAddr, os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("× Unknown command: %q\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("reCLI - Wire Client for ReplayDB")
	fmt.Println("\nUsage:")
	fmt.Println("  recli <command> [flags]")
	fmt.Println("\nCommands:")
	fmt.Println("  append    Append an event to an aggregate's log")
	fmt.Println("  travel    Reconstruct an aggregate's state at a point in time")
	fmt.Println("  snapshot  Trigger a manual snapshot for an aggregate")
	fmt.Println("  help      Show this message")
	fmt.Println("\nRun 'recli <command> -h' for flags on a specific command.")
}
