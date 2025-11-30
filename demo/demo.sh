#!/bin/bash
# Saltare Demo - Synchronous and Asynchronous MCP Calls
# Demonstrates multilingual smart queries via Cerebras LLM

set -e

MCP_URL="http://localhost:8081/mcp"
API_URL="http://localhost:8080/api/v1"

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║              🚀 SALTARE MCP DEMO                             ║"
echo "║         Sync & Async Calls + Multilingual Support            ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""

# Check if Saltare server is running
if ! curl -s "$API_URL/health" > /dev/null 2>&1; then
    echo "❌ Saltare server not running. Start with: ./bin/saltare server"
    exit 1
fi
echo "✅ Saltare server is running"

# Check if mock weather server is running
if ! curl -s "http://localhost:8082/health" > /dev/null 2>&1; then
    echo "❌ Mock weather server not running."
    echo "   Start with: cd tests/mock && go run weather_server.go"
    exit 1
fi
echo "✅ Mock weather server is running"
echo ""

# Check if weather toolkit is already registered
TOOLS_CHECK=$(curl -s "$API_URL/tools?query=get_current")
TOOL_EXISTS=$(echo "$TOOLS_CHECK" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    tools = data.get('tools', []) if isinstance(data, dict) else data
    exists = any(t.get('name') == 'get_current' for t in tools)
    print('yes' if exists else 'no')
except:
    print('no')
" 2>/dev/null)

if [ "$TOOL_EXISTS" = "yes" ]; then
    echo "✅ Weather toolkit already registered"
else
    echo "→ Registering weather toolkit..."
    REGISTER_RESULT=$(curl -s -X POST "$API_URL/toolkits" -H "Content-Type: application/json" \
        -d '{
            "name": "Weather API",
            "description": "Get weather information for cities",
            "version": "1.0.0",
            "toolboxes": [
                {
                    "name": "weather",
                    "version": "1.0.0",
                    "description": "Weather information tools",
                    "tools": [
                        {
                            "name": "get_current",
                            "description": "Get current weather for a city",
                            "tags": ["weather", "current", "forecast"],
                            "input_schema": {
                                "type": "object",
                                "properties": {
                                    "city": {
                                        "type": "string",
                                        "description": "City name"
                                    }
                                },
                                "required": ["city"]
                            },
                            "mcp_server": "http://localhost:8082/mcp"
                        }
                    ]
                }
            ]
        }')
    
    if echo "$REGISTER_RESULT" | grep -q '"success":true'; then
        echo "✅ Weather toolkit registered successfully"
    else
        echo "⚠️  Registration response: $REGISTER_RESULT"
    fi
fi

# Check if math toolkit is already registered
MATH_CHECK=$(curl -s "$API_URL/tools?query=add")
MATH_EXISTS=$(echo "$MATH_CHECK" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    tools = data.get('tools', []) if isinstance(data, dict) else data
    exists = any(t.get('name') == 'add' for t in tools)
    print('yes' if exists else 'no')
except:
    print('no')
" 2>/dev/null)

if [ "$MATH_EXISTS" = "yes" ]; then
    echo "✅ Math toolkit already registered"
else
    echo "→ Registering math toolkit..."
    MATH_RESULT=$(curl -s -X POST "$API_URL/toolkits" -H "Content-Type: application/json" \
        -d '{
            "name": "Math API",
            "description": "Mathematical operations",
            "version": "1.0.0",
            "toolboxes": [
                {
                    "name": "math",
                    "version": "1.0.0",
                    "description": "Basic math operations",
                    "tools": [
                        {
                            "name": "add",
                            "description": "Add two numbers together",
                            "tags": ["math", "calculator", "addition", "sum"],
                            "input_schema": {
                                "type": "object",
                                "properties": {
                                    "a": {
                                        "type": "number",
                                        "description": "First number"
                                    },
                                    "b": {
                                        "type": "number",
                                        "description": "Second number"
                                    }
                                },
                                "required": ["a", "b"]
                            },
                            "mcp_server": "http://localhost:8082/mcp"
                        }
                    ]
                }
            ]
        }')
    
    if echo "$MATH_RESULT" | grep -q '"success":true'; then
        echo "✅ Math toolkit registered successfully"
    else
        echo "⚠️  Math registration response: $MATH_RESULT"
    fi
fi
echo ""

# Initialize MCP session
curl -s -X POST "$MCP_URL" -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":0,"method":"initialize"}' > /dev/null

# ═══════════════════════════════════════════════════════════════════
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 DEMO 1: Synchronous MCP Call (Direct Tool)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "→ Calling tool 'get_current' with city='London'"
echo ""

SYNC_RESULT=$(curl -s -X POST "$MCP_URL" -H "Content-Type: application/json" \
    -d '{
        "jsonrpc":"2.0",
        "id":1,
        "method":"call_tool",
        "params":{
            "name":"get_current",
            "arguments":{"city":"London"}
        }
    }')

echo "$SYNC_RESULT" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    r = data['result']
    print(f\"✅ Tool: {r.get('tool_used', 'N/A')}\")
    if 'content' in r and r['content']:
        print(f\"   Result: {r['content'][0].get('text', '')[:200]}\")
elif 'error' in data:
    print(f\"❌ Error: {data['error']['message']}\")
"
echo ""

# ═══════════════════════════════════════════════════════════════════
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 DEMO 2: Smart Query (English)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "→ Query: 'What is the weather in Paris?'"
echo "  (Cerebras LLM extracts intent and parameters)"
echo ""

SMART_EN=$(curl -s -X POST "$MCP_URL" -H "Content-Type: application/json" \
    -d '{
        "jsonrpc":"2.0",
        "id":2,
        "method":"call_tool",
        "params":{"query":"What is the weather in Paris?"}
    }')

echo "$SMART_EN" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    r = data['result']
    print(f\"✅ Tool: {r.get('tool_used', 'N/A')}\")
    if 'content' in r and r['content']:
        print(f\"   Result: {r['content'][0].get('text', '')[:200]}\")
elif 'error' in data:
    print(f\"❌ Error: {data['error']['message']}\")
"
echo ""

# ═══════════════════════════════════════════════════════════════════
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 DEMO 3: Smart Routing - Math Query"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "→ Query: 'What is 2 plus 2?'"
echo "  (LLM intelligently routes to math.add instead of weather)"
echo ""

SMART_MATH=$(curl -s -X POST "$MCP_URL" -H "Content-Type: application/json" \
    -d '{
        "jsonrpc":"2.0",
        "id":3,
        "method":"call_tool",
        "params":{"query":"What is 2 plus 2?"}
    }')

echo "$SMART_MATH" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    r = data['result']
    print(f\"✅ Tool: {r.get('tool_used', 'N/A')}\")
    if 'content' in r and r['content']:
        print(f\"   Result: {r['content'][0].get('text', '')}\")
elif 'error' in data:
    print(f\"❌ Error: {data['error']['message']}\")
"
echo ""

# ═══════════════════════════════════════════════════════════════════
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 DEMO 4: Smart Query (German)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "→ Query: 'Wie ist das Wetter in Berlin?'"
echo "  (LLM understands German and extracts city='Berlin')"
echo ""

SMART_DE=$(curl -s -X POST "$MCP_URL" -H "Content-Type: application/json" \
    -d '{
        "jsonrpc":"2.0",
        "id":4,
        "method":"call_tool",
        "params":{"query":"Wie ist das Wetter in Berlin?"}
    }')

echo "$SMART_DE" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    r = data['result']
    print(f\"✅ Tool: {r.get('tool_used', 'N/A')}\")
    if 'content' in r and r['content']:
        print(f\"   Result: {r['content'][0].get('text', '')[:200]}\")
elif 'error' in data:
    print(f\"❌ Error: {data['error']['message']}\")
"
echo ""

# ═══════════════════════════════════════════════════════════════════
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 DEMO 5: Asynchronous MCP Call"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "→ Submitting async job for: 'Quel temps fait-il à Tokyo?'"
echo ""

ASYNC_RESULT=$(curl -s -X POST "$MCP_URL" -H "Content-Type: application/json" \
    -d '{
        "jsonrpc":"2.0",
        "id":5,
        "method":"call_tool",
        "params":{
            "query":"Quel temps fait-il à Tokyo?",
            "async":true
        }
    }')

JOB_ID=$(echo "$ASYNC_RESULT" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    r = data['result']
    if 'job' in r and 'id' in r['job']:
        print(r['job']['id'])
")

if [ -n "$JOB_ID" ]; then
    echo "✅ Job created: $JOB_ID"
    echo "$ASYNC_RESULT" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    r = data['result']
    job = r.get('job', {})
    print(f\"   Status: {job.get('status', 'N/A')}\")
    print(f\"   Tool: {job.get('tool', 'N/A')}\")
"
    echo ""
    echo "→ Waiting for job to complete..."
    sleep 3
    
    # Get job result
    JOB_RESULT=$(curl -s -X POST "$MCP_URL" -H "Content-Type: application/json" \
        -d "{\"jsonrpc\":\"2.0\",\"id\":6,\"method\":\"get_job\",\"params\":{\"job_id\":\"$JOB_ID\"}}")
    
    echo "$JOB_RESULT" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if 'result' in data:
    r = data['result']
    job = r.get('job', {})
    print(f\"✅ Job completed!\")
    print(f\"   Status: {job.get('status', 'N/A')}\")
    print(f\"   Tool: {job.get('tool', 'N/A')}\")
    if job.get('result'):
        res = job['result']
        if 'content' in res and res['content']:
            text = res['content'][0].get('text', '')[:150]
            print(f\"   Result: {text}...\")
elif 'error' in data:
    print(f\"❌ Error: {data['error']['message']}\")
"
else
    echo "❌ Failed to submit async job"
    echo "$ASYNC_RESULT"
fi
echo ""

# ═══════════════════════════════════════════════════════════════════
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📌 DEMO 6: List Available Tools"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

curl -s "$API_URL/tools" | python3 -c "
import sys, json
data = json.load(sys.stdin)
tools = data.get('tools', []) if isinstance(data, dict) else data
print(f'Available tools ({len(tools)}):')
for t in tools:
    desc = t.get('description', '')[:40]
    print(f\"  • {t.get('name', 'N/A')}: {desc}\")
"
echo ""

# ═══════════════════════════════════════════════════════════════════
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║                    ✅ DEMO COMPLETE                          ║"
echo "║                                                              ║"
echo "║  Demonstrated:                                               ║"
echo "║  • Synchronous direct tool call                              ║"
echo "║  • Smart queries with LLM (EN, DE, FR)                       ║"
echo "║  • Intelligent tool routing (weather vs math)                ║"
echo "║  • Asynchronous job execution                                ║"
echo "╚══════════════════════════════════════════════════════════════╝"

