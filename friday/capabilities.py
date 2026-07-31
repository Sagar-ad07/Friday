"""
Friday Capability Registry
Every tool, skill, and feature Friday has — self-described so the model
knows exactly what it can do and when to use each capability.
"""
import json
import os
import threading
from typing import Dict, List

_lock = threading.Lock()

CAPABILITIES = [
    {
        "id": "chat",
        "name": "Conversation",
        "description": "Natural chat, casual conversation, emotional support, personality-driven replies",
        "when_to_use": "User greets, chats casually, vents, shares thoughts, says thanks/bye",
        "tools_needed": [],
        "type": "builtin",
    },
    {
        "id": "web_search",
        "name": "Web Search & News",
        "description": "Search the internet, get latest news, look up facts, find websites, research topics",
        "when_to_use": "User asks about current events, wants to find information, check news, research anything",
        "tools_needed": ["web_search"],
        "type": "tool",
    },
    {
        "id": "code_execution",
        "name": "Run Code & Scripts",
        "description": "Write and execute Python code, run terminal commands, execute scripts",
        "when_to_use": "User wants calculations, data processing, automation, running scripts, testing code",
        "tools_needed": ["run_terminal", "run_code"],
        "type": "tool",
    },
    {
        "id": "file_management",
        "name": "File Management",
        "description": "Read, write, create, delete, list, rename files and directories",
        "when_to_use": "User wants to read/create/edit/delete files, manage project structure, view code",
        "tools_needed": ["manage_files"],
        "type": "tool",
    },
    {
        "id": "math_calculation",
        "name": "Math & Calculations",
        "description": "Perform arithmetic, algebraic, and mathematical computations instantly",
        "when_to_use": "User asks about math, wants to calculate something, needs numeric results",
        "tools_needed": ["calc"],
        "type": "builtin",
    },
    {
        "id": "time_date",
        "name": "Time & Date",
        "description": "Tell current time, date, day of week — zero latency, no API call",
        "when_to_use": "User asks what time/date/day it is",
        "tools_needed": [],
        "type": "builtin",
    },
    {
        "id": "memory",
        "name": "Memory & Recall",
        "description": "Remember facts about the user, recall past conversations, remember preferences, learn from interactions",
        "when_to_use": "User mentions something they told you before, asks 'do you remember', wants personalized responses",
        "tools_needed": [],
        "type": "builtin",
    },
    {
        "id": "voice",
        "name": "Voice & Speech",
        "description": "Speak responses aloud using natural Indian English voice, transcribe spoken input",
        "when_to_use": "User prefers voice interaction, asks Friday to speak, uses mic",
        "tools_needed": ["voice_synthesize", "voice_transcribe"],
        "type": "feature",
    },
    {
        "id": "screen_vision",
        "name": "Screen Vision & Eye",
        "description": "See what's on the user's screen, take screenshots, describe what's happening visually",
        "when_to_use": "User asks 'what's on my screen', wants help navigating an app, needs visual assistance",
        "tools_needed": ["screen_capture", "screen_describe"],
        "type": "feature",
    },
    {
        "id": "desktop_control",
        "name": "Desktop Control",
        "description": "Open applications, control mouse/keyboard, manage windows, automate desktop tasks",
        "when_to_use": "User wants to open an app, automate clicks, control PC remotely",
        "tools_needed": ["open_app", "desktop_control"],
        "type": "tool",
    },
    {
        "id": "trading",
        "name": "MT5 Live Trading",
        "description": "Full MT5 trading via Go bridge (port 8001). Exness account 167036042 (30.50 AED, 1:200 leverage, hedge mode). Uses EURUSDm symbols (Exness suffix 'm'). London ORB strategy: 1% risk, 1:2 RR, max 2 trades/day, 15-80 pip range filter, London session only (7-12 UTC), checks every 120s. Bridge endpoints: /health, /account, /tick/{symbol}, /rates/{symbol}/{tf}/{count}, /symbols (355 symbols), /positions, /order, /login. Requires AutoTrading play button (green ▶) enabled in MT5. Pip value: ~0.1 USD per 0.01 lot EURUSDm. Min lot 0.01, max 200. Balance > 0 = live trading; balance = 0 = paper simulation.",
        "when_to_use": "User asks about trading, markets, bot status, wants to start/stop trading, check account balance, view positions, asks about Exness/MT5",
        "tools_needed": ["trading_start", "trading_stop", "trading_status", "trading_bridge"],
        "type": "feature",
    },
    {
        "id": "learning",
        "name": "Self-Improvement & Upgrader",
        "description": "Self-modify code via the upgrader pipeline: propose, build, test, review, approve, and apply upgrades. Multi-file changes via Architect. Rollback support. Only applies after tests pass.",
        "when_to_use": "User asks Friday to improve itself, fix bugs, add features, upgrade capabilities",
        "tools_needed": ["upgrade_propose", "upgrade_apply", "upgrade_rollback"],
        "type": "feature",
    },
    {
        "id": "improver_agent",
        "name": "Improver Agent (Autonomous)",
        "description": "Background sub-agent that autonomously scans the codebase for issues (secrets, syntax errors, bare excepts, TODOs, dead code), calls Oracle (planner) + Titan (builder) + Sentinel (reviewer) sub-agents to propose upgrades via the upgrader pipeline, and learns from each cycle. Runs every 6 hours. Never auto-applies — always requires explicit approval.",
        "when_to_use": "User asks Friday to make itself better automatically, 'improve yourself', 'find bugs', 'scan for issues', runs continuously in background",
        "tools_needed": ["improver_status", "improver_scan", "improver_start", "improver_stop"],
        "type": "feature",
    },
    {
        "id": "research_deep",
        "name": "Deep Research",
        "description": "Autonomous deep research on any topic: finds sources, analyzes, generates comprehensive reports",
        "when_to_use": "User wants thorough research on a topic, deep dive analysis, comprehensive study",
        "tools_needed": ["research_run"],
        "type": "feature",
    },
    {
        "id": "planning",
        "name": "Task Planning & DAG",
        "description": "Break complex tasks into parallel steps, execute in optimal order, recover from failures",
        "when_to_use": "User gives a multi-step or complex request that needs orchestration",
        "tools_needed": ["plan_and_execute"],
        "type": "tool",
    },
    {
        "id": "system_info",
        "name": "System Information",
        "description": "Check CPU, RAM, disk usage, system health, running processes",
        "when_to_use": "User asks about computer performance, system health, resource usage",
        "tools_needed": ["system_info"],
        "type": "tool",
    },
    {
        "id": "earning_bots",
        "name": "Earning Bots & Automation",
        "description": "Create, manage, and monitor automated bots. 3 monitor types: price_alerts (MT5/crypto price watching, 120s), news_alerts (web news search, 300s), opportunity_scanner (money-making ideas, 600s). 6 strategies: crypto arbitrage, affiliate marketing, content monetization, trading bot, freelance automation. Dashboard tracks earnings by source with daily summaries.",
        "when_to_use": "User wants to set up recurring tasks, earning automation, bot management, scheduled jobs, monitor prices, scan for opportunities",
        "tools_needed": ["bot_create", "bot_list", "bot_stop", "bot_status", "earnings_dashboard"],
        "type": "feature",
    },
    {
        "id": "mt5_bridge",
        "name": "MT5 Go Bridge",
        "description": "Go REST bridge on localhost:8001 connecting to MetaTrader 5 terminal (build 6036, Exness Technologies Ltd). Communicates via named pipe IPC. Auto-reconnects every 5s. Endpoints: GET /health (build + connected), GET /account (login, balance, equity, currency, leverage), GET /tick/{symbol} (bid/ask with auto-select), GET /rates/{symbol}/{tf}/{count} (CopyRatesFromPos), GET /symbols (355 symbols with trade info), GET /positions (open positions), POST /order (market order with SL/TP, IOC filling), POST /login (switch accounts). Built with go-mt5 library (github.com/mukbeast4/go-mt5).",
        "when_to_use": "User needs MT5 account info, wants to check bridge health, needs real-time prices, wants to place/modify trades programmatically",
        "tools_needed": ["bridge_health", "bridge_account", "bridge_tick", "bridge_order"],
        "type": "infrastructure",
    },
    {
        "id": "exness_bot",
        "name": "Exness Auto-Trader Bot",
        "description": "Python bot (trading/exness_bot.py) running London ORB breakout strategy. Monitors EURUSDm on M5 timeframe every 120s during London session (7-12 UTC). Calculates 1-hour range from 12 M5 candles (15-80 pip filter, spread < 25 pips). Breakout rules: bid > high = buy at ask with SL at 1.2x range, TP at 2.4x range; ask < low = sell at bid with SL 1.2x range, TP 2.4x range. Risk: 1% per trade (min 0.1). Volume = max(0.01, min(risk / (sl_pips * 0.1) * 0.1, 0.5)). Max 2 trades/day. Paper-trades when balance=0; live when balance>0. State persisted to exness_bot_state.json.",
        "when_to_use": "User asks about auto-trading status, wants to know bot's next action, check strategy rules, understand trading decisions",
        "tools_needed": ["bot_status", "bot_logs"],
        "type": "feature",
    },
    {
        "id": "mt5_setup",
        "name": "MT5 Terminal Setup & Server Config",
        "description": "Knowledge of MT5 terminal setup: generic MetaQuotes terminal vs Exness-branded terminal. servers.dat contains broker server list. Exness must be added via File > Open an Account > search 'Exness' > select Exness Technologies Ltd. > choose Exness-MT5Real3. common.ini stores auto-login (Login, Server). accounts.dat stores saved account credentials (encrypted binary). Exness uses 'm' suffix symbols (EURUSDm). Exness requires build >= 4755 (current build 6036). AutoTrading green play button (▶) must be enabled for EA trading. Api=1 in common.ini enables IPC/MQL bridge.",
        "when_to_use": "User asks about MT5 setup, connecting to Exness, terminal configuration, why trading isn't working, server connection issues",
        "tools_needed": [],
        "type": "knowledge",
    },
    {
        "id": "email",
        "name": "Email & Communication",
        "description": "Send emails, check inbox, draft replies — handles communication on your behalf",
        "when_to_use": "User wants to send/check emails, draft messages, manage inbox",
        "tools_needed": ["email_send", "email_read"],
        "type": "feature",
    },
    {
        "id": "doctor",
        "name": "Self-Diagnosis & Recovery (Doctor)",
        "description": "Full system diagnostic that checks Friday server, trading bridge (port 8001), Exness account details, MT5 terminal, trading bot, auto-healer, and disk space. Reports results in plain English with fix suggestions. Auto-fixes by starting any missing services. Access via: python -m friday.doctor, web UI /doctor endpoint, or simply asking Friday to run a check.",
        "when_to_use": "User says 'something's wrong', 'check everything', 'run doctor', 'diagnose', 'health check', 'how's everything running', or anything seems broken — run the full suite of checks and report in plain English",
        "tools_needed": ["run_doctor"],
        "type": "feature",
    },
]

TOOL_CAPABILITY_MAP: Dict[str, str] = {}
for cap in CAPABILITIES:
    for tool in cap.get("tools_needed", []):
        TOOL_CAPABILITY_MAP[tool] = cap["id"]


def get_capabilities() -> List[dict]:
    return CAPABILITIES


def get_capability(cap_id: str) -> dict:
    for c in CAPABILITIES:
        if c["id"] == cap_id:
            return c
    return {"id": cap_id, "name": cap_id, "description": "Unknown capability"}


def get_summary() -> str:
    lines = ["Friday Capabilities:"]
    for cap in CAPABILITIES:
        lines.append(f"  • {cap['name']} — {cap['description']}")
    return "\n".join(lines)


def get_tool_capability(tool_name: str) -> str:
    return TOOL_CAPABILITY_MAP.get(tool_name, "general")


def format_for_prompt() -> str:
    lines = ["You have the following capabilities at your disposal:", ""]
    for cap in CAPABILITIES:
        lines.append(f"[{cap['name']}]")
        lines.append(f"  {cap['description']}")
        lines.append(f"  Use when: {cap['when_to_use']}")
        if cap["tools_needed"]:
            lines.append(f"  Tools: {', '.join(cap['tools_needed'])}")
        lines.append("")
    return "\n".join(lines)
