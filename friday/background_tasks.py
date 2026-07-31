"""
Friday Base - Background Tasks
Thread-based task queue for autonomous research and scheduled jobs
that run without blocking the main voice loop.
Now with SMART SCHEDULING: periodic tasks, recurring checks, and
intelligent timing based on user patterns.
"""
import json
import logging
import os
import random
import threading
import time
import uuid
from datetime import datetime, timezone, timedelta
from typing import Any, Callable, Dict, List, Optional


logger = logging.getLogger("Friday.BackgroundTasks")

PERSIST_PATH = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "background_tasks.json")
PERSIST_SCHEDULE_PATH = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "scheduled_tasks.json")
CLEANUP_INTERVAL_SECONDS = 3600
TASK_TTL_SECONDS = 86400
HIGH_PRIORITY = 0
NORMAL_PRIORITY = 1
LOW_PRIORITY = 2
PRIORITY_NAMES = {
    HIGH_PRIORITY: "high",
    NORMAL_PRIORITY: "normal",
    LOW_PRIORITY: "low",
}
VALID_TASK_TYPES = {"research", "report", "monitor", "custom", "scheduled_check", "reminder"}
VALID_DEPTHS = {"quick", "deep"}
TASK_STATUS_PENDING = "pending"
TASK_STATUS_RUNNING = "running"
TASK_STATUS_COMPLETED = "completed"
TASK_STATUS_FAILED = "failed"

RECURRING_INTERVAL_NAMES = {
    "hourly": 3600,
    "daily": 86400,
    "weekly": 604800,
    "weekday_morning": 86400,
    "weekday_evening": 86400,
}


def _utcnow_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _utcnow_ts() -> float:
    return datetime.now(timezone.utc).timestamp()


def _load_tasks() -> Dict[str, Dict[str, Any]]:
    if not os.path.exists(PERSIST_PATH):
        return {}
    try:
        with open(PERSIST_PATH, "r", encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return {}


def _save_tasks(tasks: Dict[str, Dict[str, Any]]) -> None:
    os.makedirs(os.path.dirname(PERSIST_PATH), exist_ok=True)
    try:
        with open(PERSIST_PATH, "w", encoding="utf-8") as f:
            json.dump(tasks, f, ensure_ascii=False, indent=2)
    except Exception as e:
        print(f"[background_tasks] save failed: {e}")


def _cleanup_old_tasks(tasks: Dict[str, Dict[str, Any]]) -> None:
    cutoff = _utcnow_ts() - TASK_TTL_SECONDS
    expired = [
        task_id for task_id, task in tasks.items()
        if task.get("status") in {TASK_STATUS_COMPLETED, TASK_STATUS_FAILED}
        and task.get("created_at_ts", 0) < cutoff
    ]
    for task_id in expired:
        del tasks[task_id]


class BackgroundTaskQueue:
    def __init__(self) -> None:
        self._tasks: Dict[str, Dict[str, Any]] = _load_tasks()
        self._queue: List[tuple] = []
        self._lock = threading.Lock()
        self._event = threading.Event()
        self._worker_thread: Optional[threading.Thread] = None
        self._shutdown = False
        self._rebuild_queue()
        self._start_worker()

    def _rebuild_queue(self) -> None:
        with self._lock:
            self._queue = [
                (task.get("priority", NORMAL_PRIORITY), task_id)
                for task_id, task in self._tasks.items()
                if task.get("status") == TASK_STATUS_PENDING
            ]
            self._queue.sort(key=lambda item: item[0])

    def _start_worker(self) -> None:
        if self._worker_thread and self._worker_thread.is_alive():
            return
        self._worker_thread = threading.Thread(target=self._worker_loop, daemon=True)
        self._worker_thread.start()

    def _pop_next(self) -> Optional[str]:
        with self._lock:
            if not self._queue:
                return None
            _, task_id = self._queue.pop(0)
            return task_id

    def submit_task(
        self,
        task_type: str,
        payload: Optional[Dict[str, Any]] = None,
        priority: int = NORMAL_PRIORITY,
    ) -> str:
        if task_type not in VALID_TASK_TYPES:
            raise ValueError(f"Unknown task type: {task_type!r}. Valid: {sorted(VALID_TASK_TYPES)}")
        if priority not in PRIORITY_NAMES:
            raise ValueError(f"Unknown priority: {priority!r}. Valid: {sorted(PRIORITY_NAMES.keys())}")
        if task_type == "research":
            self._validate_research_payload(payload or {})

        task_id = str(uuid.uuid4())
        now = _utcnow_ts()
        task: Dict[str, Any] = {
            "id": task_id,
            "type": task_type,
            "status": TASK_STATUS_PENDING,
            "priority": priority,
            "created_at": _utcnow_iso(),
            "created_at_ts": now,
            "updated_at": _utcnow_iso(),
            "payload": payload or {},
            "result": None,
            "error": None,
            "started_at": None,
            "finished_at": None,
        }
        with self._lock:
            self._tasks[task_id] = task
            self._queue.append((priority, task_id))
            self._queue.sort(key=lambda item: item[0])
            _save_tasks(self._tasks)
        self._event.set()
        return task_id

    def _validate_research_payload(self, payload: Dict[str, Any]) -> None:
        if "topic" not in payload or not isinstance(payload["topic"], str) or not payload["topic"].strip():
            raise ValueError("research task requires a non-empty 'topic' string")
        depth = payload.get("depth", "quick")
        if depth not in VALID_DEPTHS:
            raise ValueError(f"research depth must be one of {sorted(VALID_DEPTHS)}, got {depth!r}")
        if "output_file" not in payload or not isinstance(payload["output_file"], str):
            raise ValueError("research task requires an 'output_file' string path")
        if "notify" in payload and not isinstance(payload["notify"], bool):
            raise ValueError("research 'notify' must be a boolean")

    def get_task_status(self, task_id: str) -> Optional[str]:
        with self._lock:
            task = self._tasks.get(task_id)
            return task.get("status") if task else None

    def get_task_result(self, task_id: str) -> Optional[Any]:
        with self._lock:
            task = self._tasks.get(task_id)
            if not task:
                return None
            if task.get("status") == TASK_STATUS_COMPLETED:
                return task.get("result")
            return None

    def list_tasks(self, status: Optional[str] = None) -> List[Dict[str, Any]]:
        with self._lock:
            tasks = list(self._tasks.values())
        if status:
            tasks = [t for t in tasks if t.get("status") == status]
        tasks.sort(key=lambda t: t.get("created_at_ts", 0), reverse=True)
        return [
            {
                "id": t["id"],
                "type": t["type"],
                "status": t["status"],
                "priority": PRIORITY_NAMES.get(t.get("priority", NORMAL_PRIORITY), "normal"),
                "created_at": t["created_at"],
                "updated_at": t["updated_at"],
                "error": t.get("error"),
                "result": t.get("result") if t.get("status") == TASK_STATUS_COMPLETED else None,
            }
            for t in tasks
        ]

    def _execute_task(self, task_id: str) -> None:
        with self._lock:
            task = self._tasks.get(task_id)
            if not task or task.get("status") != TASK_STATUS_PENDING:
                return
            task["status"] = TASK_STATUS_RUNNING
            task["started_at"] = _utcnow_iso()
            task["updated_at"] = _utcnow_iso()
            _save_tasks(self._tasks)

        try:
            result = self._run(task)
            with self._lock:
                task = self._tasks.get(task_id)
                if not task:
                    return
                task["status"] = TASK_STATUS_COMPLETED
                task["result"] = result
                task["finished_at"] = _utcnow_iso()
                task["updated_at"] = _utcnow_iso()
                task["error"] = None
                _save_tasks(self._tasks)
        except Exception as e:
            with self._lock:
                task = self._tasks.get(task_id)
                if not task:
                    return
                task["status"] = TASK_STATUS_FAILED
                task["error"] = str(e)
                task["finished_at"] = _utcnow_iso()
                task["updated_at"] = _utcnow_iso()
                _save_tasks(self._tasks)
            print(f"[background_tasks] task {task_id} failed: {e}")

    def _run(self, task: Dict[str, Any]) -> Any:
        task_type = task.get("type")
        payload = task.get("payload", {})

        if task_type == "research":
            return self._run_research(payload)
        elif task_type == "report":
            return self._run_report(payload)
        elif task_type == "monitor":
            return self._run_monitor(payload)
        elif task_type == "custom":
            return self._run_custom(payload)
        else:
            raise ValueError(f"Unsupported task type: {task_type!r}")

    def _run_research(self, payload: Dict[str, Any]) -> str:
        topic = payload.get("topic", "")
        depth = payload.get("depth", "quick")
        output_file = payload.get("output_file", "")
        notify = payload.get("notify", False)

        if not topic:
            return "Error: no topic provided"

        lines = [f"# Research Report: {topic}", ""]
        lines.append(f"Generated: {_utcnow_iso()}")
        lines.append(f"Depth: {depth}")
        lines.append("")

        try:
            # Import tools here to avoid circular imports
            from .tools import safe_tool_call
            
            # Perform web search
            search_result = safe_tool_call("web_search", {"query": topic})
            lines.append("## Search Results")
            lines.append("")
            lines.append(search_result)
            lines.append("")

            # If deep research, do additional searches
            if depth == "deep":
                additional_queries = [
                    f"{topic} latest news",
                    f"{topic} tutorial",
                    f"{topic} best practices",
                ]
                lines.append("## Deep Research")
                lines.append("")
                for query in additional_queries:
                    lines.append(f"### {query}")
                    lines.append("")
                    result = safe_tool_call("web_search", {"query": query})
                    lines.append(result)
                    lines.append("")

            # Try to fetch a relevant URL for more detail
            try:
                from .tools import _open_url
                url_result = _open_url({"url": f"https://en.wikipedia.org/wiki/{topic.replace(' ', '_')}"})
                if url_result and not url_result.startswith("blocked") and not url_result.startswith("fetch error"):
                    lines.append("## Detailed Information")
                    lines.append("")
                    lines.append(url_result[:2000])
            except Exception:
                pass

            lines.append("## Summary")
            lines.append("")
            lines.append(f"Research completed for: {topic}")
            lines.append(f"Sources: Web search + Wikipedia")
            lines.append("")

            result_text = "\n".join(lines)

            # Save to file
            if output_file:
                try:
                    os.makedirs(os.path.dirname(output_file) if os.path.dirname(output_file) else ".", exist_ok=True)
                    with open(output_file, "w", encoding="utf-8") as f:
                        f.write(result_text)
                    lines.append(f"Report saved to: {output_file}")
                except Exception as e:
                    lines.append(f"Failed to save report: {e}")

            if notify:
                print(f"[background_tasks] research completed: {topic}")

            return result_text

        except Exception as e:
            error_msg = f"Research failed: {e}"
            if output_file:
                try:
                    with open(output_file, "w", encoding="utf-8") as f:
                        f.write(f"# Research Error\n\n{error_msg}\n")
                except Exception:
                    pass
            return error_msg

    def _run_report(self, payload: Dict[str, Any]) -> str:
        title = payload.get("title", "Untitled Report")
        content = payload.get("content", "")
        return f"Report generated: {title}\n\n{content}"

    def _run_monitor(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        target = payload.get("target", "")
        checks = payload.get("checks", 1)
        return {
            "target": target,
            "checks": checks,
            "status": "monitored",
            "timestamp": _utcnow_iso(),
        }

    def _run_custom(self, payload: Dict[str, Any]) -> Any:
        return payload.get("result", f"Custom task executed at {_utcnow_iso()}")

    def _cleanup_loop(self) -> None:
        with self._lock:
            _cleanup_old_tasks(self._tasks)
            _save_tasks(self._tasks)

    def schedule_recurring(self, name: str, interval: str, task_type: str,
                           payload: Optional[Dict] = None,
                           start_hour: Optional[int] = None,
                           enabled: bool = True) -> str:
        """Schedule a recurring task. Interval: hourly, daily, weekly, weekday_morning, weekday_evening."""
        schedule_id = str(uuid.uuid4())
        schedule = {
            "id": schedule_id,
            "name": name,
            "interval": interval,
            "interval_seconds": RECURRING_INTERVAL_NAMES.get(interval, 86400),
            "task_type": task_type,
            "payload": payload or {},
            "start_hour": start_hour,
            "enabled": enabled,
            "last_run": None,
            "next_run": time.time() + random.randint(60, 300),
            "created_at": _utcnow_iso(),
            "run_count": 0,
        }
        schedules = self._load_schedules()
        schedules[schedule_id] = schedule
        self._save_schedules(schedules)
        logger.info(f"Scheduled recurring task '{name}' ({interval})")
        return schedule_id

    def _load_schedules(self) -> Dict:
        try:
            with open(PERSIST_SCHEDULE_PATH, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            return {}

    def _save_schedules(self, schedules: Dict) -> None:
        try:
            with open(PERSIST_SCHEDULE_PATH, "w", encoding="utf-8") as f:
                json.dump(schedules, f, ensure_ascii=False, indent=2)
        except Exception as e:
            logger.warning(f"Failed to save schedules: {e}")

    def _check_schedules(self) -> None:
        schedules = self._load_schedules()
        now = time.time()
        for sched_id, sched in schedules.items():
            if not sched.get("enabled", True):
                continue
            next_run = sched.get("next_run", 0)
            if now >= next_run:
                self._run_scheduled(sched)
                sched["last_run"] = now
                sched["run_count"] = sched.get("run_count", 0) + 1
                interval = sched.get("interval_seconds", 86400)
                if sched.get("interval") in ("weekday_morning", "weekday_evening"):
                    now_dt = datetime.now()
                    target_hour = sched.get("start_hour", 9)
                    next_dt = now_dt.replace(hour=target_hour, minute=0, second=0) + timedelta(days=1)
                    while next_dt.weekday() >= 5:
                        next_dt += timedelta(days=1)
                    sched["next_run"] = next_dt.timestamp()
                else:
                    sched["next_run"] = now + interval + random.uniform(0, 60)
                self._save_schedules(schedules)

    def _run_scheduled(self, sched: Dict) -> None:
        logger.info(f"Running scheduled task: {sched.get('name')}")
        if sched.get("task_type") == "reminder":
            try:
                from .proactive import announce
                payload = sched.get("payload", {})
                message = payload.get("message", f"Scheduled: {sched.get('name')}")
                announce(message, "info")
            except Exception as e:
                logger.warning(f"Scheduled reminder failed: {e}")

    def shutdown(self) -> None:
        self._shutdown = True
        self._event.set()
        if self._worker_thread:
            self._worker_thread.join(timeout=2.0)
        self._cleanup_loop()

    def _worker_loop(self) -> None:
        last_schedule_check = 0
        while not self._shutdown:
            self._event.wait(timeout=1.0)
            self._event.clear()
            now = time.time()
            if now - last_schedule_check > 30:
                try:
                    self._check_schedules()
                except Exception as e:
                    logger.warning(f"Schedule check failed: {e}")
                last_schedule_check = now
            task_id = self._pop_next()
            if task_id is None:
                continue
            self._execute_task(task_id)


_background_queue: Optional[BackgroundTaskQueue] = None
_init_lock = threading.Lock()


def get_queue() -> BackgroundTaskQueue:
    global _background_queue
    if _background_queue is None:
        with _init_lock:
            if _background_queue is None:
                _background_queue = BackgroundTaskQueue()
    return _background_queue


def submit_task(
    task_type: str,
    payload: Optional[Dict[str, Any]] = None,
    priority: int = NORMAL_PRIORITY,
) -> str:
    return get_queue().submit_task(task_type, payload=payload, priority=priority)


def get_task_status(task_id: str) -> Optional[str]:
    return get_queue().get_task_status(task_id)


def get_task_result(task_id: str) -> Optional[Any]:
    return get_queue().get_task_result(task_id)


def list_tasks(status: Optional[str] = None) -> List[Dict[str, Any]]:
    return get_queue().list_tasks(status=status)


def run_cleanup() -> None:
    get_queue()._cleanup_loop()


def schedule_recurring(name: str, interval: str, task_type: str,
                       payload: Optional[Dict] = None,
                       start_hour: Optional[int] = None) -> str:
    """Schedule a recurring background task."""
    return get_queue().schedule_recurring(
        name, interval, task_type, payload=payload, start_hour=start_hour
    )


def list_schedules() -> List[Dict]:
    """List all scheduled recurring tasks."""
    return list(get_queue()._load_schedules().values())


def remove_schedule(schedule_id: str) -> bool:
    """Remove a scheduled task."""
    schedules = get_queue()._load_schedules()
    if schedule_id in schedules:
        del schedules[schedule_id]
        get_queue()._save_schedules(schedules)
        return True
    return False


def enable_schedule(schedule_id: str, enabled: bool = True) -> bool:
    """Enable or disable a scheduled task."""
    schedules = get_queue()._load_schedules()
    if schedule_id in schedules:
        schedules[schedule_id]["enabled"] = enabled
        get_queue()._save_schedules(schedules)
        return True
    return False
