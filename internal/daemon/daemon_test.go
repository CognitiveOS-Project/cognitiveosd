package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CognitiveOS-Project/cognitiveosd/internal/config"
)

func TestNewEnvelope(t *testing.T) {
	env := NewEnvelope("test_type", "test-component", map[string]string{"foo": "bar"})
	if env.Type != "test_type" {
		t.Fatalf("expected test_type, got %s", env.Type)
	}
	if env.From != "test-component" {
		t.Fatalf("expected test-component, got %s", env.From)
	}
	if env.ID != "" {
		t.Fatalf("expected empty id, got %s", env.ID)
	}
	if env.Timestamp == "" {
		t.Fatal("expected timestamp")
	}
	var payload map[string]string
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["foo"] != "bar" {
		t.Fatalf("expected bar, got %s", payload["foo"])
	}
}

func TestErrorPayload(t *testing.T) {
	ep := ErrorPayload("E_TEST", "test error")
	if ep.Status != "error" {
		t.Fatalf("expected error status, got %s", ep.Status)
	}
	if ep.Error.Code != "E_TEST" {
		t.Fatalf("expected E_TEST, got %s", ep.Error.Code)
	}
	if ep.Error.Message != "test error" {
		t.Fatalf("expected test error, got %s", ep.Error.Message)
	}
}

func TestOKPayload(t *testing.T) {
	op := OKPayload(map[string]string{"result": "ok"})
	if op.Status != "ok" {
		t.Fatalf("expected ok status, got %s", op.Status)
	}
	data := op.Data.(map[string]string)
	if data["result"] != "ok" {
		t.Fatalf("expected ok, got %s", data["result"])
	}
}

func TestInputPayloadParse(t *testing.T) {
	raw := `{
		"type": "input_forward",
		"from": "cli",
		"payload": {
			"mode": "text",
			"content": "Hello CognitiveOS",
			"context": {"session_id": "sess_123", "device": "smartphone"}
		}
	}`

	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var payload InputPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.Mode != "text" {
		t.Fatalf("expected text, got %s", payload.Mode)
	}
	if payload.Content != "Hello CognitiveOS" {
		t.Fatalf("expected Hello CognitiveOS, got %s", payload.Content)
	}
	if payload.Context.SessionID != "sess_123" {
		t.Fatalf("expected sess_123, got %s", payload.Context.SessionID)
	}
}

func TestSystemCodePayloadParse(t *testing.T) {
	raw := `{
		"type": "system_code",
		"from": "cli",
		"payload": {
			"code": "wake",
			"origin": "voice"
		}
	}`

	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var payload SystemCodePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.Code != "wake" {
		t.Fatalf("expected wake, got %s", payload.Code)
	}
	if payload.Origin != "voice" {
		t.Fatalf("expected voice, got %s", payload.Origin)
	}
}

func TestMCPRegisterPayloadParse(t *testing.T) {
	raw := `{
		"type": "mcp_register",
		"from": "display-mcp",
		"payload": {
			"server": {"name": "display-mcp", "version": "1.0.0", "transport": "stdio"},
			"tools": [
				{
					"name": "cognitiveos.display.render_image",
					"description": "Render an image",
					"inputSchema": {"type": "object"}
				}
			]
		}
	}`

	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var payload MCPRegisterPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.Server.Name != "display-mcp" {
		t.Fatalf("expected display-mcp, got %s", payload.Server.Name)
	}
	if len(payload.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(payload.Tools))
	}
	if payload.Tools[0].Name != "cognitiveos.display.render_image" {
		t.Fatalf("expected cognitiveos.display.render_image, got %s", payload.Tools[0].Name)
	}
}

func TestResponseType(t *testing.T) {
	tests := map[string]string{
		"input_forward":  "input_accepted",
		"system_code":    "code_accepted",
		"mcp_register":   "mcp_registered",
		"mcp_unregister": "mcp_unregistered",
		"audit_request":  "audit_report",
		"status_request": "status_response",
		"unknown":        "unknown_response",
	}

	for input, expected := range tests {
		got := responseType(input)
		if got != expected {
			t.Errorf("responseType(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestStateMachine(t *testing.T) {
	cfg := config.Config{
		SocketPath:    filepath.Join(t.TempDir(), "daemon.sock"),
		RunDir:        t.TempDir(),
		LogDir:        t.TempDir(),
		AuditDir:      t.TempDir(),
		MCPBinDir:     "/nonexistent",
		InferenceURL:  "http://127.0.0.1:11434",
		AuditInterval: 9999,
	}

	d := New(cfg)

	if d.CurrentState() != StateIdle {
		t.Fatalf("initial state should be idle, got %s", d.CurrentState())
	}

	d.SetState(StateListening)
	if d.CurrentState() != StateListening {
		t.Fatalf("expected listening, got %s", d.CurrentState())
	}

	d.SetState(StateProcessing)
	if d.CurrentState() != StateProcessing {
		t.Fatalf("expected processing, got %s", d.CurrentState())
	}

	d.SetState(StateSecurity)
	if d.CurrentState() != StateSecurity {
		t.Fatalf("expected security, got %s", d.CurrentState())
	}
}

func TestMCPManagerRegisterUnregister(t *testing.T) {
	mgr := NewMCPManager(nil)

	mgr.Register("display-mcp", []MCPTool{
		{Name: "cognitiveos.display.render_image", Description: "Render"},
	}, nil)

	if mgr.ActiveCount() != 1 {
		t.Fatalf("expected 1 server, got %d", mgr.ActiveCount())
	}

	tool := mgr.FindTool("cognitiveos.display.render_image")
	if tool == nil {
		t.Fatal("expected to find tool")
	}
	if tool.Name != "cognitiveos.display.render_image" {
		t.Fatalf("expected cognitiveos.display.render_image, got %s", tool.Name)
	}

	mgr.Unregister("display-mcp")
	if mgr.ActiveCount() != 0 {
		t.Fatalf("expected 0 servers, got %d", mgr.ActiveCount())
	}

	if mgr.FindTool("cognitiveos.display.render_image") != nil {
		t.Fatal("expected tool to be removed after unregister")
	}
}

func TestMCPManagerInvokeNoServer(t *testing.T) {
	mgr := NewMCPManager(nil)
	_, err := mgr.Invoke("nonexistent.tool", nil, "")
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
	if !strings.Contains(err.Error(), "E_TOOL_NOT_FOUND") {
		t.Fatalf("expected E_TOOL_NOT_FOUND, got %v", err)
	}
}

func TestAuditDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), make([]byte, 2*1024*1024), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), make([]byte, 3*1024*1024), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg := config.Config{
		PatchDir: dir,
		ModelDir: t.TempDir(),
		AuditDir: t.TempDir(),
	}

	d := New(cfg)
	a := NewAuditor(d)

	size := a.dirSizeMB(dir)
	if size != 5 {
		t.Fatalf("expected 5 MB (5 MB of files), got %d", size)
	}
}

func TestUptime(t *testing.T) {
	cfg := config.Config{
		RunDir:   t.TempDir(),
		LogDir:   t.TempDir(),
		AuditDir: t.TempDir(),
	}
	d := New(cfg)

	uptime := d.Uptime()
	if uptime < 0 {
		t.Fatal("uptime should be >= 0")
	}

	time.Sleep(10 * time.Millisecond)
	if d.Uptime() <= uptime {
		t.Fatal("uptime should increase")
	}
}

func TestAuditorCollect(t *testing.T) {
	cfg := config.Config{
		PatchDir: t.TempDir(),
		ModelDir: t.TempDir(),
		AuditDir: t.TempDir(),
	}
	d := New(cfg)
	a := NewAuditor(d)

	report := a.Collect()
	if report.RAM.TotalMB <= 0 {
		t.Fatalf("expected RAM total > 0, got %d", report.RAM.TotalMB)
	}
	if report.Storage.TotalMB <= 0 {
		t.Fatalf("expected storage total > 0, got %d", report.Storage.TotalMB)
	}
}
