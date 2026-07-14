package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/CognitiveOS-Project/cognitiveosd/internal/config"
	"github.com/CognitiveOS-Project/cognitiveosd/internal/daemon"
)

func main() {
	cfg := config.FromEnv("/etc/cognitiveos/config.toml")

	var socketPath string
	flag.StringVar(&socketPath, "socket", cfg.SocketPath, "Unix socket path")
	flag.StringVar(&cfg.RunDir, "run", cfg.RunDir, "runtime directory")
	flag.StringVar(&cfg.LogDir, "log-dir", cfg.LogDir, "log directory")
	flag.StringVar(&cfg.ModelDir, "models", cfg.ModelDir, "model directory")
	flag.StringVar(&cfg.PatchDir, "patches", cfg.PatchDir, "patch directory")
	flag.StringVar(&cfg.MCPBinDir, "mcp-bin", cfg.MCPBinDir, "MCP server binary directory")
	flag.StringVar(&cfg.InferenceURL, "inference", cfg.InferenceURL, "inference engine URL")
	flag.IntVar(&cfg.AuditInterval, "audit-interval", cfg.AuditInterval, "audit interval in seconds")
	flag.Parse()

	flag.Visit(func(f *flag.Flag) {
		if f.Name == "socket" {
			cfg.SocketPath = socketPath
			cfg.SocketPathExplicit = true
		}
	})

	cfg.Derive()

	d := daemon.New(cfg)
	logger := log.New(os.Stdout, "cognitiveosd: ", log.LstdFlags)

	if err := d.Run(); err != nil {
		logger.Fatal(fmt.Errorf("fatal: %w", err))
	}
}
