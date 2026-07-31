# Friday — AI Desktop Assistant

Single-binary AI assistant with native desktop window, 55 tools, real-time trading, and self-healing. Runs on Windows. No browser needed.

## Quick Start

Double-click **`Friday.lnk`** on your desktop. Or:

```
cd D:\Friday - Prototype\go
.\friday.exe
```

On first launch, Friday opens a native 1200x800 window with the chat UI and "9 services online" status.

## Architecture

```
┌────────────────────────────────────────────────────┐
│                   friday.exe                        │
│                                                     │
│  ┌──────────┐  ┌───────────────┐  ┌─────────────┐  │
│  │ WebView2  │  │ Gin HTTP      │  │ Trading     │  │
│  │ Desktop   │  │ Server :8000  │  │ Engine      │  │
│  │ Window    │  │               │  │ Goroutine   │  │
│  └──────────┘  │ • Chat SSE    │  │ :8001       │  │
│                │ • Tools API   │  └─────────────┘  │
│  ┌──────────┐  │ • Web UI      │  ┌─────────────┐  │
│  │ Inline   │  │ • Services    │  │ Self-Heal   │  │
│  │ Bridge   │  │ • Compounder  │  │ Watchdog    │  │
│  │ (no CGo) │  └───────────────┘  └─────────────┘  │
│  └──────────┘                                       │
└────────────────────────────────────────────────────┘
```

**No separate processes.** Everything runs inside a single `friday.exe`:
- **HTTP server** on `:8000` (Gin) — chat, tools, web UI, status
- **LLM Bridge** — inline handler at `/v1/chat/completions` → DeepSeek-V3 via DeepInfra
- **Trading Engine** — goroutine on `:8001` (MT5, crypto, risk)
- **WebView2** — native Windows desktop window (no CGo, pure Win32 API)
- **Self-healer** — background goroutine, auto-restarts on 3 consecutive failures

## System Services

| Name | Role | What It Monitors |
|------|------|-----------------|
| Vayu | Router | 55 tools indexed, selectTools active |
| Neo | Reasoner | DeepSeek-V3 LLM bridge health |
| Forge | Executor | Tool registry (55 tools ready) |
| Scout | Network | External API reachability (httpstat.us) |
| Verdict | Verifier | Validation & quality assurance |
| Prism | Monitor | Self-healing healer status |
| Oracle | Memory | Companion state (message history) |
| Titan | Trading | MT5 engine health on :8001 |
| Sentinel | Guardian | API key config, rate limiting |

Live status at `/status` and `/workers/status`.

## 55 Tools (All Real, No Stubs)

Tools are selected dynamically (~12 per query based on keywords). Each tool has a real JSON schema telling the AI exactly what parameters it needs.

| Category | Tools |
|----------|-------|
| **Core** | calc, system_info, get_time, manage_files, run_terminal, run_code, ip_info, weather, ping, word_count |
| **Search** | web_search, web_fetch, wikipedia, brave_search, mojeek_search, search (aggregator), parallel_search |
| **Crypto** | crypto_price, crypto_portfolio, crypto_grid, market_regime, crypto_backtest, crypto_trade |
| **Trading** | trading_status, get_market_data, get_account_info, get_positions, get_orders, execute_trade, calculate_risk, backtest, multi_tf_analysis, momentum_analysis, kelly_sizer, strategy_optimizer |
| **Bots** | exness_bot, blue_guardian_bot, miner_bot, resource_manager, compounder |
| **Memory** | remember_fact, recall_facts, friday_capabilities |
| **Utility** | json_tool, hash, encode, random, call_help |
| **AI** | call_model, vision_analyze, add_skill, list_skills |
| **System** | self_healer |

## Chat & UI

- **Typewriter effect** — responses stream character-by-character at 5ms/batch
- **Multi-message split** — paragraphs become separate message bubbles, 400ms apart
- **Click to skip** — click any message to instantly show full text
- **Thinking indicator** — shows "Neo thinking..." while the AI processes
- **Tool activity feed** — shows each tool call as it happens
- **Control Center** — `/control-center` for dashboards, workers, bots, trading, logs

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/status` | System status (online/offline, services, uptime) |
| GET | `/health` | Health check (LLM bridge, server) |
| GET | `/team` | Service list with live status |
| POST | `/command/stream` | SSE chat streaming |
| POST | `/tools/execute` | Direct tool execution |
| GET | `/workers/status` | 9 services with details |
| POST | `/v1/chat/completions` | LLM bridge (OpenAI-compatible) |
| GET | `/` | Web UI (index.html) |
| GET | `/static/*` | Static assets (CSS, JS, images) |

## Desktop Launchers

| Shortcut | What It Does |
|----------|-------------|
| `Friday.lnk` | Launch app directly (double-click, native window) |
| `Friday Launcher.lnk` | PowerShell console with management commands |

**Launcher commands:**
- `.\friday.ps1 start` — launch Friday
- `.\friday.ps1 stop` — stop gracefully
- `.\friday.ps1 status` — check if running + live service health
- `.\friday.ps1 logs` — view last 20 log lines
- `.\friday.ps1 restart` — full restart

Also: `D:\Friday - Prototype\launch_friday.cmd` (simple batch launcher).

## Project Structure

```
D:\Friday - Prototype\
├── go\
│   ├── friday\           # Core package (server, tools, orchestrator, services)
│   │   ├── server.go           # HTTP server, handlers, SSE
│   │   ├── tools.go            # 55 tool implementations (~3000 lines)
│   │   ├── orchestrator_simple.go  # AI orchestration, selectTools, naturalize
│   │   ├── services.go         # 9 system services with live health checks
│   │   ├── compounder.go       # Capital management with file persistence
│   │   ├── resource.go         # CPU/RAM/disk monitoring
│   │   ├── companion.go        # Memory, user state, capabilities
│   │   ├── healer.go           # Self-healing, auto-repair
│   │   ├── upgrader.go         # Auto-upgrade system
│   │   ├── types.go            # Core types (StreamEvent, Message, etc.)
│   │   └── trading/            # MT5 trading engine
│   ├── cmd/friday/main.go      # Unified entry point
│   ├── webui/                  # Frontend (HTML, CSS, JS)
│   │   ├── index.html          # Main chat UI
│   │   ├── cc.html             # Control Center
│   │   ├── app.js              # Chat logic, typewriter, multi-message
│   │   ├── cc.js               # Control Center logic
│   │   └── style.css           # Styles
│   ├── data/                   # Runtime data (compounder, memory, state)
│   ├── friday.exe              # Built binary (~33MB)
│   ├── friday.ps1              # PowerShell management script
│   └── go.mod
├── interface\                  # Legacy web UI (duplicate of webui/)
├── launch_friday.cmd           # Batch launcher
└── README_SAGAR.md             # This file
```

## Configuration

Set via `.env` file or environment variables:

```
API_KEY=HPX9yGtaX0dAQbKVRnla6XytLBil8ZFY
API_MODEL=deepseek-ai/DeepSeek-V3
API_URL=https://api.deepinfra.com/v1/openai
```

## Credentials

- **DeepInfra API** — `HPX9yGtaX0dAQbKVRnla6XytLBil8ZFY` (primary AI provider)
- **Exness MT5** — login `167036042` / `Exness-MT5Real3`
- **Blue Guardian** — login `503985` / `BlueGuardian-Server` / $5k Instant Starter
