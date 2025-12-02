#!/bin/bash
# Demo: Test Saltare MCP Proxy
# Shows how Saltare aggregates tools from multiple MCP backends

set -e

# ============================================================================
# CONFIGURATION
# ============================================================================
# Set these environment variables before running, or modify defaults below:
#
#   NPX_PATH      - Path to npx binary (default: auto-detect via 'which npx')
#   SALTARE_ROOT  - Path to Saltare project root (default: script's parent dir)
#
# Example:
#   export NPX_PATH=/usr/local/bin/npx
#   export SALTARE_ROOT=/path/to/Saltare
#   ./demo_stdio_proxy.sh
# ============================================================================

# Auto-detect paths if not set
NPX_PATH="${NPX_PATH:-$(which npx 2>/dev/null || echo "npx")}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SALTARE_ROOT="${SALTARE_ROOT:-$(dirname "$SCRIPT_DIR")}"

# Validate npx exists
if ! command -v "$NPX_PATH" &> /dev/null; then
    echo "❌ Error: npx not found at '$NPX_PATH'"
    echo ""
    echo "Please install Node.js or set NPX_PATH environment variable:"
    echo "  export NPX_PATH=/path/to/npx"
    exit 1
fi

cd "$SALTARE_ROOT"

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║           🚀 SALTARE MCP PROXY DEMO                          ║"
echo "║        Single MCP → Multiple Backends                        ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""
echo "Configuration:"
echo "  NPX_PATH:     $NPX_PATH"
echo "  SALTARE_ROOT: $SALTARE_ROOT"
echo ""

# Build if needed
if [ ! -f bin/saltare-mcp ]; then
    echo "→ Building saltare-mcp..."
    go build -o bin/saltare-mcp ./cmd/saltare-mcp
fi

# Configure minimal backends for demo
# Format: name|transport|command|arg1|arg2|...
export SALTARE_BACKENDS="
memory|stdio|${NPX_PATH}|-y|@modelcontextprotocol/server-memory
"

echo "→ Starting Saltare MCP Proxy with memory backend..."
echo ""

# Create a temp file for communication
FIFO_IN=$(mktemp -u)
FIFO_OUT=$(mktemp -u)
mkfifo "$FIFO_IN"
mkfifo "$FIFO_OUT"

# Start proxy in background
./bin/saltare-mcp < "$FIFO_IN" > "$FIFO_OUT" 2>/dev/null &
PROXY_PID=$!

# Open file descriptors
exec 3>"$FIFO_IN"
exec 4<"$FIFO_OUT"

# Give it time to start
sleep 2

# Function to send request and get response
send_request() {
    echo "$1" >&3
    read -t 10 response <&4
    echo "$response"
}

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 Step 1: Initialize"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

INIT_RESP=$(send_request '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}')
echo "$INIT_RESP"
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 Step 2: List Tools (aggregated from all backends)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

TOOLS_RESP=$(send_request '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}')
echo "$TOOLS_RESP" | python3 -c "
import sys,json
try:
    d=json.load(sys.stdin.buffer)
    tools = d.get('result',{}).get('tools',[])
    print(f'Found {len(tools)} tools:')
    for t in tools:
        print(f\"  • {t.get('name')}: {t.get('description','')[:50]}...\")
except:
    print('Error parsing response')
" 2>/dev/null || echo "$TOOLS_RESP"
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 Step 3: Call memory tool (create entity)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

CALL_RESP=$(send_request '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_create_entities","arguments":{"entities":[{"name":"Saltare","entityType":"Project","observations":["AI Tool Mesh with Semantic Routing","Supports HTTP and Stdio MCP transports"]}]}}}')
echo "Response: $CALL_RESP" | head -c 500
echo ""
echo ""

# Cleanup
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Demo complete! Cleaning up..."

exec 3>&-
exec 4<&-
kill $PROXY_PID 2>/dev/null || true
rm -f "$FIFO_IN" "$FIFO_OUT"

echo ""
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║  To use Saltare as your single MCP server in Cursor:         ║"
echo "║                                                              ║"
echo "║  1. Add to ~/.cursor/mcp.json:                               ║"
echo "║     {                                                        ║"
echo "║       \"mcpServers\": {                                        ║"
echo "║         \"saltare\": {                                         ║"
echo "║           \"command\": \"$SALTARE_ROOT/bin/saltare-mcp\",        ║"
echo "║           \"env\": {                                           ║"
echo "║             \"SALTARE_BACKENDS\": \"memory|stdio|npx|...\"       ║"
echo "║           }                                                  ║"
echo "║         }                                                    ║"
echo "║       }                                                      ║"
echo "║     }                                                        ║"
echo "║                                                              ║"
echo "║  2. Restart Cursor                                           ║"
echo "║                                                              ║"
echo "║  All your tools will be available through Saltare! 🎉        ║"
echo "╚══════════════════════════════════════════════════════════════╝"
