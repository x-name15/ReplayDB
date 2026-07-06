package helper

import (
	"fmt"
	"net"
	"os"

	"github.com/x-name15/replaydb/pkg/wire"
)

func DialAndRoundTrip(serverAddr string, req *wire.Request) (*wire.Response, error) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("connection failure: ReplayDB engine offline at %s", serverAddr)
	}
	defer conn.Close()
	if token := os.Getenv("REDB_AUTH_TOKEN"); token != "" {
		if err := wire.WriteAuthToken(conn, token); err != nil {
			return nil, fmt.Errorf("failed to send auth token: %w", err)
		}
	}
	if err := wire.WriteRequest(conn, req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	return wire.ReadResponse(conn)
}

func DialAndStream(serverAddr string, req *wire.Request) (net.Conn, error) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("connection failure: ReplayDB engine offline at %s", serverAddr)
	}
	if token := os.Getenv("REDB_AUTH_TOKEN"); token != "" {
		if err := wire.WriteAuthToken(conn, token); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to send auth token: %w", err)
		}
	}
	if err := wire.WriteRequest(conn, req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	return conn, nil
}
