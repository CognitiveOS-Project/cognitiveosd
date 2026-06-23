package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	SocketPath  string
	PidFilePath string
	LogFile     string
	ModelDir    string
	PatchDir    string
	RunDir      string
	AuditDir    string
	LogDir      string
	InferenceURL string
	AuditInterval int
	MCPBinDir   string
	MCPBridges  []string
}

var Default = Config{
	SocketPath:    "/cognitiveos/run/daemon.sock",
	PidFilePath:   "/cognitiveos/run/cognitiveosd.pid",
	LogFile:       "/cognitiveos/logs/cognitiveosd.log",
	ModelDir:      "/cognitiveos/models",
	PatchDir:      "/cognitiveos/patches",
	RunDir:        "/cognitiveos/run",
	AuditDir:      "/cognitiveos/audit",
	LogDir:        "/cognitiveos/logs",
	InferenceURL:  "http://127.0.0.1:11434",
	AuditInterval: 60,
	MCPBinDir:     "/cognitiveos/bin",
	MCPBridges: []string{
		"display-mcp",
		"audio-mcp",
		"network-mcp",
		"gpio-mcp",
		"serial-mcp",
	},
}

func FromEnv() Config {
	c := Default
	if v := os.Getenv("COGNITIVEOS_SOCKET"); v != "" {
		c.SocketPath = v
	}
	if v := os.Getenv("COGNITIVEOS_MODEL_DIR"); v != "" {
		c.ModelDir = v
	}
	if v := os.Getenv("COGNITIVEOS_PATCH_DIR"); v != "" {
		c.PatchDir = v
	}
	if v := os.Getenv("COGNITIVEOS_RUN_DIR"); v != "" {
		c.RunDir = v
	}
	if v := os.Getenv("COGNITIVEOS_LOG_DIR"); v != "" {
		c.LogDir = v
	}
	if v := os.Getenv("COGNITIVEOS_INFERENCE_URL"); v != "" {
		c.InferenceURL = v
	}
	if v := os.Getenv("COGNITIVEOS_MCP_BIN_DIR"); v != "" {
		c.MCPBinDir = v
	}

	// Derive paths from base dirs
	c.SocketPath = filepath.Join(c.RunDir, "daemon.sock")
	c.PidFilePath = filepath.Join(c.RunDir, "cognitiveosd.pid")
	c.LogFile = filepath.Join(c.LogDir, "cognitiveosd.log")
	c.AuditDir = filepath.Join(c.RunDir, "..", "audit")

	return c
}

func (c *Config) EnsureDirs() error {
	dirs := []string{c.RunDir, c.LogDir, c.AuditDir, filepath.Dir(c.SocketPath)}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
