package daemon

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/CognitiveOS-Project/cognitiveosd/internal/config"
)

func TestNewSocketListener(t *testing.T) {
	d := &Daemon{
		Config: config.Config{
			SocketPath: "/tmp/test_cognitiveosd.sock",
		},
		Log: log.New(os.Stderr, "", 0),
	}

	os.Remove(d.Config.SocketPath)

	sl, err := NewSocketListener(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sl == nil {
		t.Fatal("expected non-nil SocketListener")
	}
	if sl.listener == nil {
		t.Fatal("expected non-nil listener")
	}

	sl.Close()

	if _, err := os.Stat(d.Config.SocketPath); !os.IsNotExist(err) {
		t.Fatal("expected socket file to be removed after Close")
	}
}

func TestNewSocketListenerInvalidPath(t *testing.T) {
	d := &Daemon{
		Config: config.Config{
			SocketPath: "/nonexistent_dir/socket.sock",
		},
		Log: log.New(os.Stderr, "", 0),
	}

	_, err := NewSocketListener(d)
	if err == nil {
		t.Fatal("expected error for invalid socket path")
	}
}

func TestClientConnSendReceive(t *testing.T) {
	d := newTestDaemon()
	d.Config.SocketPath = "/tmp/test_cognitiveosd2.sock"

	os.Remove(d.Config.SocketPath)

	sl, err := NewSocketListener(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sl.Close()

	addr, err := net.ResolveUnixAddr("unix", d.Config.SocketPath)
	if err != nil {
		t.Fatalf("resolve addr: %v", err)
	}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	env := Envelope{
		Type: "status_request",
		ID:   "test-1",
		From: "test-client",
	}
	if err := enc.Encode(env); err != nil {
		t.Fatalf("encode: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
}
