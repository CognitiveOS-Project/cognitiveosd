package rawmodel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

type HealthcheckResult struct {
	Status       string `json:"status"`
	ModelLoaded  bool   `json:"model_loaded"`
}

type ValidateSystemCodeResult struct {
	Status string `json:"status"`
	Action string `json:"action"`
}

type CheckUnlockCodeResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type AuditResourcesResult struct {
	Available bool  `json:"available"`
	TotalMB   int64 `json:"total_mb"`
	FreeMB    int64 `json:"free_mb"`
	Allowed   bool  `json:"allowed"`
}

type VersionResult struct {
	Version string `json:"version"`
	Model   string `json:"model"`
	Quant   string `json:"quant"`
}

type ValidatePromptResult struct {
	Action         string `json:"action"`
	ModifiedPrompt string `json:"modified_prompt,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Model          string `json:"model,omitempty"`
}

type ValidatePackageRequestResult struct {
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Command string `json:"command"`
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

type HandlerFunc func(params json.RawMessage) (interface{}, error)

type Mock struct {
	SocketPath string
	mu         sync.RWMutex
	listener *net.UnixListener
	done       chan struct{}
	wg         sync.WaitGroup
	handlers   map[string]HandlerFunc
}

func New(socketPath string) *Mock {
	m := &Mock{
		SocketPath: socketPath,
		done:       make(chan struct{}),
		handlers:   make(map[string]HandlerFunc),
	}
	m.setDefaults()
	return m
}

func (m *Mock) setDefaults() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.handlers["healthcheck"] = func(params json.RawMessage) (interface{}, error) {
		return HealthcheckResult{Status: "ready", ModelLoaded: false}, nil
	}
	m.handlers["validate_system_code"] = func(params json.RawMessage) (interface{}, error) {
		return ValidateSystemCodeResult{Status: "valid", Action: ""}, nil
	}
	m.handlers["check_unlock_code"] = func(params json.RawMessage) (interface{}, error) {
		return CheckUnlockCodeResult{Status: "accepted", Message: "unlock accepted"}, nil
	}
	m.handlers["audit_resources"] = func(params json.RawMessage) (interface{}, error) {
		return AuditResourcesResult{Available: true, TotalMB: 32000, FreeMB: 16000, Allowed: true}, nil
	}
	m.handlers["version"] = func(params json.RawMessage) (interface{}, error) {
		return VersionResult{Version: "1.0.0", Model: "mock", Quant: "Q4_0"}, nil
	}
	m.handlers["validate_prompt"] = func(params json.RawMessage) (interface{}, error) {
		return ValidatePromptResult{Action: "allow"}, nil
	}
	m.handlers["validate_package_request"] = func(params json.RawMessage) (interface{}, error) {
		return ValidatePackageRequestResult{Status: "approved", Reason: "", Command: ""}, nil
	}
}

func (m *Mock) Handle(method string, fn HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[method] = fn
}

func (m *Mock) Start() error {
	os.Remove(m.SocketPath)

	addr, err := net.ResolveUnixAddr("unix", m.SocketPath)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}

	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		return fmt.Errorf("listen unix: %w", err)
	}
	m.listener = l

	m.wg.Add(1)
	go m.acceptLoop()
	return nil
}

func (m *Mock) Stop() {
	close(m.done)
	if m.listener != nil {
		m.listener.Close()
	}
	m.wg.Wait()
	os.Remove(m.SocketPath)
}

func (m *Mock) acceptLoop() {
	defer m.wg.Done()
	for {
		conn, err := m.listener.AcceptUnix()
		if err != nil {
			select {
			case <-m.done:
				return
			default:
				continue
			}
		}
		m.wg.Add(1)
		go m.handleConn(conn)
	}
}

func (m *Mock) handleConn(conn *net.UnixConn) {
	defer m.wg.Done()
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				Error: map[string]interface{}{
					"code":    -32700,
					"message": "Parse error",
				},
			}
			json.NewEncoder(conn).Encode(resp)
			continue
		}

		resp := m.dispatch(req)
		if err := json.NewEncoder(conn).Encode(resp); err != nil {
			return
		}

		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
}

func (m *Mock) dispatch(req jsonRPCRequest) jsonRPCResponse {
	m.mu.RLock()
	fn, ok := m.handlers[req.Method]
	m.mu.RUnlock()

	if !ok {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}

	result, err := fn(req.Params)
	if err != nil {
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: map[string]interface{}{
				"code":    -32000,
				"message": err.Error(),
			},
		}
	}

	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}
