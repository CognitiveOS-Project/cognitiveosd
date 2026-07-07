package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

type RawModelClient struct {
	daemon *Daemon
	conn   net.Conn
	mu     sync.Mutex
	ready  bool
}

type RawRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RawRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewRawModelClient(d *Daemon) *RawModelClient {
	return &RawModelClient{
		daemon: d,
	}
}

func (r *RawModelClient) Connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil {
		r.conn.Close()
	}

	addr, err := net.ResolveUnixAddr("unix", r.daemon.Config.RawSocketPath)
	if err != nil {
		return fmt.Errorf("resolve raw socket: %w", err)
	}

	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return fmt.Errorf("connect to raw model: %w", err)
	}

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r.conn = conn

	health, err := r.call("healthcheck", nil)
	if err != nil {
		conn.Close()
		r.conn = nil
		return fmt.Errorf("raw healthcheck: %w", err)
	}

	var healthResult struct {
		Status      string `json:"status"`
		ModelLoaded bool   `json:"model_loaded"`
	}
	if err := json.Unmarshal(health, &healthResult); err != nil {
		conn.Close()
		r.conn = nil
		return fmt.Errorf("parse health result: %w", err)
	}

	if healthResult.Status != "ready" {
		conn.Close()
		r.conn = nil
		return fmt.Errorf("raw model not ready: %s", healthResult.Status)
	}

	r.ready = true
	r.daemon.Log.Printf("raw model connected (model loaded: %v)", healthResult.ModelLoaded)
	return nil
}

func (r *RawModelClient) ValidateSystemCode(code, origin string) (string, string, error) {
	params := map[string]string{
		"code":   code,
		"origin": origin,
	}

	result, err := r.call("validate_system_code", params)
	if err != nil {
		return "", "", err
	}

	var resp struct {
		Status string `json:"status"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", "", fmt.Errorf("parse validate response: %w", err)
	}

	return resp.Status, resp.Action, nil
}

func (r *RawModelClient) CheckUnlockCode(code, patchName string) (string, string, error) {
	params := map[string]string{
		"code":       code,
		"patch_name": patchName,
	}

	result, err := r.call("check_unlock_code", params)
	if err != nil {
		return "", "", err
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", "", fmt.Errorf("parse unlock response: %w", err)
	}

	return resp.Status, resp.Message, nil
}

func (r *RawModelClient) AuditResources(requestedMB int64) (bool, int64, int64, bool, error) {
	params := map[string]int64{
		"requested_mb": requestedMB,
	}

	result, err := r.call("audit_resources", params)
	if err != nil {
		return false, 0, 0, false, err
	}

	var resp struct {
		Available bool  `json:"available"`
		TotalMB   int64 `json:"total_mb"`
		FreeMB    int64 `json:"free_mb"`
		Allowed   bool  `json:"allowed"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return false, 0, 0, false, fmt.Errorf("parse audit response: %w", err)
	}

	return resp.Available, resp.TotalMB, resp.FreeMB, resp.Allowed, nil
}

func (r *RawModelClient) ValidatePrompt(prompt string, routingHints map[string][]string) (string, string, string, string, error) {
	params := map[string]interface{}{
		"prompt": prompt,
	}
	if len(routingHints) > 0 {
		params["routing_hints"] = routingHints
	}

	result, err := r.call("validate_prompt", params)
	if err != nil {
		return "", "", "", "", err
	}

	var resp struct {
		Action         string `json:"action"`
		ModifiedPrompt string `json:"modified_prompt,omitempty"`
		Reason         string `json:"reason,omitempty"`
		Model          string `json:"model,omitempty"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", "", "", "", fmt.Errorf("parse validate_prompt response: %w", err)
	}

	return resp.Action, resp.ModifiedPrompt, resp.Reason, resp.Model, nil
}

func (r *RawModelClient) ValidatePackageRequest(params PackageValidationParams) (PackageValidationResult, error) {
	result, err := r.call("validate_package_request", params)
	if err != nil {
		return PackageValidationResult{}, err
	}

	var resp PackageValidationResult
	if err := json.Unmarshal(result, &resp); err != nil {
		return PackageValidationResult{}, fmt.Errorf("parse validate_package_request response: %w", err)
	}

	return resp, nil
}

func (r *RawModelClient) IsReady() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready && r.conn != nil
}

func (r *RawModelClient) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
	r.ready = false
}

var rpcID int

func (r *RawModelClient) call(method string, params interface{}) (json.RawMessage, error) {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("E_NOT_CONNECTED: raw model not connected")
	}

	rpcID++

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}

	req := RawRPCRequest{
		JSONRPC: "2.0",
		ID:      rpcID,
		Method:  method,
		Params:  rawParams,
	}

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		r.mu.Lock()
		r.conn = nil
		r.ready = false
		r.mu.Unlock()
		return nil, fmt.Errorf("E_CONNECTION_LOST: send to raw model: %w", err)
	}

	var resp RawRPCResponse
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&resp); err != nil {
		r.mu.Lock()
		r.conn = nil
		r.ready = false
		r.mu.Unlock()
		return nil, fmt.Errorf("E_CONNECTION_LOST: read from raw model: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}

	if resp.Result == nil {
		return nil, fmt.Errorf("E_EMPTY_RESPONSE: no result from raw model")
	}

	return resp.Result, nil
}
