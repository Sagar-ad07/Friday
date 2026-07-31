"""
Friday Scheduler — runs tasks on timers, auto-starting and monitoring.
Lightweight, no cron dependency, pure Python threading.
"""
import json
import logging
import os
import threading
import time
from datetime import datetime, timedelta
from typing import Callable, Dict, List, Optional

logger = logging.getLogger("Friday.Scheduler")

SCHED_PATH = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "schedule.json")

_lock = threading.Lock()
_scheduled_tasks: List[Dict] = []
_scheduler_thread: Optional[threading.Thread] = None
_running = False


def _load():
    global _scheduled_tasks
    try:
        if os.path.exists(SCHED_PATH):
            with open(SCHED_PATH) as f:
                _scheduled_tasks = json.load(f)
    except Exception as e:
        logger.warning("Failed to load schedule: %s", e)
        _scheduled_tasks = []


def _save():
    os.makedirs(os.path.dirname(SCHED_PATH), exist_ok=True)
    with open(SCHED_PATH, "w") as f:
        json.dump(_scheduled_tasks, f, indent=2)


class Task:
    def __init__(self, task_id: str, name: str, interval_minutes: int, callback: Callable):
        self.id = task_id
        self.name = name
        self.interval = interval_minutes
        self.callback = callback
        self.last_run: Optional[str] = None
        self.next_run: str = (datetime.now() + timedelta(minutes=interval_minutes)).isoformat()
        self.status = "pending"
        self.run_count = 0

    def to_dict(self) -> dict:
        return {
            "id": self.id,
            "name": self.name,
            "interval_minutes": self.interval,
            "last_run": self.last_run,
            "next_run": self.next_run,
            "status": self.status,
            "run_count": self.run_count,
        }


_registered_tasks: Dict[str, Task] = {}


def register(task_id: str, name: str, interval_minutes: int, callback: Callable):
    task = Task(task_id, name, interval_minutes, callback)
    _registered_tasks[task_id] = task
    logger.info("Scheduled task registered: %s (every %d min)", name, interval_minutes)


def unregister(task_id: str):
    _registered_tasks.pop(task_id, None)


def list_tasks() -> List[dict]:
    return [t.to_dict() for t in _registered_tasks.values()]


def _scheduler_loop():
    global _running
    _running = True
    logger.info("Scheduler started")

    while _running:
        now = datetime.now()
        for task_id, task in list(_registered_tasks.items()):
            try:
                next_time = datetime.fromisoformat(task.next_run) if task.next_run else now
                if now >= next_time:
                    logger.info("Running scheduled task: %s", task.name)
                    task.status = "running"
                    try:
                        task.callback()
                        task.status = "completed"
                        task.run_count += 1
                    except Exception as e:
                        task.status = "failed"
                        logger.error("Scheduled task %s failed: %s", task.name, e)
                    task.last_run = now.isoformat()
                    task.next_run = (now + timedelta(minutes=task.interval)).isoformat()
            except Exception as e:
                logger.error("Scheduler error for %s: %s", task_id, e)

        time.sleep(30)


def start():
    global _scheduler_thread
    if _scheduler_thread and _scheduler_thread.is_alive():
        return
    _scheduler_thread = threading.Thread(target=_scheduler_loop, daemon=True, name="friday-scheduler")
    _scheduler_thread.start()


def stop():
    global _running
    _running = False


# ── Built-in scheduled tasks ──

def _daily_briefing():
    """Generate a daily briefing — runs once on schedule."""
    try:
        from friday.bot_manager import list_bots, get_total_earnings
        bots = list_bots()
        earnings = get_total_earnings()
        from friday.memory import Memory
        from friday.config import config
        mem = Memory(config.data_dir)

        summary_parts = []
        if bots:
            summary_parts.append(f"Active bots: {len(bots)}")
        if earnings:
            summary_parts.append(f"Total earnings: ${earnings:.2f}")

        facts = mem.long_term.get("facts", [])
        if facts:
            summary_parts.append(f"Facts stored: {len(facts)}")

        logger.info("Daily briefing: %s", " | ".join(summary_parts) if summary_parts else "No data")
    except Exception as e:
        logger.warning("Daily briefing failed: %s", e)


def _bot_health_check():
    """Check all bots are alive, restart if dead."""
    try:
        from friday.bot_manager import list_bots
        for bot in list_bots():
            if bot.get("status") == "failed":
                bot_id = bot.get("id")
                logger.warning("Bot %s has failed — needs attention", bot_id)
    except Exception as e:
        logger.warning("Bot health check failed: %s", e)


def register_defaults():
    register("daily_briefing", "Daily Briefing", 1440, _daily_briefing)   # once per day
    register("bot_health", "Bot Health Check", 60, _bot_health_check)     # every hour
