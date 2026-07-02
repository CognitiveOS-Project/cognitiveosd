package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CognitiveOS-Project/cognitiveosd/internal/config"
)

const idleTimeoutDuration = 5 * time.Minute

type State string

const (
	StateIdle           State = "idle"
	StateIdleRequested  State = "idle_requested"
	StateListening      State = "listening"
	StateProcessing     State = "processing"
	StateSecurity       State = "security"
	StateShutdown       State = "shutdown"
)

type Daemon struct {
	Config   config.Config
	State    State
	mu       sync.RWMutex
	startTime time.Time

	listener *SocketListener
	mcpMgr   *MCPManager
	auditor  *Auditor
	wmClient *WideModelClient
	rmClient *RawModelClient

	clients   map[string]*ClientConn
	clientsMu sync.RWMutex

	signalCh    chan os.Signal
	done        chan struct{}
	idleTimer   *time.Timer
	lastRequest time.Time

	Log *log.Logger
}

func New(cfg config.Config) *Daemon {
	logger := log.New(os.Stdout, "cognitiveosd: ", log.LstdFlags)
	if f, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		logger = log.New(f, "cognitiveosd: ", log.LstdFlags)
	}

	return &Daemon{
		Config:    cfg,
		State:     StateIdle,
		startTime: time.Now(),
		clients:   make(map[string]*ClientConn),
		signalCh:  make(chan os.Signal, 1),
		done:      make(chan struct{}),
		Log:       logger,
	}
}

func (d *Daemon) Run() error {
	d.Log.Println("starting cognitiveosd")

	if err := d.Config.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	writePid(d.Config.PidFilePath)
	defer os.Remove(d.Config.PidFilePath)

	d.mcpMgr = NewMCPManager(d)
	d.auditor = NewAuditor(d)
	d.wmClient = NewWideModelClient(d)
	d.rmClient = NewRawModelClient(d)

	listener, err := NewSocketListener(d)
	if err != nil {
		return fmt.Errorf("socket: %w", err)
	}
	d.listener = listener

	d.Log.Printf("listening on %s", d.Config.SocketPath)

	initialAudit := d.auditor.Collect()
	d.Log.Printf("initial audit: %d MB RAM available", initialAudit.RAM.AvailableMB)

	if err := d.rmClient.Connect(); err != nil {
		d.Log.Printf("raw model not available: %v", err)
	} else {
		d.Log.Println("raw model connected")
	}

	d.auditor.Start()

	d.mcpMgr.SpawnCoreBridges()
	d.mcpMgr.StartHealthchecks()

	d.scanPatches()

	if err := d.loadWideModel(); err != nil {
		d.Log.Printf("WARN: auto-load Wide Model: %v", err)
	}

	d.startIdleTimer()

	d.broadcast(NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
		SessionID:   "system",
		Content:     "CognitiveOS ready",
		ContentType: "text",
	}))
	d.Log.Println("cognitiveosd ready")

	signal.Notify(d.signalCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	for {
		select {
		case sig := <-d.signalCh:
			d.Log.Printf("received signal %v, shutting down", sig)
			d.shutdown("signal")
			return nil

		case <-d.done:
			return nil
		}
	}
}

func (d *Daemon) Shutdown() {
	close(d.done)
}

func (d *Daemon) scanPatches() {
	entries, err := os.ReadDir(d.Config.PatchDir)
	if err != nil {
		d.Log.Printf("scan patches: %v", err)
		return
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			manifestPath := filepath.Join(d.Config.PatchDir, e.Name(), "cognitive.json")
			if _, err := os.Stat(manifestPath); err == nil {
				count++
			}
		}
	}
	d.Log.Printf("patches scanned: %d installed", count)
}

func (d *Daemon) patchCount() int {
	entries, err := os.ReadDir(d.Config.PatchDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			manifestPath := filepath.Join(d.Config.PatchDir, e.Name(), "cognitive.json")
			if _, err := os.Stat(manifestPath); err == nil {
				count++
			}
		}
	}
	return count
}

func (d *Daemon) loadWideModel() error {
	modelDir := filepath.Join(d.Config.ModelDir, "wide", "active")
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return fmt.Errorf("read wide model dir %s: %w", modelDir, err)
	}

	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".gguf") || strings.HasSuffix(e.Name(), ".safetensors")) {
			modelPath := filepath.Join(modelDir, e.Name())
			if d.rmClient.IsReady() {
				_, _, _, allowed, err := d.rmClient.AuditResources(0)
				if err != nil {
					d.Log.Printf("audit before load: %v", err)
				} else if !allowed {
					d.Log.Printf("WARN: insufficient resources for Wide Model load")
					return fmt.Errorf("insufficient resources")
				}
			}
			return d.wmClient.Load(modelPath)
		}
	}
	return fmt.Errorf("no model file found in %s", modelDir)
}

func (d *Daemon) startIdleTimer() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastRequest = time.Now()
	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}
	d.idleTimer = time.AfterFunc(idleTimeoutDuration, func() {
		d.mu.Lock()
		if time.Since(d.lastRequest) >= idleTimeoutDuration {
			d.mu.Unlock()
			d.Log.Println("idle timeout: unloading Wide Model")
			_ = d.wmClient.Unload("idle_timeout")
			d.SetState(StateIdle)
		} else {
			d.mu.Unlock()
		}
	})
}

func (d *Daemon) touchIdleTimer() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastRequest = time.Now()
}

func (d *Daemon) shutdown(reason string) {
	d.mu.Lock()
	d.State = StateShutdown
	d.mu.Unlock()

	d.Log.Printf("shutdown: %s", reason)

	d.broadcast(NewEnvelope("shutdown_notice", "cognitiveosd", ShutdownNoticePayload{Reason: reason}))

	_ = d.wmClient.Unload(reason)
	d.mcpMgr.ShutdownAll()

	time.Sleep(500 * time.Millisecond)

	d.rmClient.Close()
	d.listener.Close()
	d.auditor.Stop()

	d.clientsMu.Lock()
	for id, c := range d.clients {
		c.Close()
		delete(d.clients, id)
	}
	d.clientsMu.Unlock()

	switch reason {
	case "security_code":
		d.Log.Println("SECURITY: powering off peripherals")
		_ = exec.Command("gpioset", "0", "0=0").Run()
		_ = exec.Command("gpioset", "0", "1=0").Run()
	case "idle_code":
		d.Log.Println("IDLE: entering low-power suspend")
		_ = exec.Command("sysctl", "-w", "kernel.printk=0").Run()
	case "reset_code":
		d.Log.Println("RESET: wiping data partitions")
		_ = exec.Command("rm", "-rf", "/cognitiveos/data/*").Run()
		_ = exec.Command("rm", "-rf", "/cognitiveos/models/wide/*").Run()
		_ = exec.Command("rm", "-rf", "/cognitiveos/patches/*").Run()
	}

	d.Log.Println("shutdown complete")
}

func (d *Daemon) AddClient(id string, conn *ClientConn) {
	d.clientsMu.Lock()
	defer d.clientsMu.Unlock()
	d.clients[id] = conn
	d.Log.Printf("client connected: %s (%d active)", id, len(d.clients))
}

func (d *Daemon) RemoveClient(id string) {
	d.clientsMu.Lock()
	defer d.clientsMu.Unlock()
	delete(d.clients, id)
	d.Log.Printf("client disconnected: %s (%d active)", id, len(d.clients))
}

func (d *Daemon) SendToClient(clientID string, env Envelope) error {
	d.clientsMu.RLock()
	c, ok := d.clients[clientID]
	d.clientsMu.RUnlock()
	if !ok {
		return fmt.Errorf("E_SERVER_NOT_FOUND: client %s not connected", clientID)
	}
	return c.Send(env)
}

func (d *Daemon) Broadcast(env Envelope) {
	d.broadcast(env)
}

func (d *Daemon) broadcast(env Envelope) {
	d.clientsMu.RLock()
	defer d.clientsMu.RUnlock()
	for _, c := range d.clients {
		_ = c.Send(env)
	}
}

func (d *Daemon) SendResponse(env Envelope, status PayloadStatus) {
	respPayload, _ := json.Marshal(status)
	env.Payload = respPayload
}

func (d *Daemon) HandleMessage(env Envelope, conn *ClientConn) {
	if len(env.Payload) > 1048576 {
		d.SendError(env, conn, "E_TOO_LARGE", "message exceeds 1 MB limit")
		return
	}

	switch env.Type {
	case "input_forward":
		d.handleInputForward(env, conn)
	case "system_code":
		d.handleSystemCode(env, conn)
	case "mcp_register":
		d.handleMCPRegister(env, conn)
	case "mcp_unregister":
		d.handleMCPUnregister(env, conn)
	case "mcp_result":
		d.handleMCPResult(env, conn)
	case "audit_request":
		d.handleAuditRequest(env, conn)
	case "status_request":
		d.handleStatusRequest(env, conn)
	case "wide_model_load":
		d.handleWideModelLoad(env, conn)
	case "wide_model_unload":
		d.handleWideModelUnload(env, conn)
	default:
		d.SendError(env, conn, "E_UNKNOWN_TYPE", fmt.Sprintf("unknown message type: %s", env.Type))
	}
}

func (d *Daemon) SendError(env Envelope, conn *ClientConn, code, message string) {
	resp := Envelope{
		Type:      env.Type + "_response",
		ID:        env.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		From:      "cognitiveosd",
	}
	status := ErrorPayload(code, message)
	d.SendResponse(resp, status)
	if err := conn.Send(resp); err != nil {
		d.Log.Printf("send error response: %v", err)
	}
}

func (d *Daemon) SendOK(env Envelope, conn *ClientConn, data interface{}) {
	resp := Envelope{
		Type:      responseType(env.Type),
		ID:        env.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		From:      "cognitiveosd",
	}
	status := OKPayload(data)
	d.SendResponse(resp, status)
	if err := conn.Send(resp); err != nil {
		d.Log.Printf("send ok response: %v", err)
	}
}

func (d *Daemon) CurrentState() State {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.State
}

func (d *Daemon) SetState(s State) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.State = s
	d.Log.Printf("state: %s", s)
}

func (d *Daemon) Uptime() time.Duration {
	return time.Since(d.startTime)
}

func responseType(msgType string) string {
	switch msgType {
	case "input_forward":
		return "input_accepted"
	case "system_code":
		return "code_accepted"
	case "mcp_register":
		return "mcp_registered"
	case "mcp_unregister":
		return "mcp_unregistered"
	case "audit_request":
		return "audit_report"
	case "status_request":
		return "status_response"
	case "wide_model_load":
		return "wide_model_loaded"
	case "wide_model_unload":
		return "wide_model_unloaded"
	default:
		return msgType + "_response"
	}
}

func writePid(path string) {
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write pid: %v\n", err)
	}
}
