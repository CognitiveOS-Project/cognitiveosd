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
make build
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

## Related

- [CognitiveOS](https://github.com/CognitiveOS-Project/cognitiveos) — main project repository
- [cognitive-os.org](https://cognitive-os.org) — project website
- [cli](https://github.com/CognitiveOS-Project/cli) — TUI frontend that connects to this daemon
- [inference](https://github.com/CognitiveOS-Project/inference) — LLM engine managed by this daemon
- [core-mcp-bridges](https://github.com/CognitiveOS-Project/core-mcp-bridges) — MCP servers supervised by this daemon
- [coginit](https://github.com/CognitiveOS-Project/coginit) — boot manager that starts this daemon
- [Product Specs](https://github.com/CognitiveOS-Project/product-specs) — daemon API specification
- [CognitiveOS Project](https://github.com/CognitiveOS-Project) — GitHub organization

## Build

```bash
make build    # Compile to build/bin/cognitiveosd
make test     # Run tests
make lint     # Run go vet
make clean    # Remove build artifacts
```

## Contributing

1. Branch from `main`
2. Use topic branches: `feature/<name>`, `fix/<name>`
3. Open a PR to `main` with a clear title and description
4. Merge after review

See the [SDLC repo](https://github.com/CognitiveOS-Project/sdlc) for the full contribution guide, code review standards, and testing strategy.

## Author

**Jean Machuca** — [GitHub](https://github.com/jeanmachuca) · [Sponsor](https://github.com/sponsors/jeanmachuca)

## License

MIT
