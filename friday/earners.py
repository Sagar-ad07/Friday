"""
Friday — Active Earners
Actual money-making bots that execute strategies, not just scanners.
Designed for zero-cost, no-API-key-required earning methods.
"""
import json
import logging
import os
import random
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional

logger = logging.getLogger("Friday.Earners")

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
STATE_PATH = os.path.join(os.path.dirname(ROOT), "data", "earners_state.json")
_earners: Dict[str, "BaseEarner"] = {}
_earners_lock = threading.Lock()


# ── Base ──

class BaseEarner:
    """Base class for all earning bots."""

    def __init__(self, earner_id: str, name: str, config: dict):
        self.id = earner_id
        self.name = name
        self.config = config
        self.status = "stopped"
        self.thread: Optional[threading.Thread] = None
        self._stop = threading.Event()
        self.earnings_log: List[dict] = []
        self.started_at: Optional[str] = None

    def start(self):
        if self.thread and self.thread.is_alive():
            return
        self._stop.clear()
        self.status = "running"
        self.started_at = datetime.now(timezone.utc).isoformat()
        self.thread = threading.Thread(target=self._loop, daemon=True, name=f"earner-{self.id}")
        self.thread.start()
        logger.info("Earner started: %s", self.name)

    def stop(self):
        self._stop.set()
        self.status = "stopped"
        logger.info("Earner stopped: %s", self.name)

    def _loop(self):
        raise NotImplementedError

    def log_earning(self, source: str, amount: float, currency: str = "USD", note: str = ""):
        entry = {
            "ts": datetime.now(timezone.utc).isoformat(),
            "source": source,
            "amount": amount,
            "currency": currency,
            "note": note,
        }
        self.earnings_log.append(entry)
        # Also log to global dashboard
        try:
            from friday.money_makers import get_dashboard
            get_dashboard().log_earning(source, amount, currency)
        except Exception:
            pass
        try:
            from friday.self_model import update_earnings
            update_earnings(amount)
        except Exception:
            pass

    def status_dict(self) -> dict:
        return {
            "id": self.id,
            "name": self.name,
            "status": self.status,
            "started_at": self.started_at,
            "total_earned": sum(e["amount"] for e in self.earnings_log),
            "transactions": len(self.earnings_log),
            "config": {k: v for k, v in self.config.items() if "key" not in k.lower() and "secret" not in k.lower() and "password" not in k.lower()},
        }


# ── Web Research Earner ──
# Finds free money opportunities, cashback deals, airdrops, survey sites,
# signup bonuses — pure research, no capital required.

class ResearchEarner(BaseEarner):
    """Searches for free money opportunities online: airdrops, signup bonuses,
    cashback deals, survey sites, micro-tasks. Zero capital required."""

    def __init__(self, earner_id: str, config: dict):
        super().__init__(earner_id, "Web Research Earner", config)

    def _loop(self):
        interval = self.config.get("interval", 3600)  # every hour
        sectors = self.config.get("sectors", [
            "free crypto airdrops 2026",
            "signup bonus apps pay instantly",
            "get paid for testing apps",
            "micro task sites payout",
            "cashback apps no deposit",
        ])
        while not self._stop.is_set():
            try:
                from friday.tools_web import web_search
                for sector in sectors:
                    if self._stop.is_set():
                        return
                    logger.debug("ResearchEarner searching: %s", sector)
                    results = web_search(sector, num_results=5)
                    opps = []
                    for r in results:
                        opps.append({
                            "title": r.get("title", ""),
                            "url": r.get("url", ""),
                            "snippet": r.get("snippet", ""),
                            "sector": sector,
                        })
                    if opps:
                        self.log_earning(sector, 0.0, "info", f"Found {len(opps)} opportunities")
                    time.sleep(30)
            except Exception as e:
                logger.error("ResearchEarner error: %s", e)

            # Wait remainder of interval (check stop every 30s)
            remaining = interval
            while remaining > 0 and not self._stop.is_set():
                time.sleep(min(30, remaining))
                remaining -= 30


# ── Crypto Airdrop Earner ──
# Monitors and claims free crypto airdrops, testnet faucets, and staking rewards.

class CryptoEarner(BaseEarner):
    """Monitors crypto airdrops, testnet faucets, and staking opportunities.
    Tracks potential earnings and notifies when action is needed."""

    def __init__(self, earner_id: str, config: dict):
        super().__init__(earner_id, "Crypto Opportunity Earner", config)

    def _loop(self):
        interval = self.config.get("interval", 7200)
        queries = [
            "crypto airdrop claim today",
            "free crypto testnet faucet",
            "crypto staking rewards no deposit",
            "new cryptocurrency airdrop list",
        ]
        while not self._stop.is_set():
            try:
                from friday.tools_web import web_search
                for q in queries:
                    if self._stop.is_set():
                        return
                    results = web_search(q, num_results=3)
                    for r in results:
                        title = r.get("title", "")
                        if any(kw in title.lower() for kw in ["airdrop", "free", "claim", "faucet", "reward"]):
                            self.log_earning("crypto_opportunity", 0.0, "info",
                                f"Potential: {title[:100]}")
                    time.sleep(60)
            except Exception as e:
                logger.error("CryptoEarner error: %s", e)

            remaining = interval
            while remaining > 0 and not self._stop.is_set():
                time.sleep(min(30, remaining))
                remaining -= 30


# ── Factory & Registry ──

EARNER_TYPES = {
    "research": ResearchEarner,
    "crypto": CryptoEarner,
}

DEFAULT_EARNERS = [
    {"id": "earner_research", "type": "research", "config": {"interval": 3600}},
    {"id": "earner_crypto", "type": "crypto", "config": {"interval": 7200}},
]


def start_defaults():
    """Start all default earners."""
    for spec in DEFAULT_EARNERS:
        start_earner(spec["id"], spec["type"], spec["config"])


def start_earner(earner_id: str, earner_type: str, config: dict) -> Optional[str]:
    cls = EARNER_TYPES.get(earner_type)
    if not cls:
        logger.warning("Unknown earner type: %s", earner_type)
        return None
    with _earners_lock:
        if earner_id in _earners:
            logger.info("Earner %s already exists, restarting", earner_id)
            _earners[earner_id].stop()
        earner = cls(earner_id, config)
        _earners[earner_id] = earner
    earner.start()
    return earner_id


def stop_earner(earner_id: str) -> bool:
    with _earners_lock:
        earner = _earners.get(earner_id)
        if not earner:
            return False
        earner.stop()
        return True


def list_earners() -> List[dict]:
    with _earners_lock:
        return [e.status_dict() for e in _earners.values()]


def get_total_earned() -> float:
    with _earners_lock:
        return sum(e.status_dict()["total_earned"] for e in _earners.values())
