package daemon

import (
	"log"
	"os"
	"sync"

	"github.com/CognitiveOS-Project/cognitiveosd/internal/config"
)

// mockConn implements a mock connection for testing
type mockConn struct {
	mu     sync.Mutex
	sent   []Envelope
	closed bool
}

func (c *mockConn) Send(env Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errConnClosed
	}
	c.sent = append(c.sent, env)
	return nil
}
func (c *mockConn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

var errConnClosed = &connClosedError{}

type connClosedError struct{}

func (e *connClosedError) Error() string { return "connection closed" }

// mockWideClient implements WideModelClient methods used by Daemon
type mockWideClient struct {
	loaded    bool
	modelName string
	modelID   string
	loadErr   error
	unloadErr error
	genErr    error
}

func (m *mockWideClient) Load(modelPath string) error {
	if m.loadErr != nil {
		return m.loadErr
	}
	m.loaded = true
	m.modelName = modelPath
	return nil
}
func (m *mockWideClient) Unload(reason string) error {
	if m.unloadErr != nil {
		return m.unloadErr
	}
	m.loaded = false
	return nil
}
func (m *mockWideClient) IsLoaded() bool { return m.loaded }
func (m *mockWideClient) LoadedModelName() string { return m.modelName }
func (m *mockWideClient) LoadedModelID() string { return m.modelID }
func (m *mockWideClient) Generate(prompt string) (string, error) {
	if m.genErr != nil {
		return "", m.genErr
	}
	return "mock response: " + prompt, nil
}
func (m *mockWideClient) LoadAdapter(adapterPath string) error { return nil }
func (m *mockWideClient) Health() bool { return m.loaded }
func (m *mockWideClient) LoadWithID(modelPath, modelID string) error {
	m.loaded = true
	m.modelName = modelPath
	m.modelID = modelID
	return nil
}
func (m *mockWideClient) SetSystemPrompt(prompt string) {}

// newTestDaemon creates a minimal Daemon for testing handlers
func newTestDaemon() *Daemon {
	d := &Daemon{
		Config:   config.Config{},
		State:    StateIdle,
		clients:  make(map[string]*ClientConn),
		Log:      log.New(os.Stderr, "", 0),
		wmClient: &mockWideClient{},
		rmClient: &RawModelClient{daemon: &Daemon{}},
	}
	d.listener = &SocketListener{done: make(chan struct{})}

	d.mcpMgr = &MCPManager{
		daemon:  d,
		servers: make(map[string]*MCPServer),
	}
		d.auditor = &Auditor{daemon: d, done: make(chan struct{})}
	d.listener = &SocketListener{done: make(chan struct{})}
	return d
}
