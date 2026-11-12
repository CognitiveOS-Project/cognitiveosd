package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/CognitiveOS-Project/cogsdk/client/mcp"
)

// RawModelClient is a client for the raw model server (cograw) via MCP.
// It wraps cogsdk/client/mcp.Client to provide the RawModelClientI interface.
type RawModelClient struct {
	mu     sync.Mutex
	client *mcp.Client
	daemon *Daemon
	ready  bool
}

// NewRawModelClient creates a new RawModelClient that connects to cograw
// via the MCP protocol over a Unix socket.
func NewRawModelClient(d *Daemon) *RawModelClient {
	return &RawModelClient{
		daemon: d,
	}
}

// Connect establishes a connection to the raw model server via MCP.
func (r *RawModelClient) Connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.client != nil {
		r.client = nil
	}

	addr, err := net.ResolveUnixAddr("unix", r.daemon.Config.RawSocketPath)
	if err != nil {
		return fmt.Errorf("resolve raw socket: %w", err)
	}

	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return fmt.Errorf("connect to raw model: %w", err)
	}

	r.client = mcp.NewClient(conn, conn)
	r.ready = true
	r.daemon.Log.Printf("raw model connected via MCP")
	return nil
}

// ValidateSystemCode validates a system code via the raw model.
func (r *RawModelClient) ValidateSystemCode(code, origin string) (string, string, error) {
	if !r.IsReady() {
		return "", "", fmt.Errorf("E_NOT_CONNECTED: raw model not connected")
	}

	result, err := r.client.Call("validate_system_code", map[string]interface{}{
		"code":   code,
		"origin": origin,
	})
	if err != nil {
		return "", "", fmt.Errorf("validate system code: %w", err)
	}

	var resp struct {
		Status string `json:"status"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(result.Text()), &resp); err != nil {
		return "", "", fmt.Errorf("parse validate response: %w", err)
	}

	return resp.Status, resp.Action, nil
}

// CheckUnlockCode validates an unlock code via the raw model.
func (r *RawModelClient) CheckUnlockCode(code, patchName string) (string, string, error) {
	if !r.IsReady() {
		return "", "", fmt.Errorf("E_NOT_CONNECTED: raw model not connected")
	}

	result, err := r.client.Call("check_unlock_code", map[string]interface{}{
		"code":       code,
		"patch_name": patchName,
	})
	if err != nil {
		return "", "", err
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(result.Text()), &resp); err != nil {
		return "", "", fmt.Errorf("parse unlock response: %w", err)
	}

	return resp.Status, resp.Message, nil
}

// ValidatePrompt validates a prompt via the raw model.
func (r *RawModelClient) ValidatePrompt(prompt string, routingHints map[string][]string) (string, string, string, string, error) {
	if !r.IsReady() {
		return "", "", "", "", fmt.Errorf("E_NOT_CONNECTED: raw model not connected")
	}

	params := map[string]interface{}{
		"prompt": prompt,
	}
	if len(routingHints) > 0 {
		params["routing_hints"] = routingHints
	}

	result, err := r.client.Call("validate_prompt", params)
	if err != nil {
		return "", "", "", "", err
	}

	var resp struct {
		Action         string `json:"action"`
		ModifiedPrompt string `json:"modified_prompt,omitempty"`
		Reason         string `json:"reason,omitempty"`
		Model          string `json:"model,omitempty"`
	}
	if err := json.Unmarshal([]byte(result.Text()), &resp); err != nil {
		return "", "", "", "", fmt.Errorf("parse validate_prompt response: %w", err)
	}

	return resp.Action, resp.ModifiedPrompt, resp.Reason, resp.Model, nil
}

// ValidatePackageRequest validates a package request via the raw model.
func (r *RawModelClient) ValidatePackageRequest(params PackageValidationParams) (PackageValidationResult, error) {
	if !r.IsReady() {
		return PackageValidationResult{}, fmt.Errorf("E_NOT_CONNECTED: raw model not connected")
	}

	callParams := map[string]interface{}{
		"operation":        params.Operation,
		"patch_name":       params.PackageName,
		"version":          params.Version,
		"manifest_metadata": params.ManifestMetadata,
	}

	result, err := r.client.Call("validate_package_request", callParams)
	if err != nil {
		return PackageValidationResult{}, err
	}

	var resp PackageValidationResult
	if err := json.Unmarshal([]byte(result.Text()), &resp); err != nil {
		return PackageValidationResult{}, fmt.Errorf("parse validate_package_request response: %w", err)
	}

	return resp, nil
}

// AuditResources audits resources via the raw model.
func (r *RawModelClient) AuditResources(requestedMB int64) (bool, int64, int64, bool, error) {
	if !r.IsReady() {
		return false, 0, 0, false, fmt.Errorf("E_NOT_CONNECTED: raw model not connected")
	}

	result, err := r.client.Call("audit_resources", map[string]interface{}{
		"requested_mb": requestedMB,
	})
	if err != nil {
		return false, 0, 0, false, err
	}

	var resp struct {
		Available bool   `json:"available"`
		TotalMB   int64  `json:"total_mb"`
		FreeMB    int64  `json:"free_mb"`
		Allowed   bool   `json:"allowed"`
	}
	if err := json.Unmarshal([]byte(result.Text()), &resp); err != nil {
		return false, 0, 0, false, fmt.Errorf("parse audit response: %w", err)
	}

	return resp.Available, resp.TotalMB, resp.FreeMB, resp.Allowed, nil
}

// IsReady returns whether the raw model client is connected and ready.
func (r *RawModelClient) IsReady() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready && r.client != nil
}

// Close closes the connection to the raw model server.
func (r *RawModelClient) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.client = nil
	r.ready = false
}