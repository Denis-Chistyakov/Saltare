# Saltare Demo

Demonstrates synchronous and asynchronous MCP calls with multilingual support.

## Quick Start

```bash
# 1. Build the project (from project root)
make build

# 2. Start mock weather MCP server (in terminal 1)
cd tests/mock && go run weather_server.go

# 3. Start Saltare server (in terminal 2, from project root)
./bin/saltare server

# 4. Run demo (in terminal 3)
cd demo && chmod +x demo.sh && ./demo.sh
```

> **Note**: The demo automatically registers the weather toolkit on first run, so it works for new users without pre-existing Typesense data.

## What the Demo Shows

1. **Synchronous Direct Call** - Direct tool invocation with explicit parameters
2. **Smart Query (English)** - "What is the weather in Paris?"
3. **Smart Query (German)** - "Wie ist das Wetter in Berlin?"
4. **Asynchronous Call** - Background job execution with French query
5. **List Tools** - Display available tools

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `http://localhost:8080` | HTTP API |
| `http://localhost:8081/mcp` | MCP JSON-RPC |

## Example Calls

### Sync Call
```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"call_tool","params":{"name":"get_current","arguments":{"city":"London"}}}'
```

### Smart Query
```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"call_tool","params":{"query":"What is the weather in Tokyo?"}}'
```

### Async Call
```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"call_tool","params":{"query":"weather in Berlin","async":true}}'
```

