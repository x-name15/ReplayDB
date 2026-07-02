package helper

import (
	"fmt"
	"net"

	"github.com/x-name15/replaydb/internal/wire"
)

func DialAndRoundTrip(serverAddr string, req *wire.Request) (*wire.Response, error) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("connection failure: ReplayDB engine offline at %s", serverAddr)
	}
	defer conn.Close()

	if err := wire.WriteRequest(conn, req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	return wire.ReadResponse(conn)
}
