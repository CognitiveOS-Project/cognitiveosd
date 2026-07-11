# cognitiveosd — System Daemon

Background daemon that manages CognitiveOS lifecycle. Runs as PID 1 or supervised system service.

## Architecture

```
cmd/cognitiveosd/         — Entry point, flag/env parsing
internal/daemon/
  daemon.go               — Core: state machine, startup/shutdown, message dispatch
  socket.go               — Unix socket listener at /cognitiveos/run/daemon.sock
  types.go                — All message envelope types (18+ JSON message types)
  handlers.go             — Message dispatch for all 12 socket message types
  mcp_lifecycle.go        — MCP server process lifecycle (spawn, discover, invoke, kill)
  wide_client.go          — HTTP client to coginfer (api/generate, api/pull, api/delete)
  audit.go                — Hardware audit loop (/proc/meminfo, statfs, /sys/class/net)
internal/config/          — Configuration with env override support
```

## Message Protocol

JSON-over-Unix socket at `/cognitiveos/run/daemon.sock`. Newline-delimited JSON envelopes.

| Type | Direction | Purpose |
|------|-----------|---------|
| input_forward | cli → daemon | Forward human input to Wide Model |
| output_deliver | daemon → cli | Wide Model response |
| system_code | cli → daemon | wake/idle/security/reset/unlock |
| mcp_register | MCP → daemon | Tool capability announcement |
| mcp_invoke | daemon → MCP | Tool execution request |
| mcp_result | MCP → daemon | Tool execution result |
| audit_request | cli → daemon | Trigger hardware audit |
| audit_report | daemon → cli | Resource snapshot |
| status_request | cli → daemon | Daemon status |
| wide_model_load | daemon → inference | Load Wide Model |
| wide_model_unload | daemon → inference | Unload Wide Model |

## System Codes

- **wake**: Transition from idle to listening
- **idle**: Unload Wide Model, suspend MCP servers
- **security**: Kill all processes, power off peripherals
- **reset**: Wipe data partitions, reboot
- **unlock**: Validate unlock code

## Build

```bash
make build    # compile to build/bin/cognitiveosd
make test     # run tests
make lint     # go vet
make clean    # remove build artifacts
```

## Configuration

Environment variables / flags:

| Env | Flag | Default |
|-----|------|---------|
| COGNITIVEOS_SOCKET | --socket | /cognitiveos/run/daemon.sock |
| COGNITIVEOS_MODEL_DIR | --models | /cognitiveos/models |
| COGNITIVEOS_PATCH_DIR | --patches | /cognitiveos/patches |
| COGNITIVEOS_RUN_DIR | --run | /cognitiveos/run |
| COGNITIVEOS_LOG_DIR | --log-dir | /cognitiveos/logs |
| COGNITIVEOS_INFERENCE_URL | --inference | http://127.0.0.1:11434 |
| COGNITIVEOS_MCP_BIN_DIR | --mcp-bin | /cognitiveos/bin |
| | --audit-interval | 60 |

## Cloning Convention
- Use SSH () for development.
- Use HTTPS () for build scripts that clone public dependencies.
## Cloning Convention
- Use SSH (git@github.com:) for development.
- Use HTTPS (https://github.com/) for build scripts that clone public dependencies.
