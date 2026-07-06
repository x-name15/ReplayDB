package db

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/x-name15/replaydb/cmd/recli/helper"
	"github.com/x-name15/replaydb/pkg/wire"
)

func RunWatch(serverAddr string, args []string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	kind := fs.String("kind", "", "filter by aggregate kind (optional, e.g. order)")
	id := fs.String("id", "", "filter by aggregate ID (optional)")
	fs.Usage = func() {
		fmt.Println("Usage: recli watch [--kind <kind>] [--id <id>]")
		fmt.Println("Streams events committed to ReplayDB in real time. Omit --kind/--id to watch everything.")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	conn, err := helper.DialAndStream(serverAddr, &wire.Request{
		Op:   wire.OpWatch,
		Kind: *kind,
		ID:   *id,
	})
	if err != nil {
		fmt.Printf("× %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n☑ Stopped watching.")
		conn.Close()
		os.Exit(0)
	}()

	resp, err := wire.ReadResponse(conn)
	if err != nil {
		fmt.Printf("× Failed to subscribe: %v\n", err)
		os.Exit(1)
	}
	if resp.Status != wire.StatusOK {
		fmt.Printf("× Server error: %s\n", resp.Message)
		os.Exit(1)
	}

	filterDesc := "all events"
	if *kind != "" || *id != "" {
		filterDesc = fmt.Sprintf("kind=%q id=%q", *kind, *id)
	}
	fmt.Printf("☑ Subscribed — watching %s. Press Ctrl+C to stop.\n\n", filterDesc)

	for {
		resp, err := wire.ReadResponse(conn)
		if err != nil {
			fmt.Println("\n☑ Connection closed by server.")
			return
		}
		if resp.Status != wire.StatusOK {
			fmt.Printf("× %s\n", resp.Message)
			continue
		}
		var ev wire.BatchEvent
		if err := json.Unmarshal(resp.Body, &ev); err != nil {
			fmt.Printf("× Failed to decode event: %v\n", err)
			continue
		}
		payloadStr := string(ev.Payload)
		var prettyPayload bytes.Buffer
		if json.Indent(&prettyPayload, ev.Payload, "", "  ") == nil {
			payloadStr = prettyPayload.String()
		}
		fmt.Printf("→ %s/%s %s\n%s\n\n", ev.Kind, ev.ID, ev.EventType, payloadStr)
	}
}
