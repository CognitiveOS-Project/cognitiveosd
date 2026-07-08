//go:build integration

package daemon_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CognitiveOS-Project/cognitiveosd/internal/testutil/rawmodel"
)

type daemonTestEnv struct {
	t           *testing.T
	rawMock     *rawmodel.Mock
	daemonCmd   *exec.Cmd
	daemonSock  string
	done        chan struct{}
}

func (env *daemonTestEnv) cleanup() {
	if env.daemonCmd != nil {
		env.daemonCmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- env.daemonCmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			env.daemonCmd.Process.Kill()
			<-done
		}
	}
	if env.rawMock != nil {
		env.rawMock.Stop()
	}
}

func startDaemon(t *testing.T) *daemonTestEnv {
	t.Helper()

	runDir := t.TempDir()
	logDir := t.TempDir()
	modelDir := t.TempDir()
	patchDir := t.TempDir()
	auditDir := filepath.Join(t.TempDir(), "audit")
	os.MkdirAll(auditDir, 0755)

	rawSock := filepath.Join(runDir, "raw.sock")
	daemonSock := filepath.Join(runDir, "daemon.sock")

	rawMock := rawmodel.New(rawSock)
	if err := rawMock.Start(); err != nil {
		t.Fatalf("start raw model mock: %v", err)
	}
	if fi, err := os.Stat(rawSock); err != nil {
		t.Fatalf("raw socket %s not created after Start(): %v", rawSock, err)
	} else {
		t.Logf("raw model mock listening on %s (mode=%v)", rawSock, fi.Mode())
	}

	repoRoot := findRepoRoot(t)
	binPath := filepath.Join(t.TempDir(), "cognitiveosd")

	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/cognitiveosd")
	buildCmd.Dir = repoRoot
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build daemon: %v\n%s", err, buildOut)
	}

	daemonLogFile := filepath.Join(logDir, "cognitiveosd.log")

	cmd := exec.Command(binPath,
		"--run", runDir,
		"--log-dir", logDir,
		"--models", modelDir,
		"--patches", patchDir,
		"--mcp-bin", "/nonexistent",
		"--inference", "http://127.0.0.1:1",
		"--audit-interval", "9999",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		rawMock.Stop()
		t.Fatalf("start daemon: %v", err)
	}
	t.Logf("daemon started (pid %d), daemonLog=%s", cmd.Process.Pid, daemonLogFile)

	time.Sleep(500 * time.Millisecond)
	if logData, err := os.ReadFile(daemonLogFile); err == nil {
		t.Logf("daemon log after 500ms:\n%s", string(logData))
	} else {
		t.Logf("daemon log not yet created: %v", err)
	}

	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Logf("daemon died: %v", err)
		if logData, err := os.ReadFile(daemonLogFile); err == nil {
			t.Logf("daemon log at death:\n%s", string(logData))
		}
		out, _ := exec.Command("ps", "aux").CombinedOutput()
		t.Logf("ps output:\n%s", out)
		t.Fatalf("daemon process died shortly after start: %v", err)
	}

	env := &daemonTestEnv{
		t:          t,
		rawMock:    rawMock,
		daemonCmd:  cmd,
		daemonSock: daemonSock,
		done:       make(chan struct{}),
	}

	if !waitForFile(daemonSock, 5*time.Second) {
		env.cleanup()
		if logData, err := os.ReadFile(daemonLogFile); err == nil {
			t.Logf("daemon log at timeout:\n%s", string(logData))
		}
		t.Logf("raw socket %s exists: %v", rawSock, fileExists(rawSock))
		t.Fatal("daemon socket did not appear within 5s")
	}

	t.Cleanup(env.cleanup)
	return env
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found from " + dir)
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

type loggingWriter struct {
	t      *testing.T
	prefix string
}

func (w *loggingWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s%s", w.prefix, strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

type daemonConn struct {
	conn *net.UnixConn
	enc  *json.Encoder
	dec  *json.Decoder
}

func dialDaemon(t *testing.T, socketPath string) *daemonConn {
	t.Helper()
	addr, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		t.Fatalf("resolve addr: %v", err)
	}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return &daemonConn{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
	}
}

func (c *daemonConn) Send(v interface{}) error {
	return c.enc.Encode(v)
}

func (c *daemonConn) Receive(v interface{}) error {
	return c.dec.Decode(v)
}

func (c *daemonConn) Close() {
	c.conn.Close()
}

type envelope struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	From      string          `json:"from,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type payloadStatus struct {
	Status string      `json:"status"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

func drainPending(t *testing.T, conn *daemonConn) {
	conn.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var extra envelope
	err := conn.Receive(&extra)
	conn.conn.SetReadDeadline(time.Time{})
	if err == nil {
		t.Logf("drained: %s", extra.Type)
	}
}

func sendAndReceive(t *testing.T, conn *daemonConn, msg envelope) (envelope, payloadStatus) {
	t.Helper()
	if err := conn.Send(msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	var resp envelope
	if err := conn.Receive(&resp); err != nil {
		t.Fatalf("receive: %v", err)
	}
	var status payloadStatus
	if err := json.Unmarshal(resp.Payload, &status); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return resp, status
}

func TestDaemonIntegration_Startup(t *testing.T) {
	env := startDaemon(t)
	conn := dialDaemon(t, env.daemonSock)
	defer conn.Close()

	t.Run("status_request", func(t *testing.T) {
		msg := envelope{
			Type:    "status_request",
			From:    "test",
			Payload: json.RawMessage("{}"),
		}
		resp, status := sendAndReceive(t, conn, msg)

		if resp.Type != "status_response" {
			t.Fatalf("expected status_response, got %s", resp.Type)
		}
		if resp.From != "cognitiveosd" {
			t.Fatalf("expected from cognitiveosd, got %s", resp.From)
		}
		if status.Status != "ok" {
			t.Fatalf("expected ok, got %s", status.Status)
		}
	})

	t.Run("system_code_wake", func(t *testing.T) {
		payload := map[string]string{
			"code":   "wake",
			"origin": "test",
		}
		pb, _ := json.Marshal(payload)
		msg := envelope{
			Type:    "system_code",
			From:    "test",
			Payload: pb,
		}
		resp, status := sendAndReceive(t, conn, msg)
		if resp.Type != "code_accepted" {
			t.Fatalf("expected code_accepted, got %s", resp.Type)
		}
		if status.Status != "ok" {
			t.Fatalf("expected ok, got %s (error: %+v)", status.Status, status.Error)
		}
	})

	t.Run("status_after_wake", func(t *testing.T) {
		msg := envelope{
			Type:    "status_request",
			From:    "test",
			Payload: json.RawMessage("{}"),
		}
		_, status := sendAndReceive(t, conn, msg)
		if status.Status != "ok" {
			t.Fatalf("expected ok, got %s", status.Status)
		}

		var data map[string]interface{}
		b, _ := json.Marshal(status.Data)
		json.Unmarshal(b, &data)
		if state, ok := data["state"].(string); ok && state != "listening" {
			t.Fatalf("expected state listening after wake, got %s", state)
		}
	})

	t.Run("audit_request", func(t *testing.T) {
		msg := envelope{
			Type:    "audit_request",
			From:    "test",
			Payload: json.RawMessage("{}"),
		}
		resp, status := sendAndReceive(t, conn, msg)
		if resp.Type != "audit_report" {
			t.Fatalf("expected audit_report, got %s", resp.Type)
		}
		if status.Status != "ok" {
			t.Fatalf("expected ok, got %s", status.Status)
		}
	})

	t.Run("mcp_register", func(t *testing.T) {
		payload := map[string]interface{}{
			"server": map[string]string{
				"name":      "test-mcp",
				"version":   "1.0.0",
				"transport": "socket",
			},
			"tools": []map[string]interface{}{
				{
					"name":        "cognitiveos.test.echo",
					"description": "Echo test",
					"inputSchema": map[string]string{"type": "object"},
				},
			},
		}
		pb, _ := json.Marshal(payload)
		msg := envelope{
			Type:    "mcp_register",
			From:    "test-mcp",
			Payload: pb,
		}
		resp, status := sendAndReceive(t, conn, msg)
		if resp.Type != "mcp_registered" {
			t.Fatalf("expected mcp_registered, got %s", resp.Type)
		}
		if status.Status != "ok" {
			t.Fatalf("expected ok, got %s", status.Status)
		}
	})

	t.Run("unknown_type", func(t *testing.T) {
		msg := envelope{
			Type:    "nonexistent_type",
			From:    "test",
			Payload: json.RawMessage("{}"),
		}
		resp, status := sendAndReceive(t, conn, msg)
		if status.Status != "error" {
			t.Fatalf("expected error, got %s", status.Status)
		}
		if status.Error == nil || status.Error.Code != "E_UNKNOWN_TYPE" {
			t.Fatalf("expected E_UNKNOWN_TYPE, got %+v", status.Error)
		}
		if resp.From != "cognitiveosd" {
			t.Fatalf("expected from cognitiveosd, got %s", resp.From)
		}
	})

	t.Run("security_code_rejected", func(t *testing.T) {
		payload := map[string]string{
			"code":   "security",
			"origin": "cli",
		}
		pb, _ := json.Marshal(payload)
		msg := envelope{
			Type:    "system_code",
			From:    "test",
			Payload: pb,
		}
		_, status := sendAndReceive(t, conn, msg)
		if status.Status != "error" {
			t.Fatalf("expected error for security from software, got %s", status.Status)
		}
		if status.Error == nil || status.Error.Code != "E_UNAUTHORIZED" {
			t.Fatalf("expected E_UNAUTHORIZED, got %+v", status.Error)
		}
	})

	t.Run("input_forward", func(t *testing.T) {
		payload := map[string]interface{}{
			"mode":    "text",
			"content": "hello",
		}
		pb, _ := json.Marshal(payload)
		msg := envelope{
			Type:    "input_forward",
			From:    "test",
			ID:      "test-001",
			Payload: pb,
		}
		resp, status := sendAndReceive(t, conn, msg)
		if resp.Type != "input_accepted" {
			t.Fatalf("expected input_accepted, got %s", resp.Type)
		}
		if status.Status != "ok" {
			t.Fatalf("expected ok, got %s (error: %+v)", status.Status, status.Error)
		}
		// Drain any output_deliver from async processPrompt
		drainPending(t, conn)
	})

	t.Run("invalid_payload", func(t *testing.T) {
		msg := envelope{
			Type:    "system_code",
			From:    "test",
			Payload: json.RawMessage(`{"code":"unknown_code","origin":"test"}`),
		}
		_, status := sendAndReceive(t, conn, msg)
		if status.Status != "error" {
			t.Fatalf("expected error for unknown code, got %s", status.Status)
		}
		if status.Error == nil || status.Error.Code != "E_INVALID_PAYLOAD" {
			t.Fatalf("expected E_INVALID_PAYLOAD, got %+v", status.Error)
		}
	})

	t.Run("corrupt_payload", func(t *testing.T) {
		msg := envelope{
			Type:    "system_code",
			From:    "test",
			Payload: json.RawMessage(`"not an object"`),
		}
		resp, status := sendAndReceive(t, conn, msg)
		if status.Status != "error" {
			t.Fatalf("expected error for corrupt payload, got %s", status.Status)
		}
		if status.Error == nil || status.Error.Code != "E_INVALID_PAYLOAD" {
			t.Fatalf("expected E_INVALID_PAYLOAD, got %+v", status.Error)
		}
		if resp.From != "cognitiveosd" {
			t.Fatalf("expected from cognitiveosd, got %s", resp.From)
		}
	})
}

func TestDaemonIntegration_RawModelRejection(t *testing.T) {
	runDir := t.TempDir()
	logDir := t.TempDir()
	rawSock := filepath.Join(runDir, "raw.sock")
	daemonSock := filepath.Join(runDir, "daemon.sock")

	rawMock := rawmodel.New(rawSock)
	rawMock.Handle("validate_system_code", func(params json.RawMessage) (interface{}, error) {
		return rawmodel.ValidateSystemCodeResult{Status: "invalid", Action: "reject"}, nil
	})
	if err := rawMock.Start(); err != nil {
		t.Fatalf("start raw model mock: %v", err)
	}
	defer rawMock.Stop()

	repoRoot := findRepoRoot(t)
	binPath := filepath.Join(t.TempDir(), "cognitiveosd")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/cognitiveosd")
	buildCmd.Dir = repoRoot
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build daemon: %v\n%s", err, buildOut)
	}

	cmd := exec.Command(binPath,
		"--run", runDir,
		"--log-dir", logDir,
		"--models", t.TempDir(),
		"--patches", t.TempDir(),
		"--mcp-bin", "/nonexistent",
		"--inference", "http://127.0.0.1:1",
		"--audit-interval", "9999",
	)
	cmd.Stdout = &loggingWriter{t, "DAEMON: "}
	cmd.Stderr = &loggingWriter{t, "DAEMON: "}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Logf("daemon started (pid %d), waiting for socket %s", cmd.Process.Pid, daemonSock)
	defer cmd.Process.Kill()

	if !waitForFile(daemonSock, 5*time.Second) {
		t.Fatal("daemon socket did not appear within 5s")
	}

	conn := dialDaemon(t, daemonSock)
	defer conn.Close()

	payload, _ := json.Marshal(map[string]string{"code": "wake", "origin": "test"})
	msg := envelope{Type: "system_code", From: "test", Payload: payload}
	resp, status := sendAndReceive(t, conn, msg)
	if resp.Type != "code_accepted" {
		t.Fatalf("expected code_accepted, got %s", resp.Type)
	}
	if status.Status != "error" {
		t.Fatalf("expected error, got %s", status.Status)
	}
	if status.Error == nil || status.Error.Code != "E_INVALID_CODE" {
		t.Fatalf("expected E_INVALID_CODE, got %+v", status.Error)
	}
}

func TestDaemonIntegration_LargePayloadRejected(t *testing.T) {
	env := startDaemon(t)
	conn := dialDaemon(t, env.daemonSock)
	defer conn.Close()

	largeContent := fmt.Sprintf(`{"mode":"text","content":"%s"}`, strings.Repeat("A", 1536*1024))
	msg := envelope{
		Type:    "input_forward",
		From:    "test",
		Payload: json.RawMessage(largeContent),
	}
	_, status := sendAndReceive(t, conn, msg)
	if status.Status != "error" {
		t.Fatalf("expected error for large payload, got %s", status.Status)
	}
	if status.Error == nil || status.Error.Code != "E_TOO_LARGE" {
		t.Fatalf("expected E_TOO_LARGE, got %+v", status.Error)
	}
}
