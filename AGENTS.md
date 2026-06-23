# cognitiveosd — System Daemon

The background daemon that manages CognitiveOS lifecycle. Runs as PID 1 or a supervised system service.

## Responsibilities

- **5 System Codes**: wake, idle, security-shutdown, reset, unlock
- **Resource Audits**: monitors RAM, storage, CPU load; informs decision-making
- **MCP Supervisor**: launches, monitors, restarts MCP server processes from installed patches
- **Wide Model Lifecycle**: tracks which Wide Model is active, manages download/update flow
- **Raw Model Interface**: provides the system API that cli communicates with

## Build

```bash
go build -o bin/cognitiveosd ./cmd/cognitiveosd
```

## Communication

- Listens on a Unix socket at `/cognitiveos/run/daemon.sock`
- JSON messages for: input forwarding, system code triggers, audit results, status queries
- cli connects as a client; MCP servers register via the socket
