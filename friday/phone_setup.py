"""
Friday — Phone & Mobile Setup
Configure once, control Friday from anywhere via Telegram.
"""

TELEGRAM_SETUP = {
    "steps": [
        "1. Open Telegram, search @BotFather",
        "2. Send /newbot, name it 'Friday'",
        "3. BotFather gives you a TOKEN like: 123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
        "4. Add that token to .env file: TELEGRAM_BOT_TOKEN=your_token_here",
        "5. Restart Friday. Now chat with your bot on Telegram.",
    ],
    "commands": {
        "/start": "Start Friday",
        "/help": "Show all commands",
        "/status": "Check everything",
        "/search [query]": "Search web",
        "/sms [number] [msg]": "Send SMS via phone agent",
        "/earn": "Show earning opportunities",
        "/doctor": "Run system diagnostic",
        "/trade": "Check trading status",
    },
}

RELAY_SETUP = {
    "what": "Relay lets your phone talk to Friday even when both are on different networks.",
    "option1": {
        "name": "Railway (free)",
        "steps": [
            "Go to railway.new",
            "Connect GitHub repo",
            "Deploy friday/relay.py as a service",
            "Get URL like: friday-relay.up.railway.app",
            "Set RELAY_URL in .env",
        ],
    },
    "option2": {
        "name": "Ngrok (instant)",
        "steps": [
            "Download ngrok",
            "Run: ngrok http 8000",
            "Get URL like: https://abc123.ngrok-free.app",
            "Use that URL from your phone browser",
        ],
    },
}

def guide() -> str:
    lines = []
    lines.append("Friday Phone Access Guide")
    lines.append("=" * 40)
    lines.append("")
    lines.append("OPTION 1: Telegram Bot (EASIEST)")
    lines.append("-" * 30)
    for s in TELEGRAM_SETUP["steps"]:
        lines.append(f"  {s}")
    lines.append("")
    lines.append("Available commands after setup:")
    for cmd, desc in TELEGRAM_SETUP["commands"].items():
        lines.append(f"  {cmd:20s} {desc}")
    lines.append("")
    lines.append("OPTION 2: Ngrok Tunnel (Browser from phone)")
    lines.append("-" * 30)
    for s in RELAY_SETUP["option2"]["steps"]:
        lines.append(f"  {s}")
    lines.append("")
    lines.append("OPTION 3: Phone Agents (SMS/Call control)")
    lines.append("-" * 30)
    lines.append("  Run: configure_phone_agents.bat")
    lines.append("  Then use Friday from any phone via SMS.")
    lines.append("")
    lines.append("All three work simultaneously. Start with Telegram — it's free and instant.")
    return "\n".join(lines)
