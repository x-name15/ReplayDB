package db

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/x-name15/replaydb/cmd/recli/helper"
	"github.com/x-name15/replaydb/pkg/wire"
)

func RunCompact(serverAddr string, args []string) {
	fs := flag.NewFlagSet("compact", flag.ExitOnError)

	fs.Usage = func() {
		fmt.Println("Usage: recli compact")
		fmt.Println("Triggers a background log compaction on the ReplayDB server to free up disk space.")
		fs.PrintDefaults()
	}

	fs.Parse(args)

	fmt.Println("Triggering Log Compaction...")
	start := time.Now()

	resp, err := helper.DialAndRoundTrip(serverAddr, &wire.Request{
		Op: wire.OpCompact,
	})

	if err != nil {
		fmt.Printf("× Network error: %v\n", err)
		os.Exit(1)
	}

	if resp.Status == wire.StatusOK {
		fmt.Printf("☑ Compaction completed successfully in %v\n", time.Since(start))
	} else {
		fmt.Printf("× Server error: %s\n", resp.Message)
		os.Exit(1)
	}
}