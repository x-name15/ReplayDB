package db

import (
	"flag"
	"fmt"
	"os"

	"github.com/x-name15/replaydb/cmd/recli/helper"
	"github.com/x-name15/replaydb/pkg/wire"
)

func RunAppend(serverAddr string, args []string) {
	fs := flag.NewFlagSet("append", flag.ExitOnError)
	kind := fs.String("kind", "", "aggregate kind (e.g. order)")
	id := fs.String("id", "", "aggregate ID")
	eventType := fs.String("type", "", "event type (e.g. OrderCreated)")
	payload := fs.String("payload", "", "JSON payload for the event")
	fs.Usage = func() {
		fmt.Println("Usage: recli append --kind <kind> --id <id> --type <event_type> --payload <json>")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if *kind == "" || *id == "" || *eventType == "" || *payload == "" {
		fs.Usage()
		os.Exit(1)
	}

	resp, err := helper.DialAndRoundTrip(serverAddr, &wire.Request{
		Op:        wire.OpAppend,
		Kind:      *kind,
		ID:        *id,
		EventType: *eventType,
		Payload:   []byte(*payload),
	})
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	if resp.Status == wire.StatusOK {
		fmt.Println("☑ Event successfully committed to ReplayDB.")
	} else {
		fmt.Printf("× Server error: %s\n", resp.Message)
		os.Exit(1)
	}
}
