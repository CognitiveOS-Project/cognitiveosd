package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	sessionID := ""
	if payload.Context != nil {
		sessionID = payload.Context.SessionID
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}

	go func() {
		d.processPrompt(payload.Content, sessionID, env.From)
	}()
}

func (d *Daemon) processPrompt(prompt, sessionID, from string) {
	d.touchIdleTimer()

	routingHints := d.modelRegistryRoutingHints()
	action, modifiedPrompt, reason, modelID, err := d.rmClient.ValidatePrompt(prompt, routingHints)
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
	}

	if modelID != "" {
		if err := d.hotSwapWideModel(modelID); err != nil {
			d.Log.Printf("hot-swap to %s failed: %v, using current model", modelID, err)
		}
	}

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

	finalResponse, toolResults := d.toolLoop(resp, prompt, sessionID)

	for _, tr := range toolResults {
		d.SendToClient(from, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
			SessionID:   sessionID,
			Content:     tr,
			ContentType: "tool_result",
		}))
	}

	d.SetState(StateListening)
	d.SendToClient(from, NewEnvelope("output_deliver", "cognitiveosd", OutputPayload{
		SessionID:   sessionID,
		Content:     finalResponse,
		ContentType: "text",
	}))
}

func (d *Daemon) toolLoop(response, originalPrompt, sessionID string) (string, []string) {
	currentResponse := response
	var toolResults []string
	maxLoops := 10
	for i := 0; i < maxLoops; i++ {
		toolCalls := parseToolCalls(currentResponse)

		if len(toolCalls) == 0 {
			return currentResponse, toolResults
		}

		var results []string
		for _, tc := range toolCalls {
			// Validate if tool is in a validated namespace
			if isToolValidated(tc.Tool) {
				op := operationFromTool(tc.Tool)
				pkgName, _ := tc.Arguments["name"].(string)
				version, _ := tc.Arguments["version"].(string)

				var manifestMeta *PackageManifestMetadata
				if op == "install" || op == "update" {
					pkg := pkgName
					if pkg == "" {
						if name, ok := tc.Arguments["package_name"].(string); ok {
							pkg = name
						}
					}
					if pkg != "" {
						manifestMeta = d.mcpMgr.fetchManifestMetadata(d.Config.RegistryURL, pkg, version)
					}
				}

				validationParams := PackageValidationParams{
					Operation:        op,
					PackageName:      pkgName,
					Version:          version,
					ManifestMetadata: manifestMeta,
				}

				validationResult, err := d.rmClient.ValidatePackageRequest(validationParams)
				if err != nil {
					d.Log.Printf("package validation error: %v", err)
					results = append(results, fmt.Sprintf("E_PACKAGE_MANIFEST_FETCH: Cannot validate %s: %v", tc.Tool, err))
					continue
				}

				if validationResult.Status != "approved" {
					reason := validationResult.Reason
					if reason == "" {
						reason = "operation denied by system guardrail"
					}
					d.Log.Printf("package validation denied: %s (%s)", tc.Tool, reason)
					results = append(results, fmt.Sprintf("E_PACKAGE_DENIED: Tool %s denied: %s", tc.Tool, reason))
					continue
				}
			}

			result, err := d.mcpMgr.Invoke(tc.Tool, tc.Arguments, sessionID)
			if err != nil {
				d.Log.Printf("tool invoke error: %v", err)
				results = append(results, fmt.Sprintf("Error calling %s: %v", tc.Tool, err))
				continue
			}

			var resultText string
			for _, c := range result.Content {
				resultText += c.Text
			}
			results = append(results, fmt.Sprintf("Tool %s returned: %s", tc.Tool, resultText))
			toolResults = append(toolResults, fmt.Sprintf("%s → %s", tc.Tool, resultText))
			d.Log.Printf("tool %s result: %s", tc.Tool, result.Status)
		}

		newResp, err := d.wmClient.Generate(originalPrompt + "\n\nTool results:\n" + strings.Join(results, "\n") + "\n\nContinue.")
		if err != nil {
			d.Log.Printf("re-generate error: %v", err)
			return currentResponse, toolResults
		}
		currentResponse = newResp
	}

	return currentResponse, toolResults
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

func (d *Daemon) handleWideModelLoad(env Envelope, conn *ClientConn) {
	var payload WideModelLoadPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	modelPath := payload.ModelPath
	if modelPath == "" {
		modelPath = filepath.Join(d.Config.ModelDir, "wide", "active")
	}

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		d.SendError(env, conn, "E_MODEL_NOT_FOUND", modelPath)
		return
	}

	if err := d.wmClient.Load(modelPath); err != nil {
		d.SendError(env, conn, "E_MODEL_LOAD_FAILED", err.Error())
		return
	}

	d.SendOK(env, conn, WideModelLoadedPayload{
		Status: "ok",
		ModelInfo: &WideModelInfo{
			Loaded:     d.wmClient.LoadedModelName(),
			RAMUsageMB: 0,
		},
	})
}

func (d *Daemon) handleWideModelUnload(env Envelope, conn *ClientConn) {
	var payload WideModelUnloadPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	reason := payload.Reason
	if reason == "" {
		reason = "requested"
	}

	if err := d.wmClient.Unload(reason); err != nil {
		d.SendError(env, conn, "E_MODEL_UNLOAD_FAILED", err.Error())
		return
	}

	d.SendOK(env, conn, WideModelUnloadedPayload{
		Status:     "ok",
		RAMFreedMB: 0,
	})
}

func (d *Daemon) handleSystemCode(env Envelope, conn *ClientConn) {
	var payload SystemCodePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		d.SendError(env, conn, "E_INVALID_PAYLOAD", err.Error())
		return
	}

	code := strings.ToLower(payload.Code)
	origin := strings.ToLower(payload.Origin)

	if code == "security" || code == "reset" {
		if origin == "keyboard" || origin == "voice" || origin == "cli" {
			d.Log.Printf("WARN: %s code rejected from software origin: %s", code, origin)
			d.SendError(env, conn, "E_UNAUTHORIZED", fmt.Sprintf("%s code requires physical trigger", code))
			return
		}
	}

	effect := ""

	if d.rmClient.IsReady() {
		status, action, err := d.rmClient.ValidateSystemCode(code, origin)
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
		d.SetState(StateIdleRequested)
		d.wmClient.Unload("idle")
		d.mcpMgr.ShutdownAll()
		d.SetState(StateIdle)

	case "security":
		effect = "SECURITY SHUTDOWN: terminating all processes"
		d.SetState(StateSecurity)
		_ = d.wmClient.Unload("security")
		d.mcpMgr.ShutdownAll()
		d.shutdown("security_code")

	case "reset":
		effect = "RESET: wiping data and rebooting"
		_ = d.wmClient.Unload("reset")
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

	d.SendOK(env, conn, AuditReportPayload{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Resources: report,
	})
}

func (d *Daemon) handleStatusRequest(env Envelope, conn *ClientConn) {
	state := string(d.CurrentState())
	uptime := int64(d.Uptime().Seconds())

	wmStatus := WideModelStatus{Status: "unloaded"}
	if d.CurrentState() == StateProcessing || d.CurrentState() == StateListening {
		if !d.wmClient.IsLoaded() {
			wmStatus = WideModelStatus{Status: "loading"}
		}
	}
	if d.wmClient.IsLoaded() {
		wmStatus = WideModelStatus{
			Status:  "loaded",
			Name:    d.wmClient.LoadedModelName(),
			ModelID: d.wmClient.LoadedModelID(),
		}
	}

	mcpCount := d.mcpMgr.ActiveCount()
	regCount := len(d.modelRegistryRoutingHints())

	d.SendOK(env, conn, StatusResponsePayload{
		State:            state,
		UptimeSeconds:    uptime,
		WideModel:        wmStatus,
		ModelRegistry:    regCount,
		PatchesInstalled: d.patchCount(),
		MCPServersActive: mcpCount,
	})
}
