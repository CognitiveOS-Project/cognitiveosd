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
		resp, err := d.wmClient.Generate(payload.Content)
		if err != nil {
			d.Log.Printf("inference error: %v", err)
			d.SendToClient(env.From, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
				SessionID:   sessionID,
				Content:     fmt.Sprintf("Error: %v", err),
				ContentType: "text",
			}))
			d.SetState(StateListening)
			return
		}

		d.SetState(StateListening)

		d.SendToClient(env.From, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
			SessionID:   sessionID,
			Content:     resp,
			ContentType: "text",
		}))
	}()
}

func (d *Daemon) handleSystemCode(env Envelope, conn *ClientConn) {
	var payload SystemCodePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	code := strings.ToLower(payload.Code)
	effect := ""

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
		effect = fmt.Sprintf("unlock code %s accepted", payload.UnlockCode)

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
