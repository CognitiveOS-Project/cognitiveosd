# cognitiveosd

The CognitiveOS background system daemon — manages the 5 system codes (wake/idle/shutdown/reset/unlock), resource audits, MCP process supervision, and Wide Model lifecycle.

## System Codes

| Code | Effect |
|------|--------|
| **wake** | Transition from idle to listening |
| **idle** | Unload Wide Model, suspend MCP servers |
| **security** | Kill all processes, power off peripherals |
| **reset** | Wipe data partitions, reboot |
| **unlock** | Validate unlock code |

## Socket API

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

## Build

```bash
go build -o bin/cognitiveosd ./cmd/cognitiveosd
```

## Configuration

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

## Author

**Jean Machuca** — [GitHub](https://github.com/jeanmachuca) · [Sponsor](https://github.com/sponsors/jeanmachuca)

## License

MIT
