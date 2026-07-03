package db

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/x-name15/replaydb/cmd/recli/helper"
	"github.com/x-name15/replaydb/pkg/wire"
)

type ImportEvent struct {
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	EventType string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func RunImport(serverAddr string, args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	filePath := fs.String("file", "", "path to the JSON file containing an array of events")

	fs.Usage = func() {
		fmt.Println("Usage: recli import --file <path_to_events.json>")
		fmt.Println("The file must contain a JSON array like:")
		fmt.Println(`  [{"kind":"order", "id":"123", "type":"Created", "payload":{"foo":"bar"}}]`)
		fs.PrintDefaults()
	}

	fs.Parse(args)
	if *filePath == "" {
		fs.Usage()
		os.Exit(1)
	}

	data, err := os.ReadFile(*filePath)
	if err != nil {
		fmt.Printf("× Failed to read file: %v\n", err)
		os.Exit(1)
	}

	var rawEvents []ImportEvent
	if err := json.Unmarshal(data, &rawEvents); err != nil {
		fmt.Printf("× Invalid JSON format (must be an array of objects): %v\n", err)
		os.Exit(1)
	}

	batch := make([]wire.BatchEvent, len(rawEvents))
	for i, ev := range rawEvents {
		if ev.Kind == "" || ev.ID == "" || ev.EventType == "" {
			fmt.Printf("× Event at index %d is missing required fields (kind, id, type)\n", i)
			os.Exit(1)
		}
		batch[i] = wire.BatchEvent{
			Kind:      ev.Kind,
			ID:        ev.ID,
			EventType: ev.EventType,
			Payload:   []byte(ev.Payload),
		}
	}

	fmt.Printf("Preparing to import %d events in a single batch...\n", len(batch))

	resp, err := helper.DialAndRoundTrip(serverAddr, &wire.Request{
		Op:    wire.OpAppendBatch,
		Batch: batch,
	})

	if err != nil {
		fmt.Printf("× Network error: %v\n", err)
		os.Exit(1)
	}

	if resp.Status == wire.StatusOK {
		fmt.Println("☑ Import successful! All events were committed to ReplayDB.")
	} else {
		fmt.Printf("× Server error: %s\n", resp.Message)
		os.Exit(1)
	}
}
