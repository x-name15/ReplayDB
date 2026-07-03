package replaydb

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/x-name15/replaydb/pkg/wire"
)

type Config struct {
	Address   string
	AuthToken string
	Timeout   time.Duration
}

type Client interface {
	Append(ctx context.Context, kind, id, eventType string, payload []byte) error
	AppendBatch(ctx context.Context, events []wire.BatchEvent) error // NUEVO
	Travel(ctx context.Context, kind, id string, at time.Time) ([]byte, error)
	Snapshot(ctx context.Context, kind, id string) error
	Close() error
}

type replayClient struct {
	config Config
}

func NewClient(cfg Config) (Client, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("server address is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &replayClient{config: cfg}, nil
}

func (c *replayClient) executeRoundTrip(ctx context.Context, req *wire.Request) (*wire.Response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.config.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ReplayDB: %w", err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.config.Timeout)
	}
	_ = conn.SetDeadline(deadline)

	if err := wire.WriteRequest(conn, req); err != nil {
		return nil, fmt.Errorf("failed to send data over network: %w", err)
	}

	resp, err := wire.ReadResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from server: %w", err)
	}

	return resp, nil
}

func (c *replayClient) Append(ctx context.Context, kind, id, eventType string, payload []byte) error {
	req := &wire.Request{
		Op:        wire.OpAppend,
		Kind:      kind,
		ID:        id,
		EventType: eventType,
		Payload:   payload,
	}
	resp, err := c.executeRoundTrip(ctx, req)
	if err != nil {
		return err
	}
	if resp.Status != wire.StatusOK {
		return fmt.Errorf("engine error: %s", resp.Message)
	}
	return nil
}

func (c *replayClient) AppendBatch(ctx context.Context, events []wire.BatchEvent) error {
	if len(events) == 0 {
		return nil
	}
	req := &wire.Request{
		Op:    wire.OpAppendBatch,
		Batch: events,
	}
	resp, err := c.executeRoundTrip(ctx, req)
	if err != nil {
		return err
	}
	if resp.Status != wire.StatusOK {
		return fmt.Errorf("engine error on batch: %s", resp.Message)
	}
	return nil
}

func (c *replayClient) Travel(ctx context.Context, kind, id string, at time.Time) ([]byte, error) {
	var payload []byte
	if !at.IsZero() {
		payload = []byte(at.Format(time.RFC3339))
	}

	req := &wire.Request{
		Op:      wire.OpReplay,
		Kind:    kind,
		ID:      id,
		Payload: payload,
	}
	resp, err := c.executeRoundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Status != wire.StatusOK {
		return nil, fmt.Errorf("time-travel error: %s", resp.Message)
	}
	return resp.Body, nil
}

func (c *replayClient) Snapshot(ctx context.Context, kind, id string) error {
	req := &wire.Request{
		Op:   wire.OpSnapshot,
		Kind: kind,
		ID:   id,
	}
	resp, err := c.executeRoundTrip(ctx, req)
	if err != nil {
		return err
	}
	if resp.Status != wire.StatusOK {
		return fmt.Errorf("failed to generate snapshot: %s", resp.Message)
	}
	return nil
}

func (c *replayClient) Close() error {
	return nil
}