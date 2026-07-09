package daemon

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

type Envelope struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	From      string          `json:"from,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type PayloadStatus struct {
	Status string       `json:"status"`
	Error  *ErrorInfo   `json:"error,omitempty"`
	Data   interface{}  `json:"data,omitempty"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InputPayload struct {
	Mode    string        `json:"mode"`
	Content string        `json:"content"`
	Context *InputContext `json:"context,omitempty"`
}

type InputContext struct {
	SessionID string `json:"session_id"`
	Device    string `json:"device,omitempty"`
}

type OutputPayload struct {
	SessionID   string      `json:"session_id"`
	Content     string      `json:"content"`
	ContentType string      `json:"content_type"`
	Media       *MediaInfo  `json:"media,omitempty"`
	Actions     []Action    `json:"actions,omitempty"`
}

type MediaInfo struct {
	Type          string `json:"type"`
	Paths         []string `json:"paths"`
	RenderCommand string `json:"render_command"`
}

type Action struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

type SystemCodePayload struct {
	Code       string `json:"code"`
	UnlockCode string `json:"unlock_code,omitempty"`
	Origin     string `json:"origin"`
}

type CodeAcceptedPayload struct {
	Status string `json:"status"`
	Effect string `json:"effect"`
}

type MCPRegisterPayload struct {
	Server MCPInfo    `json:"server"`
	Tools  []MCPTool  `json:"tools"`
}

type MCPInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Transport string `json:"transport"`
	PID       int    `json:"pid,omitempty"`
}

type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type MCPRegisteredPayload struct {
	Status          string   `json:"status"`
	ServerID        string   `json:"server_id"`
	RegisteredTools []string `json:"registered_tools"`
}

type MCPUnregisterPayload struct {
	Reason string `json:"reason"`
}

type ToolCall struct {
	Tool   string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments"`
}

type MCPResultPayload struct {
	Status  string         `json:"status"`
	Error   *ErrorInfo     `json:"error,omitempty"`
	Content []ContentItem  `json:"content,omitempty"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AuditRequestPayload struct{}

type AuditReportPayload struct {
	Timestamp string          `json:"timestamp"`
	Resources AuditResources  `json:"resources"`
}

type AuditResources struct {
	RAM     RAMInfo     `json:"ram"`
	Storage StorageInfo `json:"storage"`
	CPU     CPUInfo     `json:"cpu"`
	NPU     NPUInfo     `json:"npu"`
	Network NetworkInfo `json:"network"`
}

type RAMInfo struct {
	TotalMB    int64 `json:"total_mb"`
	AvailableMB int64 `json:"available_mb"`
	UsedByAIMB int64 `json:"used_by_ai_mb"`
}

type StorageInfo struct {
	TotalMB   int64 `json:"total_mb"`
	AvailableMB int64 `json:"available_mb"`
	PatchesMB int64 `json:"patches_mb"`
	ModelsMB  int64 `json:"models_mb"`
}

type CPUInfo struct {
	Cores      int     `json:"cores"`
	LoadPercent float64 `json:"load_percent"`
}

type NPUInfo struct {
	Available bool   `json:"available"`
	Model     string `json:"model,omitempty"`
	MemoryMB  int64  `json:"memory_mb,omitempty"`
}

type NetworkInfo struct {
	Connected     bool   `json:"connected"`
	Interface     string `json:"interface,omitempty"`
	SignalPercent int    `json:"signal_percent,omitempty"`
}

type StatusRequestPayload struct{}

type StatusResponsePayload struct {
	State            string           `json:"state"`
	UptimeSeconds    int64            `json:"uptime_seconds"`
	WideModel        WideModelStatus  `json:"wide_model"`
	ModelRegistry    int              `json:"model_registry_count"`
	PatchesInstalled int              `json:"patches_installed"`
	MCPServersActive int              `json:"mcp_servers_active"`
}

type WideModelStatus struct {
	Status  string `json:"status"`
	Name    string `json:"name,omitempty"`
	ModelID string `json:"model_id,omitempty"`
}

type ModelRegistryEntry struct {
	ModelID     string   `json:"model_id"`
	Tags        []string `json:"tags,omitempty"`
	GGUFFilePath string `json:"gguf_file_path"`
}

type WideModelLoadPayload struct {
	ModelPath    string                 `json:"model_path"`
	SystemPrompt string                 `json:"system_prompt,omitempty"`
	Params       map[string]interface{} `json:"params,omitempty"`
}

type WideModelLoadedPayload struct {
	Status    string             `json:"status"`
	Error     *ErrorInfo         `json:"error,omitempty"`
	ModelInfo *WideModelInfo     `json:"model_info,omitempty"`
}

type WideModelInfo struct {
	Loaded           string  `json:"loaded"`
	RAMUsageMB       int64   `json:"ram_usage_mb"`
	TokensPerSecond  float64 `json:"tokens_per_second"`
}

type WideModelUnloadPayload struct {
	Reason string `json:"reason"`
}

type WideModelUnloadedPayload struct {
	Status    string `json:"status"`
	RAMFreedMB int64 `json:"ram_freed_mb"`
}

type ShutdownNoticePayload struct {
	Reason string `json:"reason"`
}

type PackageValidationParams struct {
	Operation        string                  `json:"operation"`
	PackageName      string                  `json:"package_name"`
	Version          string                  `json:"version,omitempty"`
	ManifestMetadata *PackageManifestMetadata `json:"manifest_metadata,omitempty"`
}

type PackageManifestMetadata struct {
	HasRawModel bool   `json:"has_raw_model,omitempty"`
	DiskSpaceMB int64  `json:"disk_space_mb,omitempty"`
	Registry    string `json:"registry,omitempty"`
	IsCritical  bool   `json:"is_critical,omitempty"`
}

type PackageValidationResult struct {
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Command string `json:"command"`
}

type PackageRegistryManifest struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	HasRawModel bool `json:"has_raw_model,omitempty"`
	IsCritical  bool `json:"is_critical,omitempty"`
	DiskSpaceMB int64 `json:"disk_space_mb,omitempty"`
	Registry    string `json:"registry,omitempty"`
}

func uuidV4() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func NewEnvelope(msgType string, from string, payload interface{}) Envelope {
	b, _ := json.Marshal(payload)
	return Envelope{
		Type:      msgType,
		ID:        uuidV4(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		From:      from,
		Payload:   b,
	}
}

func ErrorPayload(code, message string) PayloadStatus {
	return PayloadStatus{Status: "error", Error: &ErrorInfo{Code: code, Message: message}}
}

func OKPayload(data interface{}) PayloadStatus {
	return PayloadStatus{Status: "ok", Data: data}
}
