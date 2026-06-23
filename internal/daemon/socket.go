package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
)

type SocketListener struct {
	daemon   *Daemon
	listener *net.UnixListener
	done     chan struct{}
}

func NewSocketListener(d *Daemon) (*SocketListener, error) {
	os.Remove(d.Config.SocketPath)

	addr, err := net.ResolveUnixAddr("unix", d.Config.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("resolve addr: %w", err)
	}

	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("listen unix: %w", err)
	}

	if err := os.Chmod(d.Config.SocketPath, 0700); err != nil {
		l.Close()
		return nil, fmt.Errorf("chmod: %w", err)
	}

	sl := &SocketListener{
		daemon:   d,
		listener: l,
		done:     make(chan struct{}),
	}

	go sl.acceptLoop()
	return sl, nil
}

func (sl *SocketListener) acceptLoop() {
	for {
		conn, err := sl.listener.AcceptUnix()
		if err != nil {
			select {
			case <-sl.done:
				return
			default:
				sl.daemon.Log.Printf("accept error: %v", err)
				continue
			}
		}

		cc := NewClientConn(sl.daemon, conn)
		go cc.ReadLoop()
	}
}

func (sl *SocketListener) Close() {
	close(sl.done)
	sl.listener.Close()
}

type ClientConn struct {
	daemon  *Daemon
	conn    *net.UnixConn
	reader  *bufio.Scanner
	mu      sync.Mutex
	encoder *json.Encoder
	id      string
	done    chan struct{}
}

func NewClientConn(d *Daemon, conn *net.UnixConn) *ClientConn {
	return &ClientConn{
		daemon:  d,
		conn:    conn,
		reader:  bufio.NewScanner(conn),
		encoder: json.NewEncoder(conn),
		done:    make(chan struct{}),
	}
}

func (cc *ClientConn) ReadLoop() {
	defer cc.Close()

	for {
		select {
		case <-cc.done:
			return
		default:
		}

		if !cc.reader.Scan() {
			return
		}

		var env Envelope
		if err := json.Unmarshal(cc.reader.Bytes(), &env); err != nil {
			cc.daemon.Log.Printf("parse error from %s: %v", cc.id, err)
			resp := NewEnvelope("error", "cognitiveosd", ErrorPayload("E_INVALID_PAYLOAD", err.Error()))
			cc.Send(resp)
			continue
		}

		if cc.id == "" && env.From != "" {
			cc.id = env.From
			cc.daemon.AddClient(cc.id, cc)
		}

		cc.daemon.HandleMessage(env, cc)
	}
}

func (cc *ClientConn) Send(env Envelope) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	select {
	case <-cc.done:
		return fmt.Errorf("connection closed")
	default:
	}
	return cc.encoder.Encode(env)
}

func (cc *ClientConn) Close() {
	select {
	case <-cc.done:
		return
	default:
		close(cc.done)
	}

	if cc.id != "" {
		cc.daemon.RemoveClient(cc.id)
	}
	cc.conn.Close()
}
