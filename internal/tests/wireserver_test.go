package tests

import (
	"net"
	"testing"
	"time"

	"github.com/x-name15/replaydb/internal/domain"
	"github.com/x-name15/replaydb/internal/engine"
	"github.com/x-name15/replaydb/internal/wireserver"
	"github.com/x-name15/replaydb/pkg/wire"
)

func newWireTestAppender(t *testing.T) (*engine.Appender, *engine.Index) {
	t.Helper()
	dir := t.TempDir()
	index := engine.NewIndex()
	appender, err := engine.NewAppender(dir, index)
	if err != nil {
		t.Fatalf("NewAppender failed: %v", err)
	}
	t.Cleanup(func() { appender.Close() })
	return appender, index
}

func orderRegistry() *domain.Registry {
	r := domain.NewRegistry()
	r.Register("order", func(id string) domain.Aggregate {
		return domain.NewOrderState(id)
	})
	return r
}

func TestHandleConnection_RejectsWrongToken(t *testing.T) {
	appender, index := newWireTestAppender(t)
	client, srv := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		wireserver.HandleConnection(srv, appender, t.TempDir(), orderRegistry(), index, "correct-token")
		close(done)
	}()
	if err := wire.WriteAuthToken(client, "wrong-token"); err != nil {
		t.Fatalf("WriteAuthToken failed: %v", err)
	}
	resp, err := wire.ReadResponse(client)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if resp.Status != wire.StatusErr {
		t.Errorf("expected StatusErr for a wrong token, got %v", resp.Status)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("HandleConnection did not close the connection after rejecting an invalid token")
	}
}

func TestHandleConnection_SingleOpAppend(t *testing.T) {
	appender, index := newWireTestAppender(t)
	dir := t.TempDir()
	client, srv := net.Pipe()
	defer client.Close()
	go wireserver.HandleConnection(srv, appender, dir, orderRegistry(), index, "")
	if err := wire.WriteRequest(client, &wire.Request{
		Op:        wire.OpAppend,
		Kind:      "order",
		ID:        "o-1",
		EventType: "OrderCreated",
		Payload:   []byte(`{"total":10,"currency":"USD"}`),
	}); err != nil {
		t.Fatalf("WriteRequest failed: %v", err)
	}
	resp, err := wire.ReadResponse(client)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if resp.Status != wire.StatusOK {
		t.Errorf("expected StatusOK for single-event OpAppend (used by recli append), got %v: %s", resp.Status, resp.Message)
	}
}

func TestHandleConnection_AcceptsCorrectToken(t *testing.T) {
	appender, index := newWireTestAppender(t)
	dir := t.TempDir()
	client, srv := net.Pipe()
	defer client.Close()
	go wireserver.HandleConnection(srv, appender, dir, orderRegistry(), index, "correct-token")
	if err := wire.WriteAuthToken(client, "correct-token"); err != nil {
		t.Fatalf("WriteAuthToken failed: %v", err)
	}
	if err := wire.WriteRequest(client, &wire.Request{
		Op:   wire.OpAppendBatch,
		Kind: "order",
		ID:   "o-1",
		Batch: []wire.BatchEvent{
			{Kind: "order", ID: "o-1", EventType: "OrderCreated", Payload: []byte(`{"total":10,"currency":"USD"}`)},
		},
	}); err != nil {
		t.Fatalf("WriteRequest failed: %v", err)
	}
	resp, err := wire.ReadResponse(client)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if resp.Status != wire.StatusOK {
		t.Errorf("expected StatusOK after successful auth, got %v: %s", resp.Status, resp.Message)
	}
}

func TestHandleConnection_NoTokenConfiguredSkipsHandshake(t *testing.T) {
	appender, index := newWireTestAppender(t)
	dir := t.TempDir()
	client, srv := net.Pipe()
	defer client.Close()
	go wireserver.HandleConnection(srv, appender, dir, orderRegistry(), index, "")
	if err := wire.WriteRequest(client, &wire.Request{
		Op:   wire.OpAppendBatch,
		Kind: "order",
		ID:   "o-1",
		Batch: []wire.BatchEvent{
			{Kind: "order", ID: "o-1", EventType: "OrderCreated", Payload: []byte(`{"total":10,"currency":"USD"}`)},
		},
	}); err != nil {
		t.Fatalf("WriteRequest failed: %v", err)
	}
	resp, err := wire.ReadResponse(client)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if resp.Status != wire.StatusOK {
		t.Errorf("expected StatusOK when no auth token is configured, got %v: %s", resp.Status, resp.Message)
	}
}
