package daemon

import (
	"encoding/json"
	"testing"
)

func TestHandleSystemCodeWake(t *testing.T) {
	d := newTestDaemon()
	d.rmClient = &RawModelClient{daemon: d}

	env := Envelope{
		Type:   "system_code",
		ID:     "test-1",
		From:   "keyboard",
		Payload: json.RawMessage(`{"code": "wake", "origin": "keyboard"}`),
	}
	conn := &mockConn{}

	d.handleSystemCode(env, conn)

	if d.State != StateListening {
		t.Fatalf("expected listening state, got %s", d.State)
	}
}

func TestHandleSystemCodeSecurity(t *testing.T) {
	d := newTestDaemon()
	d.rmClient = &RawModelClient{daemon: d}

	env := Envelope{
		Type:   "system_code",
		ID:     "test-2",
		From:   "hardware",
		Payload: json.RawMessage(`{"code": "security", "origin": "hardware"}`),
	}
	conn := &mockConn{}

	d.handleSystemCode(env, conn)

	if d.State != StateSecurity && d.State != StateShutdown {
		t.Fatalf("expected security or shutdown state, got %s", d.State)
	}
}

func TestHandleSystemCodeIdle(t *testing.T) {
	d := newTestDaemon()
	d.rmClient = &RawModelClient{daemon: d}

	env := Envelope{
		Type:   "system_code",
		ID:     "test-3",
		From:   "keyboard",
		Payload: json.RawMessage(`{"code": "idle", "origin": "keyboard"}`),
	}
	conn := &mockConn{}

	d.handleSystemCode(env, conn)

	if d.State != StateIdle {
		t.Fatalf("expected idle state, got %s", d.State)
	}
}

func TestHandleSystemCodeInvalidCode(t *testing.T) {
	d := newTestDaemon()
	d.rmClient = &RawModelClient{daemon: d}

	env := Envelope{
		Type:   "system_code",
		ID:     "test-4",
		From:   "cli",
		Payload: json.RawMessage(`{"code": "invalid_code", "origin": "cli"}`),
	}
	conn := &mockConn{}

	d.handleSystemCode(env, conn)

	if d.State != StateIdle {
		t.Fatalf("expected idle state (unchanged), got %s", d.State)
	}
}

func TestHandleInputForward(t *testing.T) {
	d := newTestDaemon()
	d.rmClient = &RawModelClient{daemon: d}
	d.wmClient = &WideModelClient{daemon: d}

	env := Envelope{
		Type:   "input_forward",
		ID:     "test-5",
		From:   "cli",
		Payload: json.RawMessage(`{"content": "hello world", "mode": "text"}`),
	}
	conn := &mockConn{}

	d.handleInputForward(env, conn)

	if d.State != StateProcessing {
		t.Fatalf("expected processing state, got %s", d.State)
	}
}

func TestHandleInputForwardEmptyContent(t *testing.T) {
	d := newTestDaemon()
	d.rmClient = &RawModelClient{daemon: d}
	d.wmClient = &WideModelClient{daemon: d}

	env := Envelope{
		Type:   "input_forward",
		ID:     "test-6",
		From:   "cli",
		Payload: json.RawMessage(`{"content": "", "mode": "text"}`),
	}
	conn := &mockConn{}

	d.handleInputForward(env, conn)

	if d.State != StateIdle {
		t.Fatalf("expected idle state (empty content), got %s", d.State)
	}
}

func TestHandleMCPRegister(t *testing.T) {
	d := newTestDaemon()

	env := Envelope{
		Type:   "mcp_register",
		ID:     "test-7",
		From:   "mcp-server-1",
		Payload: json.RawMessage(`{"name": "test-mcp", "tools": [{"name": "tool1"}]}`),
	}
	conn := &mockConn{}

	d.handleMCPRegister(env, conn)
}

func TestHandleMCPRegisterNoName(t *testing.T) {
	d := newTestDaemon()

	env := Envelope{
		Type:   "mcp_register",
		ID:     "test-8",
		From:   "mcp-server-1",
		Payload: json.RawMessage(`{"tools": []}`),
	}
	conn := &mockConn{}

	d.handleMCPRegister(env, conn)
}

func TestHandleAuditRequest(t *testing.T) {
	d := newTestDaemon()
	d.auditor = &Auditor{daemon: d}

	env := Envelope{
		Type:   "audit_request",
		ID:     "test-9",
		From:   "cli",
		Payload: json.RawMessage(`{}`),
	}
	conn := &mockConn{}

	d.handleAuditRequest(env, conn)
}

func TestHandleStatusRequest(t *testing.T) {
	d := newTestDaemon()
	d.mcpMgr = &MCPManager{daemon: d}
	d.wmClient = &WideModelClient{daemon: d}
	d.rmClient = &RawModelClient{daemon: d}

	env := Envelope{
		Type:   "status_request",
		ID:     "test-10",
		From:   "cli",
		Payload: json.RawMessage(`{}`),
	}
	conn := &mockConn{}

	d.handleStatusRequest(env, conn)
}
