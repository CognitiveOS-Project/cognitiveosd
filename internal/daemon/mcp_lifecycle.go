package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type MCPServer struct {
	Name    string
	Info    MCPInfo
	Tools   []MCPTool
	Conn    *ClientConn
	Process *exec.Cmd
	Stdin   *json.Encoder
	Stdout  *bufio.Scanner
	mu      sync.Mutex
	active  bool
}

type MCPManager struct {
	daemon  *Daemon
	servers map[string]*MCPServer
	mu      sync.RWMutex
}

func NewMCPManager(d *Daemon) *MCPManager {
	return &MCPManager{
		daemon:  d,
		servers: make(map[string]*MCPServer),
	}
}

func (m *MCPManager) SpawnCoreBridges() {
	for _, name := range m.daemon.Config.MCPBridges {
		binaryPath := filepath.Join(m.daemon.Config.MCPBinDir, name)
		if _, err := os.Stat(binaryPath); err != nil {
			binaryPath = name
		}
		go m.Spawn(name, binaryPath)
	}
}

func (m *MCPManager) Spawn(name string, binaryPath string) {
	cmd := exec.Command(binaryPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.daemon.Log.Printf("MCP %s: stdin pipe: %v", name, err)
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.daemon.Log.Printf("MCP %s: stdout pipe: %v", name, err)
		return
	}

	if err := cmd.Start(); err != nil {
		m.daemon.Log.Printf("MCP %s: start: %v", name, err)
		return
	}

	encoder := json.NewEncoder(stdin)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 65536), 1048576)

	server := &MCPServer{
		Name:    name,
		Process: cmd,
		Stdin:   encoder,
		Stdout:  scanner,
		active:  true,
	}
	m.RegisterProcess(name, server)

	m.daemon.Log.Printf("MCP %s: spawned (pid %d)", name, cmd.Process.Pid)

	server.DiscoverTools(encoder, scanner)

	go server.Wait(m)
}

func (m *MCPManager) RegisterProcess(name string, server *MCPServer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[name] = server
}

func (m *MCPManager) Register(name string, tools []MCPTool, conn *ClientConn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.servers[name]; ok {
		existing.Conn = conn
		existing.Tools = tools
		existing.Info = MCPInfo{
			Name:      name,
			Version:   "1.0.0",
			Transport: "stdio",
		}
		return
	}

	m.servers[name] = &MCPServer{
		Name:  name,
		Tools: tools,
		Conn:  conn,
		Info: MCPInfo{
			Name:      name,
			Version:   "1.0.0",
			Transport: "stdio",
		},
		active: true,
	}
}

func (m *MCPManager) Unregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.servers[name]; ok {
		s.active = false
		delete(m.servers, name)
	}
}

func (m *MCPManager) Invoke(toolName string, args map[string]interface{}, sessionID string) (MCPResultPayload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	serverName := m.resolveServer(toolName)
	if serverName == "" {
		return MCPResultPayload{}, fmt.Errorf("E_TOOL_NOT_FOUND: tool %s not found", toolName)
	}

	server, ok := m.servers[serverName]
	if !ok || !server.active {
		return MCPResultPayload{}, fmt.Errorf("E_SERVER_NOT_FOUND: server for %s not active", toolName)
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	rpcReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      fmt.Sprintf("invoke-%d", time.Now().UnixNano()),
		"method":  toolName,
		"params": map[string]interface{}{
			"arguments": args,
		},
	}

	if err := server.Stdin.Encode(rpcReq); err != nil {
		return MCPResultPayload{}, fmt.Errorf("send to MCP server: %w", err)
	}

	var rpcResp map[string]interface{}
	if !server.Stdout.Scan() {
		return MCPResultPayload{}, fmt.Errorf("E_SERVER_NOT_FOUND: MCP server %s disconnected", serverName)
	}
	if err := json.Unmarshal(server.Stdout.Bytes(), &rpcResp); err != nil {
		return MCPResultPayload{}, fmt.Errorf("parse MCP response: %w", err)
	}

	if errVal, ok := rpcResp["error"]; ok {
		errObj := errVal.(map[string]interface{})
		return MCPResultPayload{
			Status: "error",
			Error: &ErrorInfo{
				Code:    fmt.Sprintf("%v", errObj["code"]),
				Message: fmt.Sprintf("%v", errObj["message"]),
			},
		}, nil
	}

	result, _ := rpcResp["result"].(map[string]interface{})
	content, _ := result["content"].([]interface{})

	var items []ContentItem
	for _, c := range content {
		if cm, ok := c.(map[string]interface{}); ok {
			items = append(items, ContentItem{
				Type: fmt.Sprintf("%v", cm["type"]),
				Text: fmt.Sprintf("%v", cm["text"]),
			})
		}
	}

	return MCPResultPayload{
		Status:  "ok",
		Content: items,
	}, nil
}

func (m *MCPManager) resolveServer(toolName string) string {
	for name, server := range m.servers {
		for _, tool := range server.Tools {
			if tool.Name == toolName {
				return name
			}
		}
	}
	return ""
}

func (m *MCPManager) FindTool(toolName string) *MCPTool {
	for _, server := range m.servers {
		for _, tool := range server.Tools {
			if tool.Name == toolName {
				return &tool
			}
		}
	}
	return nil
}

func (m *MCPManager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.servers)
}

func (m *MCPManager) ShutdownAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, server := range m.servers {
		if server.Process != nil && server.Process.Process != nil {
			server.Process.Process.Signal(syscall.SIGTERM)
			go func(p *os.Process, n string) {
				done := make(chan error, 1)
				go func() {
					_, err := p.Wait()
					done <- err
				}()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					p.Kill()
				}
			}(server.Process.Process, name)
		}
		server.active = false
		delete(m.servers, name)
	}
}

func (s *MCPServer) DiscoverTools(encoder *json.Encoder, scanner *bufio.Scanner) {
	rpcReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "mcp.list_tools",
	}

	encoder.Encode(rpcReq)

	if scanner.Scan() {
		var resp map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &resp); err == nil {
			if result, ok := resp["result"].(map[string]interface{}); ok {
				if toolsRaw, ok := result["tools"].([]interface{}); ok {
					for _, t := range toolsRaw {
						if tm, ok := t.(map[string]interface{}); ok {
							tool := MCPTool{
								Name:        fmt.Sprintf("%v", tm["name"]),
								Description: fmt.Sprintf("%v", tm["description"]),
								InputSchema: tm["inputSchema"],
							}
							s.Tools = append(s.Tools, tool)
						}
					}
				}
			}
		}
	}
}

func (s *MCPServer) Wait(mgr *MCPManager) {
	err := s.Process.Wait()
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()

	mgr.Unregister(s.Name)

	if err != nil {
		mgr.daemon.Log.Printf("MCP %s: exited: %v", s.Name, err)
	} else {
		mgr.daemon.Log.Printf("MCP %s: exited cleanly", s.Name)
	}

	if !strings.HasPrefix(s.Name, "bridge-") && mgr.daemon.CurrentState() != StateShutdown {
		mgr.daemon.Log.Printf("MCP %s: restarting in 2s", s.Name)
		time.Sleep(2 * time.Second)
		mgr.Spawn(s.Name, s.Name)
	}
}
