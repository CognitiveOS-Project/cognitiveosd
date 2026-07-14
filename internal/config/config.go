package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	SocketPath        string
	RawSocketPath     string
	PidFilePath       string
	LogFile           string
	ModelDir          string
	PatchDir          string
	RunDir            string
	AuditDir          string
	LogDir            string
	InferenceURL      string
	RegistryURL       string
	AuditInterval     int
	MCPBinDir         string
	MCPBridges        []string
	RawModelPath  string
	SocketPathExplicit bool
}

var Default = Config{
	SocketPath:    "/cognitiveos/run/daemon.sock",
	RawSocketPath: "/cognitiveos/run/raw.sock",
	PidFilePath:   "/cognitiveos/run/cognitiveosd.pid",
	LogFile:       "/cognitiveos/logs/cognitiveosd.log",
	ModelDir:      "/cognitiveos/models",
	PatchDir:      "/cognitiveos/patches",
	RunDir:        "/cognitiveos/run",
	AuditDir:      "/cognitiveos/audit",
	LogDir:        "/cognitiveos/logs",
	InferenceURL:  "http://127.0.0.1:11434",
	RegistryURL:   "https://registry.cognitiveos.org",
	AuditInterval: 60,
	MCPBinDir:     "/cognitiveos/bin",
	MCPBridges: []string{
		"display-mcp",
		"audio-mcp",
		"network-mcp",
		"gpio-mcp",
		"serial-mcp",
		"package-mcp",
	},
	RawModelPath: "/cognitiveos/models/raw/raw-model.gguf",
}

func FromTOML(path string) Config {
	c := Default

	type tomlConfig struct {
		Daemon struct {
			SocketPath    string `toml:"socket_path"`
			AuditInterval int    `toml:"audit_interval_seconds"`
			MCPBinDir     string `toml:"mcp_bin_dir"`
		} `toml:"daemon"`
		RawModel struct {
			Model string `toml:"model"`
		} `toml:"raw_model"`
		Inference struct {
			Endpoint string `toml:"endpoint"`
		} `toml:"inference"`
	}

	var tc tomlConfig
	if _, err := toml.DecodeFile(path, &tc); err == nil {
	if tc.Daemon.SocketPath != "" {
		c.SocketPath = tc.Daemon.SocketPath
		c.SocketPathExplicit = true
	}
		if tc.Daemon.AuditInterval != 0 {
			c.AuditInterval = tc.Daemon.AuditInterval
		}
		if tc.Daemon.MCPBinDir != "" {
			c.MCPBinDir = tc.Daemon.MCPBinDir
		}
		if tc.RawModel.Model != "" {
			c.RawModelPath = tc.RawModel.Model
		}
		if tc.Inference.Endpoint != "" {
			c.InferenceURL = tc.Inference.Endpoint
		}
	}

	return c
}

func FromEnv(tomlPath string) Config {
	c := FromTOML(tomlPath)

	if v := os.Getenv("COGNITIVEOS_SOCKET"); v != "" {
		c.SocketPath = v
		c.SocketPathExplicit = true
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
	if v := os.Getenv("COGNITIVEOS_RAW_MODEL_PATH"); v != "" {
		c.RawModelPath = v
	}

	c.Derive()

	return c
}

// Derive recalculates paths that depend on RunDir/LogDir after overrides.
func (c *Config) Derive() {
	if !c.SocketPathExplicit {
		c.SocketPath = filepath.Join(c.RunDir, "daemon.sock")
	}
	c.RawSocketPath = filepath.Join(c.RunDir, "raw.sock")
	c.PidFilePath = filepath.Join(c.RunDir, "cognitiveosd.pid")
	c.LogFile = filepath.Join(c.LogDir, "cognitiveosd.log")
	c.AuditDir = filepath.Join(c.RunDir, "..", "audit")
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
