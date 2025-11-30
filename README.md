<p align="center">
  <img src="saltare.jpg" alt="Saltare" width="400"/>
</p>

<h1 align="center">Saltare</h1>

<p align="center">
  <strong>The AI Tool Mesh with Semantic Routing</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-1.0.0-blue.svg" alt="Version"/>
  <img src="https://img.shields.io/badge/license-AGPL%203.0-green.svg" alt="License"/>
  <img src="https://img.shields.io/badge/Go-1.23+-00ADD8.svg" alt="Go"/>
  <img src="https://img.shields.io/badge/Meilisearch-v1.12-ff5caa.svg" alt="Meilisearch"/>
</p>

---

## What is Saltare?

Saltare is a **production-grade AI tool orchestration platform** that connects your AI agents to any MCP-compatible tool through a single, intelligent gateway.

**Why Saltare?**
- 🚀 **Built for Speed** — Sub-second response times with intelligent LLM routing
- 🛡️ **Built for Reliability** — Multi-tier fallback ensures zero downtime
- 🎯 **Built for Simplicity** — Natural language queries, no complex configurations
- 🔍 **Built for Discovery** — Hybrid search (keyword + semantic) finds the right tools

Instead of managing dozens of tool integrations, point your AI to Saltare — it understands what you need and routes requests to the right tool automatically.

```
"What's the weather in Tokyo?" 
    → Saltare figures out you need weather.get_current 
    → Extracts {city: "Tokyo"} 
    → Returns the result
```

---

## ✨ Features

### 🧠 Semantic Routing with LLM Fallback
Natural language queries automatically matched to the right tool. No need to remember exact tool names or parameters — just describe what you want.

**Blazing Fast + Rock Solid Reliability:**
- **Primary:** Cerebras AI — industry-leading speed with <1s response time
- **Fallback:** OpenRouter with **your choice of model** — hundreds of options
- **Local Option:** Run your own Ollama models for complete control
- **Zero Downtime:** Intelligent multi-tier fallback ensures requests never fail

```
Primary LLM busy? → Instant automatic fallback
OpenRouter down? → Seamlessly switches to local Ollama
Your app stays running 24/7 ✅
```

### 🔍 Hybrid Search Engine (NEW!)
**Choose your search backend:**

| Engine | Best For | Features |
|--------|----------|----------|
| **Meilisearch** | Hybrid/Semantic Search | Vector search, AI embeddings, typo tolerance |
| **Typesense** | Fast Keyword Search | Instant results, faceting, filtering |

Both support auto-indexing when tools are registered!

### ⚡ Async Job Queue
Long-running operations? No problem. Built-in job queue with real-time SSE streaming, progress tracking, and graceful cancellation.

### 🔌 Universal Gateway
One endpoint, multiple protocols:
- **MCP Protocol** — Standard Model Context Protocol support
- **HTTP REST API** — For any HTTP client
- **CLI** — Command-line interface for scripts

### 📦 Tool Mesh Architecture
Aggregate tools from multiple MCP servers into a unified registry. Search, discover, and execute tools from anywhere.

### 🔒 Production Ready
- Embedded BadgerDB storage (zero external dependencies)
- Graceful shutdown with job persistence
- Prometheus metrics out of the box
- Health checks for Kubernetes
- **Kubernetes deployment manifests included**

---

## 🚀 Quick Start

### Install

```bash
git clone https://github.com/Denis-Chistyakov/Saltare.git
cd Saltare
go build -o saltare ./cmd/saltare
```

### Run with Meilisearch (Recommended)

```bash
# 1. Start Meilisearch
docker-compose -f docker/docker-compose.meilisearch.yml up -d

# 2. Set your LLM API keys
export CEREBRAS_API_KEY=your-cerebras-key
export OPENROUTER_API_KEY=your-openrouter-key

# 3. Start Saltare
./saltare server --config configs/saltare.yaml

# 4. Verify
curl http://localhost:8080/api/v1/health
```

### Try the Demo

```bash
# Terminal 1: Start services
docker-compose -f docker/docker-compose.meilisearch.yml up -d
./saltare server

# Terminal 2: Start mock server & run demo
cd tests/mock && go run weather_server.go &
cd ../../demo && ./demo.sh
```

The demo demonstrates:
- ✅ **Synchronous MCP calls** — Direct tool invocation
- ✅ **Smart queries** — Natural language in English, German, French
- ✅ **Intelligent routing** — Weather vs Math tool selection
- ✅ **Async jobs** — Background execution with SSE streaming

---

## 🔍 Search Engines

### Meilisearch (Default)

Meilisearch provides **hybrid search** combining keyword and semantic (vector) search:

```yaml
# configs/saltare.yaml
storage:
  search:
    provider: meilisearch
    meilisearch:
      enabled: true
      host: http://localhost:7700
      api_key: your-master-key
      index_name: tools
      
      # OpenRouter embeddings for semantic search
      embedder:
        source: rest
        url: https://openrouter.ai/api/v1/embeddings
        api_key: ${OPENROUTER_API_KEY}
        model: openai/text-embedding-3-small
        dimensions: 1536
      
      hybrid_search:
        enabled: true
        semantic_ratio: 0.5  # 0=keyword, 1=semantic, 0.5=balanced
```

```bash
# Start Meilisearch
docker-compose -f docker/docker-compose.meilisearch.yml up -d

# Verify
curl http://localhost:7700/health
```

### Typesense (Alternative)

For fast keyword-only search:

```yaml
storage:
  search:
    provider: typesense
  typesense:
    enabled: true
    nodes:
      - http://localhost:8108
    api_key: your-key
```

```bash
# Start Typesense
docker-compose -f docker/docker-compose.typesense.yml up -d
```

### Without External Search

Saltare works without any search backend — falls back to local keyword search:

```bash
./saltare server
```

---

## ⚙️ LLM Configuration

Saltare uses a **multi-tier LLM strategy** for maximum speed and reliability:

### Primary: Cerebras AI (Fastest)
```bash
export CEREBRAS_API_KEY=your-cerebras-key
```
- Sub-second response times
- Handles 99% of requests
- Auto-falls back if unavailable

### Fallback: OpenRouter (Flexible & Reliable)
```bash
export OPENROUTER_API_KEY=your-openrouter-key
```
- Seamlessly activates if primary LLM is unavailable
- **Also powers semantic search embeddings**
- Free and paid tiers available with hundreds of models
- Ensures your application never goes down

### Local: Ollama (Optional)
```bash
# If no API keys provided, uses local Ollama
ollama pull llama3.2:3b
```

**You only need ONE of these to get started.** The system automatically uses the best available option.

---

## 📚 API

### Tools

```bash
# List all tools
curl http://localhost:8080/api/v1/tools

# Search tools (uses Meilisearch/Typesense)
curl "http://localhost:8080/api/v1/tools?query=weather"

# Execute a tool
curl -X POST http://localhost:8080/api/v1/tools/weather.get_current/execute \
  -H "Content-Type: application/json" \
  -d '{"args": {"city": "Paris"}}'
```

### Async Jobs

```bash
# Create async job with natural language
curl -X POST http://localhost:8080/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"query": "Get weather in Berlin"}'

# Check job status
curl http://localhost:8080/api/v1/jobs/{job_id}

# Stream real-time updates (SSE)
curl -N http://localhost:8080/api/v1/jobs/{job_id}/stream

# Wait for completion
curl -X POST "http://localhost:8080/api/v1/jobs/{job_id}/wait?timeout=30"
```

### MCP Protocol

```bash
# Initialize session
curl -X POST http://localhost:8081/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize"}'

# List available tools
curl -X POST http://localhost:8081/mcp \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

# Smart query (natural language)
curl -X POST http://localhost:8081/mcp \
  -d '{
    "jsonrpc":"2.0",
    "id":3,
    "method":"call_tool",
    "params":{"query":"What is the weather in Tokyo?"}
  }'
```

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                           SALTARE                               │
├─────────────────────────────────────────────────────────────────┤
│  Gateway Layer                                                  │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐                         │
│  │   MCP   │  │  HTTP   │  │   CLI   │                         │
│  │ :8081   │  │ :8080   │  │         │                         │
│  └────┬────┘  └────┬────┘  └────┬────┘                         │
├───────┴────────────┴────────────┴───────────────────────────────┤
│  Semantic Router (with LLM Fallback)                            │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Intent Parser → Parameter Extraction → Tool Matching   │   │
│  │                                                          │   │
│  │  LLM Chain:                                              │   │
│  │  1️⃣ Cerebras AI (primary, <1s)                           │   │
│  │       ↓ (on failure)                                     │   │
│  │  2️⃣ OpenRouter (fallback, reliable)                      │   │
│  │       ↓ (if no API key)                                  │   │
│  │  3️⃣ Local Ollama (optional)                              │   │
│  └─────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│  Search Layer (Pluggable)                                       │
│  ┌─────────────────────────┐  ┌─────────────────────────────┐  │
│  │     Meilisearch         │  │        Typesense            │  │
│  │  (Hybrid: kw+semantic)  │  │    (Fast keyword search)    │  │
│  │  + OpenRouter Embeddings│  │                             │  │
│  └─────────────────────────┘  └─────────────────────────────┘  │
├─────────────────────────────────────────────────────────────────┤
│  Async Job Queue                                                │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐           │
│  │ Workers │  │  Queue  │  │ Storage │  │   SSE   │           │
│  └─────────┘  └─────────┘  └─────────┘  └─────────┘           │
├─────────────────────────────────────────────────────────────────┤
│  Execution Layer                                                │
│  ┌─────────────────────┐  ┌─────────────────────────────────┐  │
│  │     Code Mode       │  │         Direct Mode             │  │
│  │   (Goja Sandbox)    │  │      (MCP Client Pool)          │  │
│  └─────────────────────┘  └─────────────────────────────────┘  │
├─────────────────────────────────────────────────────────────────┤
│  Storage: BadgerDB (embedded)                                   │
└─────────────────────────────────────────────────────────────────┘
```

---

## ☸️ Kubernetes Deployment

Production-ready Kubernetes manifests are included:

```bash
# Deploy with Kustomize
kubectl apply -k deployments/kubernetes/

# Or manually
kubectl apply -f deployments/kubernetes/namespace.yaml
kubectl apply -f deployments/kubernetes/secrets.yaml
kubectl apply -f deployments/kubernetes/configmap.yaml
kubectl apply -f deployments/kubernetes/meilisearch.yaml
kubectl apply -f deployments/kubernetes/saltare-deployment.yaml
kubectl apply -f deployments/kubernetes/saltare-service.yaml
```

See [`deployments/kubernetes/README.md`](deployments/kubernetes/README.md) for full documentation.

---

## ⚙️ Configuration

Edit `configs/saltare.yaml`:

```yaml
server:
  host: 0.0.0.0
  port: 8080

mcp:
  http:
    enabled: true
    port: 8081

llm:
  primary:
    provider: cerebras
    api_key: ${CEREBRAS_API_KEY}
    model: llama-3.3-70b
  fallback:
    api_key: ${OPENROUTER_API_KEY}
    model: google/gemini-2.0-flash-exp:free

jobs:
  num_workers: 10
  queue_size: 1000
  job_timeout: 5m

storage:
  type: badger
  badger:
    path: ./data/badger
  
  # Search engine: "meilisearch" or "typesense"
  search:
    provider: meilisearch
    meilisearch:
      enabled: true
      host: http://localhost:7700
      api_key: your-master-key
      hybrid_search:
        enabled: true
        semantic_ratio: 0.5
```

---

## 🧪 Testing

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Meilisearch integration tests
MEILISEARCH_TEST=1 go test ./internal/storage/meilisearch/...

# Run demo
./demo/demo.sh
```

---

## 🤝 Contributing

We welcome contributions!

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing`)
5. Open a Pull Request

---

## 📄 License

GNU Affero General Public License v3.0 (AGPL-3.0) — see [LICENSE](LICENSE) for details.

### Commercial Licensing

For commercial licensing inquiries, please contact:
- **Twitter**: [@Chistyakov65590](https://x.com/Chistyakov65590)

---

<p align="center">
  <strong>Built for developers who want AI tools that just work.</strong>
</p>

<p align="center">
  ⭐ Star us on GitHub • Report Bug • Request Feature
</p>
