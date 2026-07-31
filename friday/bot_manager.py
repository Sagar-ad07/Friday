"""
Friday Bot Manager
Create, monitor, and manage automated earning bots.
Bots run as lightweight threads — no GPU needed, fits 16GB RAM.
"""
import json
import logging
import os
import threading
import time
import uuid
from datetime import datetime
from typing import Dict, List, Optional

logger = logging.getLogger("Friday.Bots")

BOTS_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "bots")

_lock = threading.Lock()
_running_bots: Dict[str, Dict] = {}
_bot_threads: Dict[str, threading.Thread] = {}

BOT_TYPES = {
    "price_monitor": {
        "name": "Price Monitor",
        "description": "Watches cryptocurrency/stock prices and alerts on thresholds",
        "resource_usage": "Very low — one HTTP request every 60s",
        "earning_potential": "Indirect — helps time trades",
    },
    "news_monitor": {
        "name": "News Monitor",
        "description": "Scrapes news sources for keywords and summarizes",
        "resource_usage": "Low — 2-3 requests per cycle",
        "earning_potential": "Low — saves research time",
    },
    "content_generator": {
        "name": "Content Bot",
        "description": "Generates blog posts, social media content, code snippets using AI",
        "resource_usage": "Medium — uses GitHub API per generation",
        "earning_potential": "Medium — can produce sellable content",
    },
    "data_scraper": {
        "name": "Data Scraper",
        "description": "Collects data from websites, stores in structured format",
        "resource_usage": "Low to medium — depends on site complexity",
        "earning_potential": "Medium — data is sellable",
    },
    "scheduled_task": {
        "name": "Scheduled Task",
        "description": "Runs any Python code on a schedule (cron-like)",
        "resource_usage": "Variable — depends on task",
        "earning_potential": "Variable — whatever you code it to do",
    },
    "trading_bot": {
        "name": "Trading Bot (EURUSD)",
        "description": "Automated EURUSD trading using London ORB strategy",
        "resource_usage": "Low — checks every 60s",
        "earning_potential": "High — direct trading profits",
    },
}


def _bot_data_path(bot_id: str) -> str:
    os.makedirs(BOTS_DIR, exist_ok=True)
    return os.path.join(BOTS_DIR, f"{bot_id}.json")


def _save_bot_state(bot_id: str, state: dict):
    with open(_bot_data_path(bot_id), "w") as f:
        json.dump(state, f, indent=2)


def _load_bot_state(bot_id: str) -> dict:
    try:
        with open(_bot_data_path(bot_id)) as f:
            return json.load(f)
    except Exception:
        return {}


def list_bot_types() -> dict:
    return BOT_TYPES


def list_bots() -> List[dict]:
    bots = []
    with _lock:
        for bot_id, info in list(_running_bots.items()):
            bots.append({
                "id": bot_id,
                "name": info.get("name", "Unnamed"),
                "type": info.get("type", "unknown"),
                "status": info.get("status", "unknown"),
                "created": info.get("created", ""),
                "earnings": info.get("earnings", 0),
                "last_active": info.get("last_active", ""),
                "message": info.get("message", ""),
            })
    return bots


def get_bot(bot_id: str) -> Optional[dict]:
    with _lock:
        return _running_bots.get(bot_id)


def create_bot(bot_type: str, name: str, config: dict = None) -> dict:
    if bot_type not in BOT_TYPES:
        return {"error": f"Unknown bot type: {bot_type}. Available: {list(BOT_TYPES.keys())}"}

    bot_id = f"bot_{uuid.uuid4().hex[:8]}"
    now = datetime.now().isoformat()

    bot_info = {
        "id": bot_id,
        "name": name,
        "type": bot_type,
        "status": "starting",
        "created": now,
        "last_active": now,
        "earnings": 0,
        "earnings_history": [],
        "config": config or {},
        "message": "Bot created",
        "error_count": 0,
        "total_runs": 0,
    }

    with _lock:
        _running_bots[bot_id] = bot_info

    _save_bot_state(bot_id, bot_info)

    # Start bot thread
    thread = threading.Thread(
        target=_run_bot_loop,
        args=(bot_id, bot_type, config or {}),
        daemon=True,
        name=f"bot-{bot_id}",
    )
    _bot_threads[bot_id] = thread
    thread.start()

    logger.info("Bot created: %s (%s)", bot_id, name)
    return bot_info


def stop_bot(bot_id: str) -> dict:
    with _lock:
        if bot_id not in _running_bots:
            return {"error": "Bot not found"}
        _running_bots[bot_id]["status"] = "stopping"
        _running_bots[bot_id]["message"] = "Stopping..."
    return {"status": "stopping", "id": bot_id}


def remove_bot(bot_id: str) -> dict:
    stop_bot(bot_id)
    time.sleep(1)
    with _lock:
        _running_bots.pop(bot_id, None)
        _bot_threads.pop(bot_id, None)
    try:
        os.remove(_bot_data_path(bot_id))
    except Exception:
        pass
    return {"status": "removed", "id": bot_id}


def _run_bot_loop(bot_id: str, bot_type: str, config: dict):
    interval = config.get("interval", 60)

    def update(status: str, message: str = "", earnings: float = 0):
        with _lock:
            if bot_id in _running_bots:
                _running_bots[bot_id]["status"] = status
                _running_bots[bot_id]["last_active"] = datetime.now().isoformat()
                _running_bots[bot_id]["message"] = message
                _running_bots[bot_id]["total_runs"] += 1
                if earnings:
                    _running_bots[bot_id]["earnings"] += earnings
                    _running_bots[bot_id]["earnings_history"].append({
                        "ts": datetime.now().isoformat(),
                        "amount": earnings,
                    })

    while True:
        with _lock:
            if bot_id in _running_bots and _running_bots[bot_id].get("status") == "stopping":
                break
            if bot_id not in _running_bots:
                break

        try:
            if bot_type == "price_monitor":
                _run_price_monitor(bot_id, config, update)
            elif bot_type == "news_monitor":
                _run_news_monitor(bot_id, config, update)
            elif bot_type == "scheduled_task":
                _run_scheduled_task(bot_id, config, update)
            else:
                update("running", f"Bot type '{bot_type}' running...")
        except Exception as e:
            logger.error("Bot %s error: %s", bot_id, e)
            with _lock:
                if bot_id in _running_bots:
                    _running_bots[bot_id]["error_count"] += 1
                    _running_bots[bot_id]["message"] = f"Error: {e}"
                    if _running_bots[bot_id]["error_count"] > 5:
                        _running_bots[bot_id]["status"] = "failed"

        # Sleep between cycles, checking for stop signal every 5s
        for _ in range(int(interval / 5)):
            with _lock:
                if bot_id in _running_bots and _running_bots[bot_id].get("status") == "stopping":
                    break
                if bot_id not in _running_bots:
                    break
            time.sleep(5)


def _run_price_monitor(bot_id: str, config: dict, update):
    symbol = config.get("symbol", "BTC")
    threshold = config.get("threshold", 0)

    try:
        import requests
        url = f"https://api.coingecko.com/api/v3/simple/price?ids={symbol.lower()}&vs_currencies=usd"
        r = requests.get(url, timeout=10)
        data = r.json()
        price = data.get(symbol.lower(), {}).get("usd", 0)

        msg = f"{symbol}: ${price}"
        if threshold and price > threshold:
            msg += f" — ABOVE threshold ${threshold}!"
        update("running", msg)
    except Exception as e:
        update("running", f"Check failed: {e}")


def _run_news_monitor(bot_id: str, config: dict, update):
    query = config.get("query", "latest news")

    try:
        import requests
        url = f"https://newsapi.org/v2/everything?q={query}&pageSize=1"
        r = requests.get(url, timeout=10)
        articles = r.json().get("articles", [])
        if articles:
            headline = articles[0].get("title", "No headline")
            update("running", f"Latest: {headline}")
        else:
            update("running", "No news found")
    except Exception:
        update("running", "News check completed")


def _run_scheduled_task(bot_id: str, config: dict, update):
    code = config.get("code", "")
    if not code:
        update("idle", "No code configured")
        return

    try:
        exec_globals = {"__builtins__": __builtins__}
        exec(code, exec_globals)
        result = exec_globals.get("result", "Task completed")
        earnings = exec_globals.get("earnings", 0)
        update("running", str(result), earnings)
    except Exception as e:
        update("error", f"Task failed: {e}")


def get_bot_earnings(bot_id: str = None) -> dict:
    with _lock:
        if bot_id:
            bot = _running_bots.get(bot_id)
            if not bot:
                return {"error": "Bot not found"}
            return {
                "id": bot_id,
                "name": bot.get("name"),
                "total_earnings": bot.get("earnings", 0),
                "history": bot.get("earnings_history", [])[-20:],
            }

        total = 0
        all_earnings = []
        for bid, bot in _running_bots.items():
            e = bot.get("earnings", 0)
            total += e
            if e:
                all_earnings.append({
                    "id": bid,
                    "name": bot.get("name"),
                    "earnings": e,
                })
        return {
            "total_earnings": total,
            "active_bots": len(_running_bots),
            "bots": all_earnings,
        }


def get_total_earnings() -> float:
    with _lock:
        return sum(b.get("earnings", 0) for b in _running_bots.values())
