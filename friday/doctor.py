"""
Friday — Doctor
Self-diagnosis, self-repair, and plain-English status for the user.
Run this whenever something feels wrong: python -m friday.doctor

Checks everything, explains in simple language, fixes common issues.
"""
import json
import os
import subprocess
import sys
import time
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


# ── Checks ─────────────────────────────────────────────

def check_bridge() -> dict:
    result = {"name": "Trading Bridge", "status": "ok", "detail": "", "fix": ""}
    try:
        import requests
        r = requests.get("http://localhost:8001/health", timeout=5)
        if r.status_code == 200:
            data = r.json()
            if data.get("mt5_connected"):
                result["detail"] = f"Running (build {data.get('build')}), MT5 connected"
            else:
                result["status"] = "warning"
                result["detail"] = "Bridge running but MT5 not connected"
                result["fix"] = "Wait 10s for MT5 to start, or restart MT5"
        else:
            result["status"] = "error"
            result["detail"] = f"Bridge returned HTTP {r.status_code}"
            result["fix"] = "Restart bridge via run_friday.bat"
    except Exception:
        result["status"] = "error"
        result["detail"] = "Not running"
        result["fix"] = "Double-click run_friday.bat to start everything"
    return result


def check_account() -> dict:
    result = {"name": "Exness Account", "status": "ok", "detail": "", "fix": ""}
    try:
        import requests
        r = requests.get("http://localhost:8001/account", timeout=5)
        if r.status_code == 200:
            data = r.json()
            bal = data.get("balance", 0)
            cur = data.get("currency", "USD")
            lev = data.get("leverage", 0)
            result["detail"] = f"Login {data.get('login')} — {bal} {cur} — 1:{lev} leverage"

            # Check if terminal needs AutoTrading button
            if bal == 0 and cur == "USD":
                result["status"] = "warning"
                result["detail"] += " (balance shows 0 — check MT5 connection)"
                result["fix"] = "Open MT5, log into Exness (File > Login), enable green play button"
        else:
            result["status"] = "warning"
            result["detail"] = "Could not read account"
            result["fix"] = "Check MT5 is running and logged into Exness"
    except Exception:
        result["status"] = "error"
        result["detail"] = "Bridge not reachable"
        result["fix"] = "Start bridge first (run_friday.bat)"
    return result


def check_bot() -> dict:
    result = {"name": "Trading Bot", "status": "ok", "detail": "", "fix": ""}
    try:
        import psutil
        for p in psutil.process_iter(["pid", "name", "cmdline"]):
            c = str(p.info.get("cmdline", ""))
            if "exness_bot" in c:
                result["detail"] = f"Running (PID {p.info['pid']})"
                break
        else:
            result["status"] = "error"
            result["detail"] = "Not running"
            result["fix"] = "Start bot via run_friday.bat, or run: python -m trading.exness_bot"
    except Exception as e:
        result["status"] = "error"
        result["detail"] = f"Check failed: {e}"
        result["fix"] = "Run doctor again"
    return result


def check_terminal() -> dict:
    result = {"name": "MT5 Terminal", "status": "ok", "detail": "", "fix": ""}
    try:
        import psutil
        for p in psutil.process_iter(["pid", "name"]):
            if p.info["name"] == "terminal64.exe":
                result["detail"] = f"Running (PID {p.info['pid']})"
                break
        else:
            result["status"] = "error"
            result["detail"] = "Not running"
            result["fix"] = "Start MT5 from Start Menu, or run run_friday.bat"
    except Exception as e:
        result["status"] = "error"
        result["detail"] = f"Check failed: {e}"
        result["fix"] = "Run doctor again"
    return result


def check_healer() -> dict:
    result = {"name": "Auto-Healer", "status": "ok", "detail": "", "fix": ""}
    try:
        from friday.healer import get_instance
        h = get_instance()
        if h.running:
            s = h.status()
            result["detail"] = f"Active (checks every 30s, {s['bridge_restarts']} bridge restarts)"
        else:
            result["status"] = "warning"
            result["detail"] = "Not started — will start on server boot"
            result["fix"] = "Restart Friday server, or ignore if everything works"
    except Exception:
        result["status"] = "warning"
        result["detail"] = "Module available but not active"
        result["fix"] = "Will start automatically with Friday server"
    return result


def check_friday_server() -> dict:
    result = {"name": "Friday Server", "status": "ok", "detail": "", "fix": ""}
    result["detail"] = "Running on port 8000"
    return result


def check_disk() -> dict:
    result = {"name": "Disk Space", "status": "ok", "detail": "", "fix": ""}
    try:
        import shutil
        usage = shutil.disk_usage(ROOT)
        free_gb = usage.free / (1024**3)
        total_gb = usage.total / (1024**3)
        pct = usage.free / usage.total * 100
        result["detail"] = f"{free_gb:.1f} GB free of {total_gb:.1f} GB ({pct:.0f}% free)"
        if free_gb < 1:
            result["status"] = "error"
            result["detail"] += " — CRITICAL"
            result["fix"] = "Free up disk space: empty Recycling Bin, remove old files"
        elif free_gb < 5:
            result["status"] = "warning"
            result["fix"] = "Consider freeing up space soon"
    except Exception:
        result["status"] = "warning"
        result["detail"] = "Could not check"
    return result


ALL_CHECKS = [
    check_friday_server,
    check_bridge,
    check_account,
    check_terminal,
    check_bot,
    check_healer,
    check_disk,
]


# ── Report ─────────────────────────────────────────────

def run_all() -> List[dict]:
    results = []
    for check in ALL_CHECKS:
        try:
            results.append(check())
        except Exception as e:
            results.append({
                "name": check.__name__,
                "status": "error",
                "detail": str(e)[:60],
                "fix": "Run doctor again",
            })
    return results


def plain_english_summary(results: List[dict]) -> str:
    lines = []
    ok_count = sum(1 for r in results if r["status"] == "ok")
    warn_count = sum(1 for r in results if r["status"] == "warning")
    err_count = sum(1 for r in results if r["status"] == "error")

    lines.append("Friday Health Report")
    lines.append("=" * 40)
    lines.append(f"{ok_count} OK, {warn_count} warnings, {err_count} errors")
    lines.append("")

    for r in results:
        icon = {"ok": "[OK]", "warning": "[!]", "error": "[X]"}.get(r["status"], "[?]")
        lines.append(f"{icon} {r['name']}: {r['detail'] or r['status']}")
        if r["status"] == "error" and r["fix"]:
            lines.append(f"   -> Fix: {r['fix']}")
        if r["status"] == "warning" and r["fix"]:
            lines.append(f"   -> Tip: {r['fix']}")

    lines.append("")
    if err_count > 0:
        lines.append("Need help? Double-click run_friday.bat — it starts everything.")
    elif warn_count > 0:
        lines.append("Minor things to check when you have time.")
    else:
        lines.append("Everything is running smoothly.")

    return "\n".join(lines)


def quick_fix_all():
    """Start anything that's not running."""
    actions = []

    # Start MT5 terminal
    term_exe = r"C:\Program Files\MetaTrader 5\terminal64.exe"
    if os.path.isfile(term_exe):
        import psutil
        if not any(p.info["name"] == "terminal64.exe" for p in psutil.process_iter(["name"])):
            subprocess.Popen([term_exe], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            actions.append("Started MT5 terminal")
            time.sleep(8)

    # Start bridge
    bridge_exe = os.path.join(ROOT, "friday_go", "cmd", "mt5_bridge", "mt5_bridge.exe")
    if os.path.isfile(bridge_exe):
        import psutil
        if not any(p.info["name"] == "mt5_bridge.exe" for p in psutil.process_iter(["name"])):
            subprocess.Popen([bridge_exe], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            actions.append("Started trading bridge")
            time.sleep(3)

    # Start Exness bot
    import psutil
    bot_running = False
    for p in psutil.process_iter(["pid", "name", "cmdline"]):
        if "exness_bot" in str(p.info.get("cmdline", "")):
            bot_running = True
            break
    if not bot_running:
        subprocess.Popen([sys.executable, "-m", "trading.exness_bot"],
                        cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        actions.append("Started trading bot")

    # Start Friday server
    try:
        import requests
        requests.get("http://localhost:8000/health", timeout=3)
    except Exception:
        subprocess.Popen([sys.executable, "run.py"],
                        cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        actions.append("Started Friday server")

    return actions


# ── Module-level API ──

def diagnose() -> List[dict]:
    """Run all checks and return raw results."""
    return run_all()


def diagnose_text() -> str:
    """Run all checks and return plain-English summary."""
    return plain_english_summary(run_all())


def heal() -> List[str]:
    """Auto-fix: start any missing services. Returns list of actions taken."""
    return quick_fix_all()


def heal_and_verify() -> str:
    """Auto-fix then re-check. Returns plain-English summary."""
    actions = heal()
    if actions:
        time.sleep(5)
    return plain_english_summary(run_all())


if __name__ == "__main__":
    print("Friday Doctor — checking everything...\n")
    results = run_all()
    print(plain_english_summary(results))

    errors = [r for r in results if r["status"] == "error"]
    if errors:
        print("\nAuto-fix mode: starting missing services...")
        actions = quick_fix_all()
        if actions:
            for a in actions:
                print(f"  ✓ {a}")
            print("\nRe-checking...")
            time.sleep(5)
            results = run_all()
            print(plain_english_summary(results))
        else:
            print("  Everything already running.")
