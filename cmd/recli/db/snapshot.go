package db

import (
	"flag"
	"fmt"
	"os"

	"github.com/x-name15/replaydb/cmd/recli/helper"
	"github.com/x-name15/replaydb/internal/wire"
)

func RunSnapshot(serverAddr string, args []string) {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	kind := fs.String("kind", "", "aggregate kind (e.g. order)")
	id := fs.String("id", "", "aggregate ID")
	fs.Usage = func() {
		fmt.Println("Usage: recli snapshot --kind <kind> --id <id>")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if *kind == "" || *id == "" {
		fs.Usage()
		os.Exit(1)
	}

	resp, err := helper.DialAndRoundTrip(serverAddr, &wire.Request{
		Op:   wire.OpSnapshot,
		Kind: *kind,
		ID:   *id,
	})
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	if resp.Status == wire.StatusOK {
		fmt.Println("☑ Snapshot successfully generated and persisted.")
	} else {
		fmt.Printf("× Server error: %s\n", resp.Message)
		os.Exit(1)
	}
}
