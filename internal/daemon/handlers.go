package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (d *Daemon) handleInputForward(env Envelope, conn *ClientConn) {
	var payload InputPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}
	if payload.Content == "" {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", "content is required")
		return
	}

	d.SetState(StateProcessing)
	d.SendOK(env, conn, nil)

	sessionID := payload.Context.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}

	go func() {
		d.processPrompt(payload.Content, sessionID, env.From)
	}()
}

func (d *Daemon) processPrompt(prompt, sessionID, from string) {
	// 1. Validate prompt through Raw Model
	action, modifiedPrompt, reason, err := d.rmClient.ValidatePrompt(prompt)
	if err != nil {
		d.Log.Printf("raw model validate error: %v", err)
		d.SendToClient(from, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
			SessionID:   sessionID,
			Content:     fmt.Sprintf("Guardrail error: %v", err),
			ContentType: "text",
		}))
		d.SetState(StateListening)
		return
	}

	switch action {
	case "deny":
		msg := "Request blocked by system guardrail."
		if reason != "" {
			msg = "Guardrail: " + reason
		}
		d.SendToClient(from, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
			SessionID:   sessionID,
			Content:     msg,
			ContentType: "text",
		}))
		d.SetState(StateListening)
		return

	case "modify":
		if modifiedPrompt != "" {
			prompt = modifiedPrompt
		}
	case "allow":
		// proceed
	}

	// 2. Generate from Wide Model
	resp, err := d.wmClient.Generate(prompt)
	if err != nil {
		d.Log.Printf("inference error: %v", err)
		d.SendToClient(from, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
			SessionID:   sessionID,
			Content:     fmt.Sprintf("Error: %v", err),
			ContentType: "text",
		}))
		d.SetState(StateListening)
		return
	}

	// 3. Loop: parse response for tool calls, invoke tools, regenerate
	finalResponse := d.toolLoop(resp, prompt, sessionID)

	d.SetState(StateListening)
	d.SendToClient(from, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
		SessionID:   sessionID,
		Content:     finalResponse,
		ContentType: "text",
	}))
}

func (d *Daemon) toolLoop(response, originalPrompt, sessionID string) string {
	currentResponse := response
	maxLoops := 10
	for i := 0; i < maxLoops; i++ {
		toolCalls := parseToolCalls(currentResponse)

		if len(toolCalls) == 0 {
			return currentResponse
		}

		for _, tc := range toolCalls {
			result, err := d.mcpMgr.Invoke(tc.Tool, tc.Arguments, sessionID)
			if err != nil {
				d.Log.Printf("tool invoke error: %v", err)
				continue
			}
			d.Log.Printf("tool %s result: %s", tc.Tool, result.Status)
		}

		// Regenerate with tool results
		newResp, err := d.wmClient.Generate(originalPrompt + "\n\n[Tool results processing complete. Continue.]")
		if err != nil {
			d.Log.Printf("re-generate error: %v", err)
			return currentResponse
		}
		currentResponse = newResp
	}

	return currentResponse
}

func parseToolCalls(response string) []ToolCall {
	var calls []ToolCall
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "@@") || !strings.HasSuffix(line, "@@") {
			continue
		}
		inner := line[2 : len(line)-2]
		parenIdx := strings.Index(inner, "(")
		if parenIdx < 0 {
			continue
		}
		toolName := inner[:parenIdx]
		argsStr := inner[parenIdx+1 : len(inner)-1]

		args := make(map[string]interface{})
		if argsStr != "" {
			pairs := strings.Split(argsStr, ",")
			for _, pair := range pairs {
				pair = strings.TrimSpace(pair)
				eqIdx := strings.Index(pair, "=")
				if eqIdx < 0 {
					continue
				}
				k := strings.TrimSpace(pair[:eqIdx])
				v := strings.Trim(strings.TrimSpace(pair[eqIdx+1:]), "\"")
				args[k] = v
			}
		}

		calls = append(calls, ToolCall{Tool: toolName, Arguments: args})
	}
	return calls
}

func (d *Daemon) handleSystemCode(env Envelope, conn *ClientConn) {
	var payload SystemCodePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	code := strings.ToLower(payload.Code)
	effect := ""

	if d.rmClient.IsReady() {
		status, action, err := d.rmClient.ValidateSystemCode(code, payload.Origin)
		if err != nil {
			d.SendError(env, conn, "E_RAW_MODEL_ERROR", err.Error())
			return
		}
		if status != "valid" {
			d.SendError(env, conn, "E_INVALID_CODE", "system code rejected by raw model")
			return
		}
		_ = action
	}

	switch code {
	case "wake":
		effect = "waking from idle state"
		d.SetState(StateListening)

	case "idle":
		effect = "entering idle state"
		d.SetState(StateIdle)
		d.wmClient.Unload("idle")
		d.mcpMgr.ShutdownAll()

	case "security":
		effect = "SECURITY SHUTDOWN: terminating all processes"
		d.SetState(StateSecurity)
		d.wmClient.Unload("security")
		d.mcpMgr.ShutdownAll()
		d.shutdown("security_code")

	case "reset":
		effect = "RESET: wiping data and rebooting"
		d.wmClient.Unload("reset")
		d.mcpMgr.ShutdownAll()
		d.shutdown("reset_code")

	case "unlock":
		if payload.UnlockCode == "" {
			d.SendError(env, conn, "E_INVALID_PAYLOAD", "unlock_code required for unlock code")
			return
		}
		if d.rmClient.IsReady() {
			codeStatus, msg, err := d.rmClient.CheckUnlockCode(payload.UnlockCode, "wide-model")
			if err != nil {
				d.SendError(env, conn, "E_RAW_MODEL_ERROR", err.Error())
				return
			}
			if codeStatus != "accepted" {
				d.SendError(env, conn, "E_UNLOCK_DENIED", msg)
				return
			}
			effect = msg
		} else {
			effect = fmt.Sprintf("unlock code %s accepted", payload.UnlockCode)
		}

	default:
		d.SendError(env, conn, "E_INVALID_PAYLOAD", fmt.Sprintf("unknown system code: %s", code))
		return
	}

	d.SendOK(env, conn, CodeAcceptedPayload{Status: "ok", Effect: effect})
}

func (d *Daemon) handleMCPRegister(env Envelope, conn *ClientConn) {
	var payload MCPRegisterPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	serverName := payload.Server.Name
	if serverName == "" {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", "server name is required")
		return
	}

	d.mcpMgr.Register(serverName, payload.Tools, conn)

	var toolNames []string
	for _, t := range payload.Tools {
		toolNames = append(toolNames, t.Name)
	}

	d.SendOK(env, conn, MCPRegisteredPayload{
		Status:          "ok",
		ServerID:        serverName,
		RegisteredTools: toolNames,
	})

	d.Log.Printf("MCP server registered: %s (%d tools)", serverName, len(payload.Tools))
}

func (d *Daemon) handleMCPUnregister(env Envelope, conn *ClientConn) {
	var payload MCPUnregisterPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	serverID := env.From
	if serverID == "" {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", "from field is required")
		return
	}

	d.mcpMgr.Unregister(serverID)
	d.SendOK(env, conn, map[string]string{"server_id": serverID})
	d.Log.Printf("MCP server unregistered: %s (reason: %s)", serverID, payload.Reason)
}

func (d *Daemon) handleMCPResult(env Envelope, conn *ClientConn) {
	var payload MCPResultPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	d.Log.Printf("MCP result from %s: status=%s", env.From, payload.Status)
}

func (d *Daemon) handleAuditRequest(env Envelope, conn *ClientConn) {
	report := d.auditor.Collect()

	resp := Envelope{
		Type:      "audit_report",
		ID:        env.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		From:      "cognitiveosd",
	}
	payload := AuditReportPayload{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Resources: report,
	}
	respPayload, _ := json.Marshal(payload)
	resp.Payload = respPayload
	if err := conn.Send(resp); err != nil {
		d.Log.Printf("send audit_report: %v", err)
	}
}

func (d *Daemon) handleStatusRequest(env Envelope, conn *ClientConn) {
	state := string(d.CurrentState())
	uptime := int64(d.Uptime().Seconds())

	wmStatus := WideModelStatus{Status: "unloaded"}
	if d.wmClient.IsLoaded() {
		wmStatus = WideModelStatus{Status: "loaded", Name: d.wmClient.LoadedModelName()}
	}

	mcpCount := d.mcpMgr.ActiveCount()

	resp := Envelope{
		Type:      "status_response",
		ID:        env.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		From:      "cognitiveosd",
	}
	payload := StatusResponsePayload{
		State:            state,
		UptimeSeconds:    uptime,
		WideModel:        wmStatus,
		PatchesInstalled: 0,
		MCPServersActive: mcpCount,
	}
	respPayload, _ := json.Marshal(payload)
	resp.Payload = respPayload
	if err := conn.Send(resp); err != nil {
		d.Log.Printf("send status_response: %v", err)
	}
}
