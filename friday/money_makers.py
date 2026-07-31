"""
Friday Money-Making Engine
Bots that search, research, monitor, strategise, and track earnings.
Designed to run on low-end hardware — no GPU, no heavy infra.
"""
import logging
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timedelta
from typing import Dict, List, Optional

from .tools_web import web_search, web_search_parallel, fetch_page

logger = logging.getLogger("Friday.MoneyMakers")

_lock = threading.Lock()


# ── WebSearchBot ──

class WebSearchBot:
    """Runs parallel web searches on multiple topics simultaneously."""

    def __init__(self, num_workers: int = 3):
        self.num_workers = num_workers

    def search_all(self, queries: list[str]) -> list[dict]:
        results = []
        batch = web_search_parallel(queries, num_results=5)
        for q, res in zip(queries, batch):
            results.append({"query": q, "results": res, "count": len(res)})
        return results

    def search_and_analyze(self, topic: str, subtopics: list[str]) -> dict:
        all_queries = [topic] + [f"{topic} {s}" for s in subtopics]
        raw = self.search_all(all_queries)
        combined = []
        seen = set()
        for item in raw:
            for r in item["results"]:
                key = r.get("url", r.get("title", ""))
                if key not in seen:
                    seen.add(key)
                    combined.append(r)
        return {
            "topic": topic,
            "subtopics": subtopics,
            "total_results": len(combined),
            "results": combined[:20],
            "timestamp": datetime.now().isoformat(),
        }


# ── ResearchBot ──

class ResearchBot:
    """Deep research on topics — searches, reads top results, extracts insights."""

    def deep_research(self, topic: str, depth: int = 3) -> dict:
        results = web_search(topic, num_results=depth + 3)
        urls = [r["url"] for r in results[:depth] if r.get("url") and r["url"] != ""]
        pages = []
        with ThreadPoolExecutor(max_workers=depth) as pool:
            futures = {pool.submit(fetch_page, url): url for url in urls}
            for future in as_completed(futures):
                try:
                    text = future.result()
                    if text and not text.startswith("Error"):
                        pages.append({"url": futures[future], "content": text[:1500]})
                except Exception:
                    pass
        insights = []
        for p in pages:
            sentences = [s.strip() for s in p["content"].split(".") if len(s.strip()) > 40]
            insights.extend(sentences[:3])
        return {
            "topic": topic,
            "sources_consulted": urls,
            "pages_parsed": len(pages),
            "insights": insights[:10],
            "raw_results": results[:depth],
            "timestamp": datetime.now().isoformat(),
        }

    def find_opportunities(self, sector: str) -> list[dict]:
        queries = [
            f"make money {sector} 2026",
            f"{sector} profitable opportunities",
            f"best {sector} side hustle",
            f"{sector} passive income ideas",
        ]
        search_results = web_search_parallel(queries, num_results=6)
        opportunities = []
        seen = set()
        for q, results in zip(queries, search_results):
            for r in results:
                title = r.get("title", "")
                snippet = r.get("snippet", "")
                key = title + snippet
                if key not in seen:
                    seen.add(key)
                    opportunities.append({
                        "source_query": q,
                        "title": title,
                        "url": r.get("url", ""),
                        "snippet": snippet,
                        "sector": sector,
                    })
        return opportunities


# ── MonitorBot ──

class MonitorBot:
    """Background monitor that watches conditions and reports findings."""

    def __init__(self):
        self._monitors: Dict[str, dict] = {}
        self._threads: Dict[str, threading.Thread] = {}
        self._results: Dict[str, list] = {}

    MONITOR_TYPES = {
        "price_alerts": {
            "name": "Price Alert Monitor",
            "description": "Checks MT5/crypto prices and alerts on thresholds",
            "default_interval": 120,
        },
        "news_alerts": {
            "name": "News Alert Monitor",
            "description": "Searches web for specific news topics periodically",
            "default_interval": 300,
        },
        "opportunity_scanner": {
            "name": "Opportunity Scanner",
            "description": "Scours for new money-making ideas in target sectors",
            "default_interval": 600,
        },
    }

    def start_monitor(self, name: str, config: dict) -> str:
        if name not in self.MONITOR_TYPES:
            raise ValueError(f"Unknown monitor type: {name}. Available: {list(self.MONITOR_TYPES.keys())}")

        monitor_id = f"mon_{uuid.uuid4().hex[:8]}"
        interval = config.get("interval", self.MONITOR_TYPES[name]["default_interval"])

        monitor_info = {
            "id": monitor_id,
            "name": name,
            "config": config,
            "interval": interval,
            "status": "running",
            "created": datetime.now().isoformat(),
        }

        thread = threading.Thread(
            target=self._run_monitor_loop,
            args=(monitor_id, name, config, interval),
            daemon=True,
            name=f"mon-{monitor_id}",
        )

        with _lock:
            self._monitors[monitor_id] = monitor_info
            self._results[monitor_id] = []
            self._threads[monitor_id] = thread

        thread.start()
        logger.info("Monitor started: %s (%s)", monitor_id, name)
        return monitor_id

    def stop_monitor(self, monitor_id: str) -> bool:
        with _lock:
            if monitor_id not in self._monitors:
                return False
            self._monitors[monitor_id]["status"] = "stopping"
        return True

    def get_results(self, monitor_id: str) -> list:
        with _lock:
            return list(self._results.get(monitor_id, []))

    def list_monitors(self) -> list[dict]:
        with _lock:
            return [
                {
                    "id": mid,
                    "name": info["name"],
                    "status": info["status"],
                    "interval": info["interval"],
                    "created": info["created"],
                }
                for mid, info in self._monitors.items()
            ]

    def _run_monitor_loop(self, monitor_id: str, name: str, config: dict, interval: int):
        while True:
            with _lock:
                if monitor_id not in self._monitors:
                    break
                if self._monitors[monitor_id].get("status") == "stopping":
                    self._monitors[monitor_id]["status"] = "stopped"
                    break

            try:
                if name == "price_alerts":
                    result = self._check_prices(config)
                elif name == "news_alerts":
                    result = self._check_news(config)
                elif name == "opportunity_scanner":
                    result = self._scan_opportunities(config)
                else:
                    result = {"error": f"Unknown monitor: {name}"}

                with _lock:
                    if monitor_id in self._results:
                        result["timestamp"] = datetime.now().isoformat()
                        self._results[monitor_id].append(result)
                        if len(self._results[monitor_id]) > 100:
                            self._results[monitor_id] = self._results[monitor_id][-50:]

            except Exception as e:
                logger.error("Monitor %s error: %s", monitor_id, e)

            # Efficient sleep with stop-check every 30s
            remaining = interval
            while remaining > 0:
                with _lock:
                    if monitor_id in self._monitors and self._monitors[monitor_id].get("status") == "stopping":
                        return
                    if monitor_id not in self._monitors:
                        return
                chunk = min(30, remaining)
                time.sleep(chunk)
                remaining -= chunk

    def _check_prices(self, config: dict) -> dict:
        symbol = config.get("symbol", "BTC")
        threshold_high = config.get("threshold_high", 0)
        threshold_low = config.get("threshold_low", 0)
        try:
            import requests
            url = f"https://api.coingecko.com/api/v3/simple/price?ids={symbol.lower()}&vs_currencies=usd"
            r = requests.get(url, timeout=10)
            data = r.json()
            price = data.get(symbol.lower(), {}).get("usd", 0)
            alerts = []
            if threshold_high and price >= threshold_high:
                alerts.append(f"ABOVE ${threshold_high}")
            if threshold_low and price <= threshold_low:
                alerts.append(f"BELOW ${threshold_low}")
            return {"symbol": symbol, "price": price, "alerts": alerts, "type": "price_check"}
        except Exception as e:
            return {"symbol": symbol, "error": str(e), "type": "price_check"}

    def _check_news(self, config: dict) -> dict:
        query = config.get("query", "breaking news")
        try:
            results = web_search(query, num_results=3)
            return {"query": query, "articles": results, "type": "news_check"}
        except Exception as e:
            return {"query": query, "error": str(e), "type": "news_check"}

    def _scan_opportunities(self, config: dict) -> dict:
        sector = config.get("sector", "online earning")
        bot = ResearchBot()
        opps = bot.find_opportunities(sector)
        return {"sector": sector, "opportunities": opps[:5], "type": "opportunity_scan"}


# ── EarningStrategy ──

class EarningStrategy:
    """Strategy engine with predefined earning strategies and personalised planning."""

    def __init__(self):
        self.strategies = {
            "forex_scalping": {
                "name": "Forex Scalping Bot",
                "description": "Automated EURUSD scalping using London ORB breakout with 1-minute entries. Targets 5-10 pips per trade.",
                "roi_estimate": "5-15% monthly",
                "risk_level": "High",
                "setup_steps": [
                    "Install MT5 or connect to broker API",
                    "Configure risk per trade (recommended: 1-2%)",
                    "Set up trading bot with EURUSD strategy",
                    "Backtest on 6 months of historical data",
                    "Start with demo account for 2 weeks",
                    "Go live with small capital ($100-500)",
                ],
                "required_tools": ["MT5 terminal", "broker API", "trading bot", "VPS (optional)"],
                "min_capital": 100,
                "time_commitment": "2-4 hours/day initial, 30min/day maintenance",
            },
            "crypto_arbitrage": {
                "name": "Crypto Arbitrage Scanner",
                "description": "Monitor price differences across exchanges and execute triangular arbitrage opportunities.",
                "roi_estimate": "2-8% monthly",
                "risk_level": "Medium",
                "setup_steps": [
                    "Create accounts on 3+ exchanges (Binance, Coinbase, Kraken)",
                    "Set up exchange API keys with trading permissions",
                    "Deploy arbitrage scanner bot",
                    "Configure minimum profit threshold (0.5-1%)",
                    "Test with small amounts first",
                    "Scale up once strategy is validated",
                ],
                "required_tools": ["exchange APIs", "arbitrage scanner bot", "stablecoin reserves"],
                "min_capital": 500,
                "time_commitment": "1-2 hours/day monitoring",
            },
            "affiliate_marketing": {
                "name": "Affiliate Marketing Automator",
                "description": "AI-powered content generation for affiliate sites. Auto-publish reviews, comparisons, and roundups.",
                "roi_estimate": "$500-5000/month",
                "risk_level": "Low",
                "setup_steps": [
                    "Choose niche (tech, finance, health, hobbies)",
                    "Register domain and set up WordPress site",
                    "Join affiliate programs (Amazon, CJ, ShareASale)",
                    "Configure content generator bot",
                    "Set up SEO optimisation pipeline",
                    "Auto-publish 3-5 articles per week",
                    "Build email list for repeat traffic",
                ],
                "required_tools": ["domain + hosting", "WordPress", "content generator", "SEO tools"],
                "min_capital": 50,
                "time_commitment": "1-2 hours/week after setup",
            },
            "content_monetization": {
                "name": "Content Monetisation Pipeline",
                "description": "Create and sell digital products: templates, code snippets, ebooks, courses using AI generation.",
                "roi_estimate": "$200-3000/month",
                "risk_level": "Low",
                "setup_steps": [
                    "Identify high-demand digital products in your niche",
                    "Use AI tools to create product drafts",
                    "Polish and package products",
                    "Set up store on Gumroad or Sellfy",
                    "Create social media promotion pipeline",
                    "Build email list for launches",
                ],
                "required_tools": ["AI content tools", "Gumroad/Sellfy account", "social media scheduler"],
                "min_capital": 20,
                "time_commitment": "5-10 hours/week",
            },
            "trading_bot": {
                "name": "Automated Trading System",
                "description": "Multi-strategy trading bot combining momentum, mean-reversion, and breakout strategies.",
                "roi_estimate": "3-12% monthly",
                "risk_level": "High",
                "setup_steps": [
                    "Select markets (forex, crypto, or stocks)",
                    "Develop/acquire trading strategies",
                    "Implement risk management (max 2% per trade)",
                    "Backtest across multiple market conditions",
                    "Run paper trading for 1 month",
                    "Deploy with small capital",
                    "Monitor and adjust parameters weekly",
                ],
                "required_tools": ["broker API", "trading framework", "backtesting engine", "VPS"],
                "min_capital": 200,
                "time_commitment": "3-5 hours/day initial, 1 hour/day maintenance",
            },
            "freelance_automation": {
                "name": "Freelance Service Automator",
                "description": "Automate freelance service delivery: data entry, web scraping, content writing, virtual assistant tasks.",
                "roi_estimate": "$1000-8000/month",
                "risk_level": "Low",
                "setup_steps": [
                    "Set up profiles on Upwork, Fiverr, Freelancer",
                    "Create service packages with clear pricing",
                    "Build automation scripts for service delivery",
                    "Use AI for proposal writing and client communication",
                    "Automate invoicing and follow-ups",
                    "Scale by outsourcing overflow to virtual assistants",
                ],
                "required_tools": ["freelance platform accounts", "automation scripts", "AI writing tools"],
                "min_capital": 0,
                "time_commitment": "10-20 hours/week",
            },
        }

    def get_strategy(self, name: str) -> dict:
        if name in self.strategies:
            return self.strategies[name]
        return {"error": f"Unknown strategy: {name}. Available: {list(self.strategies.keys())}"}

    def get_all_strategies(self) -> list[dict]:
        return list(self.strategies.values())

    def generate_plan(self, balance: float, risk_tolerance: str) -> dict:
        risk_tolerance = risk_tolerance.lower()

        compatible = []
        for key, s in self.strategies.items():
            risk_ok = (
                (risk_tolerance == "low" and s["risk_level"] == "Low")
                or (risk_tolerance == "medium" and s["risk_level"] in ("Low", "Medium"))
                or (risk_tolerance == "high")
                or (risk_tolerance == "low" and s["risk_level"] == "Medium")
            )
            if risk_ok and balance >= s["min_capital"]:
                compatible.append(key)

        if not compatible:
            cheapest = min(self.strategies.items(), key=lambda x: x[1]["min_capital"])
            compatible = [cheapest[0]]

        plan_steps = []
        total_allocation = 0
        allocation_per = round(min(100 / len(compatible), 50), 1)

        for key in compatible[:4]:
            s = self.strategies[key]
            alloc = min(balance * allocation_per / 100, balance - total_allocation)
            total_allocation += alloc
            plan_steps.append({
                "strategy": key,
                "name": s["name"],
                "allocation": round(alloc, 2),
                "risk_level": s["risk_level"],
                "expected_roi": s["roi_estimate"],
                "setup_steps": s["setup_steps"],
            })

        return {
            "plan_generated": datetime.now().isoformat(),
            "total_balance": balance,
            "risk_tolerance": risk_tolerance,
            "allocation_total": round(total_allocation, 2),
            "reserve": round(balance - total_allocation, 2),
            "steps": plan_steps,
        }


# ── EarningDashboard ──

class EarningDashboard:
    """Tracks earnings across all bots and sources."""

    def __init__(self):
        self._lock = threading.Lock()
        self._earnings: List[dict] = []

    def log_earning(self, source: str, amount: float, currency: str = "USD"):
        entry = {
            "source": source,
            "amount": amount,
            "currency": currency,
            "timestamp": datetime.now().isoformat(),
        }
        with self._lock:
            self._earnings.append(entry)

    def get_total_earnings(self) -> dict:
        with self._lock:
            totals = {}
            for e in self._earnings:
                curr = e["currency"]
                totals[curr] = totals.get(curr, 0) + e["amount"]
            return {
                "totals_by_currency": totals,
                "grand_total_usd": totals.get("USD", 0),
                "total_entries": len(self._earnings),
            }

    def get_earnings_by_source(self) -> dict:
        with self._lock:
            by_source = {}
            for e in self._earnings:
                src = e["source"]
                if src not in by_source:
                    by_source[src] = {"total": 0, "count": 0, "currency": e["currency"]}
                by_source[src]["total"] += e["amount"]
                by_source[src]["count"] += 1
            return by_source

    def get_daily_summary(self) -> dict:
        with self._lock:
            today = datetime.now().date()
            yesterday = today - timedelta(days=1)
            today_total = 0
            today_count = 0
            yesterday_total = 0
            for e in self._earnings:
                ts = datetime.fromisoformat(e["timestamp"]).date()
                if ts == today:
                    today_total += e["amount"]
                    today_count += 1
                elif ts == yesterday:
                    yesterday_total += e["amount"]
            return {
                "date": today.isoformat(),
                "today_earnings": today_total,
                "today_transactions": today_count,
                "yesterday_earnings": yesterday_total,
                "sources_active": len(set(e["source"] for e in self._earnings
                                        if datetime.fromisoformat(e["timestamp"]).date() == today)),
            }

    def get_history(self, limit: int = 50) -> list[dict]:
        with self._lock:
            return list(self._earnings[-limit:])


# ── Singleton factory for easy access ──

_default_dashboard = EarningDashboard()
_default_monitor = MonitorBot()
_default_strategy = EarningStrategy()


def get_dashboard() -> EarningDashboard:
    return _default_dashboard


def get_monitor() -> MonitorBot:
    return _default_monitor


def get_strategy_engine() -> EarningStrategy:
    return _default_strategy
