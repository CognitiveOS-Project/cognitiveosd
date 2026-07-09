package daemon

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Auditor struct {
	daemon *Daemon
	done   chan struct{}
}

func NewAuditor(d *Daemon) *Auditor {
	return &Auditor{
		daemon: d,
		done:   make(chan struct{}),
	}
}

func (a *Auditor) Start() {
	go func() {
		ticker := time.NewTicker(time.Duration(a.daemon.Config.AuditInterval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				report := a.Collect()
				a.saveReport(report)
				a.broadcastReport(report)
			case <-a.done:
				return
			}
		}
	}()
	a.daemon.Log.Printf("audit loop started (interval: %ds)", a.daemon.Config.AuditInterval)
}

func (a *Auditor) Stop() {
	close(a.done)
}

func (a *Auditor) Collect() AuditResources {
	return AuditResources{
		RAM:     a.readRAM(),
		Storage: a.readStorage(),
		CPU:     a.readCPU(),
		NPU:     a.readNPU(),
		Network: a.readNetwork(),
	}
}

func (a *Auditor) readRAM() RAMInfo {
	totalMB := int64(8192)
	availableMB := int64(4096)

	data, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				var kb int64
				if _, err := fmt.Sscanf(line, "MemTotal: %d kB", &kb); err == nil {
					totalMB = kb / 1024
				}
			}
			if strings.HasPrefix(line, "MemAvailable:") {
				var kb int64
				if _, err := fmt.Sscanf(line, "MemAvailable: %d kB", &kb); err == nil {
					availableMB = kb / 1024
				}
			}
		}
	}

	return RAMInfo{
		TotalMB:     totalMB,
		AvailableMB: availableMB,
		UsedByAIMB:  totalMB - availableMB,
	}
}

func (a *Auditor) readStorage() StorageInfo {
	totalMB := int64(32768)
	availableMB := int64(12288)

	var stat syscall.Statfs_t
	root := "/"
	if err := syscall.Statfs(root, &stat); err == nil {
		totalBytes := int64(stat.Blocks) * stat.Bsize
		availBytes := int64(stat.Bavail) * stat.Bsize
		totalMB = totalBytes / (1024 * 1024)
		availableMB = availBytes / (1024 * 1024)
	}

	patchesMB := a.dirSizeMB(a.daemon.Config.PatchDir)
	modelsMB := a.dirSizeMB(a.daemon.Config.ModelDir)

	return StorageInfo{
		TotalMB:     totalMB,
		AvailableMB: availableMB,
		PatchesMB:   patchesMB,
		ModelsMB:    modelsMB,
	}
}

func (a *Auditor) readCPU() CPUInfo {
	cores := 0
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "processor") {
				cores++
			}
		}
	}
	if cores == 0 {
		cores = 1
	}

	loadPercent := 0.0
	data, err = os.ReadFile("/proc/loadavg")
	if err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			if _, err := fmt.Sscanf(fields[0], "%f", &loadPercent); err != nil {
				a.daemon.Log.Printf("parse loadavg: %v", err)
			}
			loadPercent = loadPercent / float64(cores) * 100
		}
	}

	return CPUInfo{
		Cores:       cores,
		LoadPercent: loadPercent,
	}
}

func (a *Auditor) readNPU() NPUInfo {
	entries, err := os.ReadDir("/sys/class/accelerator")
	if err != nil || len(entries) == 0 {
		entries, err = os.ReadDir("/dev")
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "npu") || strings.HasPrefix(e.Name(), "hailo") || strings.HasPrefix(e.Name(), "neural") {
					return NPUInfo{Available: true, Model: e.Name()}
				}
			}
		}
		return NPUInfo{Available: false}
	}
	return NPUInfo{Available: true, Model: entries[0].Name()}
}

func (a *Auditor) readNetwork() NetworkInfo {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil || len(entries) == 0 {
		return NetworkInfo{Connected: false}
	}

	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		operState, err := os.ReadFile(filepath.Join("/sys/class/net", name, "operstate"))
		if err == nil && strings.TrimSpace(string(operState)) == "up" {
			return NetworkInfo{
				Connected:     true,
				Interface:     name,
				SignalPercent: 80,
			}
		}
	}

	return NetworkInfo{Connected: false}
}

func (a *Auditor) dirSizeMB(path string) int64 {
	var total int64
	if err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	}); err != nil {
		a.daemon.Log.Printf("dir size walk %s: %v", path, err)
	}
	return int64(math.Ceil(float64(total) / (1024 * 1024)))
}

func (a *Auditor) saveReport(r AuditResources) {
	if err := os.MkdirAll(a.daemon.Config.AuditDir, 0755); err != nil {
		return
	}
	report := AuditReportPayload{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Resources: r,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		a.daemon.Log.Printf("marshal report: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(a.daemon.Config.AuditDir, "current.json"), data, 0644); err != nil {
		a.daemon.Log.Printf("save report: %v", err)
	}
}

func (a *Auditor) broadcastReport(r AuditResources) {
	env := NewEnvelope("audit_report", "cognitiveosd", AuditReportPayload{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Resources: r,
	})
	a.daemon.Broadcast(env)
}
