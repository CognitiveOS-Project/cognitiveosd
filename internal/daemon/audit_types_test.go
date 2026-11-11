package daemon

import (
	"testing"
)

func TestAuditResourcesZero(t *testing.T) {
	ar := AuditResources{}
	if ar.RAM.TotalMB != 0 {
		t.Fatalf("expected 0 total MB, got %d", ar.RAM.TotalMB)
	}
	if ar.RAM.AvailableMB != 0 {
		t.Fatalf("expected 0 available MB, got %d", ar.RAM.AvailableMB)
	}
	if ar.Storage.TotalMB != 0 {
		t.Fatalf("expected 0 total storage MB, got %d", ar.Storage.TotalMB)
	}
	if ar.CPU.LoadPercent != 0 {
		t.Fatalf("expected 0 CPU percent, got %f", ar.CPU.LoadPercent)
	}
}

func TestRAMInfoFields(t *testing.T) {
	ram := RAMInfo{TotalMB: 8192, AvailableMB: 4096, UsedByAIMB: 1024}
	if ram.TotalMB != 8192 {
		t.Fatalf("expected 8192 total MB, got %d", ram.TotalMB)
	}
	if ram.AvailableMB != 4096 {
		t.Fatalf("expected 4096 available MB, got %d", ram.AvailableMB)
	}
	if ram.UsedByAIMB != 1024 {
		t.Fatalf("expected 1024 used by AI MB, got %d", ram.UsedByAIMB)
	}
}

func TestStorageInfoFields(t *testing.T) {
	storage := StorageInfo{TotalMB: 512000, AvailableMB: 128000, PatchesMB: 10000, ModelsMB: 50000}
	if storage.TotalMB != 512000 {
		t.Fatalf("expected 512000 total MB, got %d", storage.TotalMB)
	}
	if storage.AvailableMB != 128000 {
		t.Fatalf("expected 128000 available MB, got %d", storage.AvailableMB)
	}
	if storage.PatchesMB != 10000 {
		t.Fatalf("expected 10000 patches MB, got %d", storage.PatchesMB)
	}
	if storage.ModelsMB != 50000 {
		t.Fatalf("expected 50000 models MB, got %d", storage.ModelsMB)
	}
}

func TestCPUInfoFields(t *testing.T) {
	cpu := CPUInfo{Cores: 4, LoadPercent: 42.5}
	if cpu.Cores != 4 {
		t.Fatalf("expected 4 cores, got %d", cpu.Cores)
	}
	if cpu.LoadPercent != 42.5 {
		t.Fatalf("expected 42.5 load percent, got %f", cpu.LoadPercent)
	}
}

func TestNPUInfoFields(t *testing.T) {
	npu := NPUInfo{Available: true, Model: "test-npu", MemoryMB: 4096}
	if !npu.Available {
		t.Fatal("expected NPU available")
	}
	if npu.Model != "test-npu" {
		t.Fatalf("expected test-npu model, got %s", npu.Model)
	}
	if npu.MemoryMB != 4096 {
		t.Fatalf("expected 4096 NPU memory MB, got %d", npu.MemoryMB)
	}
}

func TestNetworkInfoFields(t *testing.T) {
	net := NetworkInfo{Connected: true, Interface: "eth0", SignalPercent: 85}
	if !net.Connected {
		t.Fatal("expected network connected")
	}
	if net.Interface != "eth0" {
		t.Fatalf("expected eth0 interface, got %s", net.Interface)
	}
	if net.SignalPercent != 85 {
		t.Fatalf("expected 85 signal percent, got %d", net.SignalPercent)
	}
}

func TestSystemCodePayloadValidation(t *testing.T) {
	scp := SystemCodePayload{Code: "security", Origin: "button"}
	if scp.Code != "security" {
		t.Fatalf("expected security code, got %s", scp.Code)
	}
	if scp.Origin != "button" {
		t.Fatalf("expected button origin, got %s", scp.Origin)
	}
}

func TestMCPRegisterPayloadValidation(t *testing.T) {
	mrp := MCPRegisterPayload{
		Server: MCPInfo{Name: "test-mcp", Version: "1.0.0", Transport: "stdio"},
		Tools: []MCPTool{
			{Name: "test.tool", Description: "A test tool", InputSchema: map[string]interface{}{"type": "object"}},
		},
	}
	if mrp.Server.Name != "test-mcp" {
		t.Fatalf("expected test-mcp, got %s", mrp.Server.Name)
	}
	if len(mrp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(mrp.Tools))
	}
	if mrp.Tools[0].Name != "test.tool" {
		t.Fatalf("expected test.tool, got %s", mrp.Tools[0].Name)
	}
}

func TestAuditRequestPayloadEmpty(t *testing.T) {
	arp := AuditRequestPayload{}
	var _ = arp
}

func TestMCPResultPayload(t *testing.T) {
	mrp := MCPResultPayload{
		Status: "ok",
		Content: []ContentItem{
			{Type: "text", Text: "success"},
		},
	}
	if mrp.Status != "ok" {
		t.Fatalf("expected ok status, got %s", mrp.Status)
	}
	if len(mrp.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(mrp.Content))
	}
	if mrp.Content[0].Type != "text" {
		t.Fatalf("expected text content type, got %s", mrp.Content[0].Type)
	}
	if mrp.Content[0].Text != "success" {
		t.Fatalf("expected success text, got %s", mrp.Content[0].Text)
	}
}
