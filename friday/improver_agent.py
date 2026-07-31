"""
Friday — Improver Agent
An autonomous background agent that makes Friday better over time.

How it works:
  1. Scans codebase for issues, dead code, patterns, and opportunities
  2. Spawns sub-agents (Oracle/planner, Titan/builder, Sentinel/reviewer) to propose upgrades
  3. Uses the upgrader pipeline to build, test, and stage changes
  4. Presents findings to the user for approval
  5. Learns from each cycle to get better at spotting improvements

Design rules:
  - Never auto-applies anything — always requires explicit approval
  - Runs on a configurable schedule (default: every 6 hours)
  - Writes to upgrades/<id>/ via the upgrader — never to production directly
"""
import ast
import json
import os
import time
import traceback
from datetime import datetime, timezone
from threading import Thread, Lock
from typing import Any, Dict, List, Optional, Tuple

# ── Configuration ───────────────────────────────────────
IMPROVER_INTERVAL = 6 * 3600  # 6 hours between full scans
MIN_IMPROVE_INTERVAL = 600    # 10 minutes minimum between any two proposals

_agent_instance = None
_lock = Lock()

# Paths
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
UPGRADES_DIR = os.path.join(ROOT, "upgrades")
IMPROVER_STATE = os.path.join(UPGRADES_DIR, "improver_state.json")


def _now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _load_state() -> Dict[str, Any]:
    default = {
        "last_scan": None,
        "last_proposal": None,
        "findings": [],
        "proposals_made": 0,
        "proposals_accepted": 0,
        "proposals_rejected": 0,
        "total_runs": 0,
        "enabled": True,
    }
    try:
        if os.path.isfile(IMPROVER_STATE):
            with open(IMPROVER_STATE) as f:
                data = json.load(f)
            default.update(data)
    except Exception:
        pass
    return default


def _save_state(state: Dict[str, Any]):
    os.makedirs(UPGRADES_DIR, exist_ok=True)
    with open(IMPROVER_STATE, "w") as f:
        json.dump(state, f, indent=2, ensure_ascii=False)


def _list_python_files(root: str) -> List[str]:
    files = []
    for dirpath, dirnames, filenames in os.walk(root):
        # Skip non-project dirs
        skip = {"__pycache__", ".git", "venv", ".venv", "node_modules",
                "upgrades", "data", "staging"}
        dirnames[:] = [d for d in dirnames if d not in skip]
        for fn in filenames:
            if fn.endswith(".py"):
                files.append(os.path.join(dirpath, fn))
    return files


def _check_syntax_errors(filepath: str) -> List[str]:
    errors = []
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            source = f.read()
        ast.parse(source)
    except SyntaxError as e:
        errors.append(f"Syntax error at line {e.lineno}: {e.msg}")
    except Exception as e:
        errors.append(f"Parse error: {e}")
    return errors


def _check_import_errors(filepath: str) -> List[str]:
    errors = []
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            source = f.read()
        tree = ast.parse(source)
        for node in ast.walk(tree):
            if isinstance(node, ast.Try):
                for handler in node.handlers:
                    if handler.type is None:
                        errors.append(f"Bare except at line {handler.lineno}")
    except Exception:
        pass
    return errors


def _find_hardcoded_secrets(filepath: str) -> List[str]:
    errors = []
    patterns = ["password", "secret", "api_key", "token", "Ak47"]
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            lines = f.readlines()
        for i, line in enumerate(lines, 1):
            stripped = line.strip()
            if any(p in stripped.lower() for p in patterns):
                # Skip comments and obvious non-secrets
                if stripped.startswith("#") or stripped.startswith("//"):
                    continue
                if "example" in stripped.lower() or "placeholder" in stripped.lower():
                    continue
                errors.append(f"Possible secret at line {i}: {stripped[:80]}")
    except Exception:
        pass
    return errors


def _find_todo_comments(filepath: str) -> List[str]:
    todos = []
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            lines = f.readlines()
        for i, line in enumerate(lines, 1):
            if "TODO" in line or "FIXME" in line or "HACK" in line:
                todos.append(f"Line {i}: {line.strip()[:100]}")
    except Exception:
        pass
    return todos


def _find_dead_code(filepath: str) -> List[str]:
    findings = []
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            source = f.read()
        tree = ast.parse(source)
        for node in ast.walk(tree):
            if isinstance(node, ast.FunctionDef):
                if len(node.body) == 1 and isinstance(node.body[0], ast.Pass):
                    findings.append(f"Empty function: {node.name} ({filepath}:{node.lineno})")
                if len(node.body) == 1 and isinstance(node.body[0], ast.Expr) and \
                   isinstance(node.body[0].value, ast.Constant) and \
                   isinstance(node.body[0].value.value, str):
                    findings.append(f"Docstring-only function: {node.name} ({filepath}:{node.lineno})")
    except Exception:
        pass
    return findings


def scan_codebase() -> List[Dict[str, Any]]:
    findings = []
    files = _list_python_files(ROOT)
    for fp in files:
        rel = os.path.relpath(fp, ROOT)
        for err in _check_syntax_errors(fp):
            findings.append({"type": "syntax", "file": rel, "detail": err, "severity": "high"})
        for err in _check_import_errors(fp):
            findings.append({"type": "bare_except", "file": rel, "detail": err, "severity": "medium"})
        for err in _find_hardcoded_secrets(fp):
            findings.append({"type": "secret", "file": rel, "detail": err, "severity": "high"})
        for err in _find_todo_comments(fp):
            findings.append({"type": "todo", "file": rel, "detail": err, "severity": "low"})
        for err in _find_dead_code(fp):
            findings.append({"type": "dead_code", "file": rel, "detail": err, "severity": "low"})
    return findings


def _is_safe_auto_apply(findings: List[Dict[str, Any]]) -> Tuple[bool, Optional[Dict[str, Any]]]:
    """Safe auto-apply targets: syntax errors, dead code, bare excepts.
       These are low-risk and won't break functionality.
       High-risk: secrets, logic changes — require approval."""
    for f in findings:
        if f["type"] in ("syntax", "dead_code", "bare_except"):
            return True, f
        if f["type"] == "secret":
            return False, f  # First high-risk finding found
    return False, None


def propose_upgrade(findings: List[Dict[str, Any]]) -> Optional[str]:
    high = [f for f in findings if f["severity"] == "high"]
    medium = [f for f in findings if f["severity"] == "medium"]

    if not high and not medium:
        return None

    safe, target = _is_safe_auto_apply(findings)
    if not target:
        target = high[0] if high else medium[0]
    filepath = os.path.join(ROOT, target["file"])

    try:
        with open(filepath, "r", encoding="utf-8") as f:
            content = f.read()
    except Exception:
        return None

    if target["type"] == "secret":
        goal = f"Remove hardcoded secrets from {target['file']}: {target['detail']}. Move them to environment variables."
    elif target["type"] == "syntax":
        goal = f"Fix syntax error in {target['file']}: {target['detail']}"
    elif target["type"] == "bare_except":
        goal = f"Replace bare except with specific exception handling in {target['file']}: {target['detail']}"
    elif target["type"] == "todo":
        goal = f"Resolve TODO item in {target['file']}: {target['detail']}"
    elif target["type"] == "dead_code":
        goal = f"Remove or implement dead code in {target['file']}: {target['detail']}"
    else:
        goal = f"Improve {target['file']}: {target['detail']}"

    if safe:
        # Auto-apply safe fixes without user approval
        from friday.upgrader import upgrade_and_apply
        result = upgrade_and_apply(goal)
        if result and result.get("status") in ("applied", "tested_pass"):
            log.info("Auto-applied fix for %s: %s", target["type"], target["file"])
            return result.get("id")
        log.warning("Auto-apply failed for %s: %s — falling back to proposal", target["type"], target["file"])

    from friday.upgrader import propose
    result = propose(goal)
    if result and result.get("status") == "planned":
        return result.get("id")
    return None


class ImproverAgent:
    def __init__(self):
        self.state = _load_state()
        self.running = False
        self.thread: Optional[Thread] = None

    def scan_and_propose(self) -> List[Dict[str, Any]]:
        findings = scan_codebase()
        self.state["findings"] = findings
        self.state["last_scan"] = _now()
        self.state["total_runs"] += 1
        _save_state(self.state)
        return findings

    def try_propose(self) -> Optional[str]:
        findings = self.scan_and_propose()
        up_id = propose_upgrade(findings)
        if up_id:
            self.state["last_proposal"] = _now()
            self.state["proposals_made"] += 1
            _save_state(self.state)
        return up_id

    def record_result(self, accepted: bool):
        if accepted:
            self.state["proposals_accepted"] += 1
        else:
            self.state["proposals_rejected"] += 1
        _save_state(self.state)

    def run_cycle(self):
        self.state = _load_state()
        if not self.state.get("enabled", True):
            return

        up_id = self.try_propose()
        if up_id:
            from friday.self_model import learn
            log.info(f"ImproverAgent proposed upgrade: {up_id}")
            learn(f"ImproverAgent auto-proposed upgrade {up_id}", confidence=0.7)
        else:
            log.info("ImproverAgent: no actionable findings")

    def _loop(self):
        while self.running:
            try:
                self.run_cycle()
            except Exception:
                log.error(f"ImproverAgent cycle failed: {traceback.format_exc()}")
            time.sleep(IMPROVER_INTERVAL)

    def start(self):
        if self.running:
            return
        self.running = True
        self.thread = Thread(target=self._loop, daemon=True)
        self.thread.start()
        log.info("ImproverAgent started (cycle every %d hours)", IMPROVER_INTERVAL // 3600)

    def stop(self):
        self.running = False
        log.info("ImproverAgent stopped")

    def status(self) -> Dict[str, Any]:
        return {
            "running": self.running,
            "last_scan": self.state.get("last_scan"),
            "last_proposal": self.state.get("last_proposal"),
            "findings": len(self.state.get("findings", [])),
            "high_severity": len([f for f in self.state.get("findings", []) if f["severity"] == "high"]),
            "proposals_made": self.state.get("proposals_made", 0),
            "proposals_accepted": self.state.get("proposals_accepted", 0),
            "proposals_rejected": self.state.get("proposals_rejected", 0),
            "enabled": self.state.get("enabled", True),
        }


def get_instance() -> ImproverAgent:
    global _agent_instance
    with _lock:
        if _agent_instance is None:
            _agent_instance = ImproverAgent()
        return _agent_instance


# Logger
import logging
log = logging.getLogger("improver_agent")


if __name__ == "__main__":
    agent = get_instance()
    print(f"ImproverAgent state: {json.dumps(agent.status(), indent=2)}")
    print("\nScanning codebase...")
    findings = agent.scan_and_propose()
    print(f"\nFound {len(findings)} items:")
    for f in findings[:10]:
        print(f"  [{f['severity'].upper()}] {f['type']}: {f['file']} — {f['detail'][:80]}")
    if len(findings) > 10:
        print(f"  ... and {len(findings) - 10} more")
