"""
Friday — Auto-Healer
Monitors all critical processes every 30s. Auto-restarts anything that crashes.
Only alerts the user if something can't be fixed. Silent recovery otherwise.

Managed processes:
  - mt5_bridge.exe (Go bridge, port 8001)
  - terminal64.exe (MT5 terminal)
  - Exness bot (trading.exness_bot)
  - Improver agent (already running in-process)
  - Friday server (FastAPI, port 8000)
"""
import logging
import os
import subprocess
import sys
import time
import traceback
from datetime import datetime, timezone
from threading import Thread, Lock
from typing import Dict, List, Optional, Tuple

import requests
import psutil

log = logging.getLogger("healer")

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHECK_INTERVAL = 30  # seconds
_HEALER_INSTANCE = None
_lock = Lock()


# Cache: avoids hammering the bridge on every check cycle
_cache = {"bridge": None, "ts": 0}


def _bridge_healthy() -> Tuple[bool, str]:
    now = time.time()
    if _cache["bridge"] and now - _cache["ts"] < 10:
        return _cache["bridge"]
    try:
        r = requests.get("http://localhost:8001/health", timeout=3)
        if r.status_code == 200:
            data = r.json()
            ok = data.get("mt5_connected", False)
            msg = f"build {data.get('build', '?')} connected" if ok else "running but MT5 disconnected"
            _cache["bridge"] = (ok, msg)
            _cache["ts"] = now
            return ok, msg
        return False, f"HTTP {r.status_code}"
    except requests.ConnectionError:
        return False, "port 8001 not reachable"
    except Exception as e:
        return False, str(e)[:60]


def _bridge_pid() -> Optional[int]:
    try:
        for p in psutil.process_iter(["pid", "name"]):
            if p.info["name"] == "mt5_bridge.exe":
                return p.info["pid"]
    except Exception:
        pass
    return None


def _bridge_start() -> Optional[int]:
    exe = os.path.join(ROOT, "friday_go", "cmd", "mt5_bridge", "mt5_bridge.exe")
    if not os.path.isfile(exe):
        log.warning("Bridge exe not found at %s", exe)
        return None
    try:
        proc = subprocess.Popen([exe], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        log.info("Bridge started (PID %d)", proc.pid)
        return proc.pid
    except Exception as e:
        log.error("Bridge start failed: %s", e)
        return None


def _bot_pid() -> Optional[int]:
    try:
        import psutil
        for p in psutil.process_iter(["pid", "name", "cmdline"]):
            c = str(p.info.get("cmdline", ""))
            if "exness_bot" in c:
                return p.info["pid"]
    except Exception:
        pass
    return None


def _bot_start() -> Optional[int]:
    try:
        proc = subprocess.Popen(
            [sys.executable, "-m", "trading.exness_bot"],
            cwd=ROOT,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        log.info("Exness bot started (PID %d)", proc.pid)
        return proc.pid
    except Exception as e:
        log.error("Bot start failed: %s", e)
        return None


def _terminal_pid() -> Optional[int]:
    try:
        import psutil
        for p in psutil.process_iter(["pid", "name"]):
            if p.info["name"] == "terminal64.exe":
                return p.info["pid"]
    except Exception:
        pass
    return None


def _terminal_start() -> Optional[int]:
    exe = r"C:\Program Files\MetaTrader 5\terminal64.exe"
    if not os.path.isfile(exe):
        log.warning("MT5 terminal not found at %s", exe)
        return None
    try:
        subprocess.Popen([exe], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        log.info("MT5 terminal started")
        return 1
    except Exception as e:
        log.error("Terminal start failed: %s", e)
        return None


class Healer:
    def __init__(self):
        self.running = False
        self.thread: Optional[Thread] = None
        self.stats = {
            "bridge_restarts": 0,
            "bot_restarts": 0,
            "terminal_restarts": 0,
            "last_check": None,
            "errors": [],
        }

    def check_and_heal(self):
        """One cycle: check all processes, restart dead ones."""
        now = datetime.now(timezone.utc).strftime("%H:%M:%S")
        self.stats["last_check"] = now

        # 1. Bridge
        b_ok, b_msg = _bridge_healthy()
        if not b_ok:
            log.warning("Bridge unhealthy: %s — restarting", b_msg)
            pid = _bridge_pid()
            if pid:
                try:
                    import psutil
                    psutil.Process(pid).terminate()
                    time.sleep(2)
                except Exception:
                    pass
            _bridge_start()
            self.stats["bridge_restarts"] += 1
        else:
            log.debug("Bridge OK (%s)", b_msg)

        # 2. Exness bot
        bp = _bot_pid()
        if bp is None:
            log.warning("Exness bot not running — restarting")
            _bot_start()
            self.stats["bot_restarts"] += 1
            self._record_error("Exness bot was down, auto-restarted")
        else:
            log.debug("Exness bot OK (PID %d)", bp)

        # 3. MT5 terminal
        tp = _terminal_pid()
        if tp is None:
            log.warning("MT5 terminal not running — restarting")
            _terminal_start()
            self.stats["terminal_restarts"] += 1
            self._record_error("MT5 terminal was down, auto-restarted")
        else:
            log.debug("MT5 terminal OK (PID %d)", tp)

    def _record_error(self, msg: str):
        self.stats["errors"].append({
            "time": datetime.now(timezone.utc).isoformat(),
            "msg": msg,
        })
        if len(self.stats["errors"]) > 100:
            self.stats["errors"] = self.stats["errors"][-100:]

    def _loop(self):
        while self.running:
            try:
                self.check_and_heal()
            except Exception as e:
                log.error("Healer cycle failed: %s", traceback.format_exc())
            time.sleep(CHECK_INTERVAL)

    def start(self):
        if self.running:
            return
        self.running = True
        self.thread = Thread(target=self._loop, daemon=True)
        self.thread.start()
        log.info("Healer started (check every %ds)", CHECK_INTERVAL)

    def stop(self):
        self.running = False
        log.info("Healer stopped")

    def status(self) -> dict:
        return {
            "running": self.running,
            "check_interval": CHECK_INTERVAL,
            **self.stats,
        }


def get_instance() -> Healer:
    global _HEALER_INSTANCE
    with _lock:
        if _HEALER_INSTANCE is None:
            _HEALER_INSTANCE = Healer()
        return _HEALER_INSTANCE


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    h = get_instance()
    print("Healer status:", h.status())
    print("\nRunning check...")
    h.check_and_heal()
    print("After check:", h.status())
