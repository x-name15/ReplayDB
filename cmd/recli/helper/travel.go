package helper

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/x-name15/replaydb/internal/wire"
)

func RunTravel(serverAddr string, args []string) {
	fs := flag.NewFlagSet("travel", flag.ExitOnError)
	kind := fs.String("kind", "", "aggregate kind (e.g. order)")
	id := fs.String("id", "", "aggregate ID")
	at := fs.String("at", "", "target RFC3339 timestamp (e.g. 2026-07-02T10:15:00Z); defaults to now")
	fs.Usage = func() {
		fmt.Println("Usage: recli travel --kind <kind> --id <id> [--at <RFC3339_timestamp>]")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if *kind == "" || *id == "" {
		fs.Usage()
		os.Exit(1)
	}

	targetTime := time.Now().UTC()
	if *at != "" {
		parsed, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			fmt.Println("× Invalid --at format. Use strict RFC3339 (e.g. 2026-07-02T10:15:00Z)")
			os.Exit(1)
		}
		targetTime = parsed
	}

	resp, err := DialAndRoundTrip(serverAddr, &wire.Request{
		Op:       wire.OpReplay,
		Kind:     *kind,
		ID:       *id,
		TargetTS: targetTime.UnixNano(),
	})
	if err != nil {
		fmt.Printf("× %v\n", err)
		os.Exit(1)
	}
	if resp.Status != wire.StatusOK {
		fmt.Printf("× Server time-travel error: %s\n", resp.Message)
		os.Exit(1)
	}

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, resp.Body, "", "  "); err == nil {
		fmt.Println("☑ State successfully reconstructed:")
		fmt.Println(prettyJSON.String())
	} else {
		fmt.Printf("☑ Reconstructed State: %s\n", string(resp.Body))
	}
}
