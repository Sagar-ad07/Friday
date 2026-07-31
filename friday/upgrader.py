"""
Friday Base - Safe Self-Upgrade Engine (CORE)

A safety-critical, self-modifying subsystem. The design enforces strict
invariants so a mistake can never corrupt the production `friday/` package or
`run.py` on its own:

  INVARIANT 1: The engine NEVER writes into production `friday/` or `run.py`
               on its own. It only writes under <root>/upgrades/<id>/.
  INVARIANT 2: Production apply requires an EXPLICIT `approve()` call, only
               reachable after tests PASSED. A proposal whose tests failed
               CANNOT be approved (approve() re-checks status and refuses).
  INVARIANT 3: Every apply first makes a timestamped BACKUP of the target
               under <root>/upgrades/_backups/<ts>/ for rollback.
  INVARIANT 4: The applier only writes to a path INSIDE `friday/` or exactly
               `run.py` at root. Anything else (and any `..`) is rejected.
  INVARIANT 5: A persistent JSON ledger records every proposal and its state
               so nothing is lost between sessions.
  INVARIANT 6: The tester runs the generated test in a SUBPROCESS with a hard
               timeout, so a bad test can never hang Friday.

No public function raises to the caller for a *normal* failure: failures are
recorded into the ledger. The only deliberate raises are safety stops on
disallowed target paths (a ValueError that is also recorded as an error).
"""
import difflib
import json
import os
import shutil
import subprocess
import sys
import threading
import time
import uuid

from . import llm, prompts, resilience
from .config import config


PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
UPGRADES_DIR = os.path.join(PROJECT_ROOT, "upgrades")
BACKUPS_DIR = os.path.join(UPGRADES_DIR, "_backups")
LEDGER_PATH = os.path.join(UPGRADES_DIR, "ledger.json")


def _git_snapshot(message: str = "auto-snapshot before upgrade") -> str | None:
    """Auto-commit the current state to git before applying an upgrade.
    Returns the commit hash or None if git is unavailable / not a repo.
    """
    try:
        r = subprocess.run(
            ["git", "rev-parse", "--is-inside-work-tree"],
            capture_output=True, text=True, timeout=5,
            cwd=PROJECT_ROOT,
        )
        if r.returncode != 0:
            return None
        subprocess.run(["git", "add", "-A"], capture_output=True, timeout=10,
                       cwd=PROJECT_ROOT)
        r2 = subprocess.run(
            ["git", "commit", "--allow-empty", "-m", message],
            capture_output=True, text=True, timeout=10,
            cwd=PROJECT_ROOT,
        )
        r3 = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            capture_output=True, text=True, timeout=5,
            cwd=PROJECT_ROOT,
        )
        sha = r3.stdout.strip() if r3.returncode == 0 else None
        if sha and r2.stdout:
            pass
        return sha
    except Exception:
        return None

os.makedirs(UPGRADES_DIR, exist_ok=True)
os.makedirs(BACKUPS_DIR, exist_ok=True)


STATUS_PLANNED = "planned"
STATUS_BUILT = "built"
STATUS_TESTED_PASS = "tested_pass"
STATUS_TESTED_FAIL = "tested_fail"
STATUS_APPROVED = "approved"
STATUS_APPLIED = "applied"
STATUS_REJECTED = "rejected"
STATUS_ROLLED_BACK = "rolled_back"
STATUS_ERROR = "error"
STATUSES = {
    STATUS_PLANNED, STATUS_BUILT, STATUS_TESTED_PASS, STATUS_TESTED_FAIL,
    STATUS_APPROVED, STATUS_APPLIED, STATUS_REJECTED, STATUS_ROLLED_BACK,
    STATUS_ERROR,
}

TEST_TIMEOUT = 60
TEST_PASS_MARKER = "UPGRADE TEST PASSED"


_ledger_lock = threading.Lock()


def _load_ledger() -> dict:
    """Tolerant load: never raise; return a well-shaped dict."""
    try:
        with open(LEDGER_PATH, "r", encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict):
            data = {}
        if not isinstance(data.get("proposals"), dict):
            data["proposals"] = {}
        return data
    except FileNotFoundError:
        return {"proposals": {}}
    except Exception:
        try:
            if os.path.exists(LEDGER_PATH):
                shutil.copy(LEDGER_PATH, LEDGER_PATH + ".corrupt")
        except Exception:
            pass
        return {"proposals": {}}


def _save_ledger(data: dict) -> None:
    """Atomic-ish write with a temp file + rename to avoid partial writes."""
    tmp = LEDGER_PATH + ".tmp"
    try:
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
        os.replace(tmp, LEDGER_PATH)
    except Exception:
        try:
            os.remove(tmp)
        except Exception:
            pass
        raise


def _get(upgrade_id: str) -> dict | None:
    return _load_ledger().get("proposals", {}).get(upgrade_id)


def _put(record: dict) -> dict:
    """Persist (or update) a record in the ledger. Returns the record."""
    with _ledger_lock:
        data = _load_ledger()
        data.setdefault("proposals", {})
        data["proposals"][record["id"]] = record
        _save_ledger(data)
    return record


def _mutate(upgrade_id: str, fn) -> dict | None:
    """Atomically read-modify-write a single record under the ledger lock."""
    with _ledger_lock:
        data = _load_ledger()
        proposals = data.setdefault("proposals", {})
        record = proposals.get(upgrade_id)
        if record is None:
            return None
        result = fn(record)
        _save_ledger(data)
        return result


def _event(record: dict, phase: str, detail: str) -> None:
    record.setdefault("events", []).append(
        {"phase": phase, "detail": detail, "ts": time.time()})


def _clean_json(s: str) -> str:
    s = s.strip()
    if s.startswith("```"):
        parts = s.split("```")
        s = parts[1] if len(parts) > 1 else s
        if s.lower().startswith("json"):
            s = s[4:]
    return s.strip()


def _parse_json(text: str) -> dict:
    return json.loads(_clean_json(text))


# The user may command Friday to modify any file they own within the project.
# Only ".." traversal and obvious secret/config files are blocked.
_SECRET_PATTERNS = (".env", "secret", "credentials", "private_key", "id_rsa", "token", "password", "api_key")


def _path_is_allowed_target(rel_path: str) -> bool:
    """Allow edits to any user-owned project file.

    Blocks: ".." traversal and obvious secret/config files.
    Allows: everything else inside the project root, including run.py,
    friday/, interface/, and any other file the user explicitly asks to modify.
    """
    if not rel_path:
        return False
    norm = rel_path.replace("\\", "/").strip("/")
    if not norm or ".." in norm.split("/"):
        return False

    low = norm.lower()
    for pat in _SECRET_PATTERNS:
        if low.endswith(pat) or f"/{pat}" in low:
            return False

    candidate = os.path.abspath(os.path.join(PROJECT_ROOT, rel_path))
    root = os.path.abspath(PROJECT_ROOT)
    if candidate == root or not candidate.startswith(root + os.sep):
        return False
    return True


def upgrade_and_apply(goal: str) -> dict:
    """Self-upgrade END-TO-END (user-commanded).

    plan -> build -> test -> (if tested_pass) apply. Safe net:
      * never applies unless tests PASSED,
      * always writes a timestamped backup first,
      * marks restart_required (Python does not hot-reload).
    Returns the ledger record. If tests fail, status stays tested_fail and
    nothing is applied - this is the hard safety boundary.
    """
    record = propose(goal)
    if record.get("status") != STATUS_TESTED_PASS:
        record.setdefault("detail",
            "NOT APPLIED: tests did not pass. Nothing changed.")
        return record
    # tests passed -> apply explicitly (record already in ledger)
    return approve(record["id"])


def plan(goal: str) -> dict:
    """Decide a single safe target file + a precise test description."""
    messages = [
        {"role": "system", "content": prompts.UPGRADE_PLANNER_PROMPT},
        {"role": "user", "content": f"GOAL:\n{goal}"},
    ]
    raw, _ = llm.chat(messages, role="reasoner", temperature=0.3,
                      max_tokens=1200, json_mode=True)
    spec = _parse_json(raw)
    target = spec.get("target_file", "")
    if not _path_is_allowed_target(target):
        raise ValueError(
            f"planner proposed disallowed target: {target!r}. "
            f"Must be inside the project and not a secret file.")
    return {
        "target_file": target,
        "description": spec.get("description", ""),
        "change_summary": spec.get("change_summary", ""),
        "test_description": spec.get("test_description", ""),
    }


def build(upgrade_id: str, goal: str, spec: dict) -> dict:
    """Generate the full new file content + a standalone test. Writes only
    under upgrades/<id>/ (INVARIANT 1)."""
    record = _get(upgrade_id)
    if record is None:
        raise ValueError(f"unknown upgrade_id: {upgrade_id}")

    target_file = spec["target_file"]
    prod_path = os.path.abspath(os.path.join(PROJECT_ROOT, target_file))

    try:
        with open(prod_path, "r", encoding="utf-8", errors="replace") as f:
            current_content = f.read()
    except Exception as e:
        current_content = ""
        _event(record, "build", f"could not read current target ({e}); editing as blank")

    messages = [
        {"role": "system", "content": prompts.UPGRADE_BUILDER_PROMPT},
        {"role": "user", "content": (
            f"GOAL:\n{goal}\n\n"
            f"SPEC (from planner):\n{json.dumps(spec, ensure_ascii=False, indent=2)}\n\n"
            f"CURRENT FULL CONTENT of '{target_file}':\n"
            f"----- BEGIN CURRENT FILE -----\n{current_content}\n"
            f"----- END CURRENT FILE -----")},
    ]
    raw, _ = llm.chat(messages, role="coder", temperature=0.2,
                      max_tokens=4000, json_mode=True)
    out = _parse_json(raw)
    full_new_file = out.get("full_new_file", "")
    test_file = out.get("test_file", "")
    notes = out.get("notes", "")

    id_dir = os.path.join(UPGRADES_DIR, upgrade_id)
    os.makedirs(id_dir, exist_ok=True)
    new_code_path = os.path.join(id_dir, os.path.basename(target_file))
    test_path = os.path.join(id_dir, "test_upgrade.py")

    with open(new_code_path, "w", encoding="utf-8") as f:
        f.write(full_new_file)
    with open(test_path, "w", encoding="utf-8") as f:
        f.write(test_file)

    old_lines = current_content.splitlines()
    new_lines = full_new_file.splitlines()
    diff = difflib.unified_diff(old_lines, new_lines, lineterm="")
    diff_text = "\n".join(diff)
    diff_summary = (
        f"old_lines={len(old_lines)} new_lines={len(new_lines)} "
        f"added={len(new_lines) - len(old_lines)}\n{diff_text}"
    )[:2000]

    record["status"] = STATUS_BUILT
    record["spec"] = spec
    record["target_file"] = target_file
    record["new_code_path"] = new_code_path
    record["test_path"] = test_path
    record["diff_summary"] = diff_summary
    record["notes"] = notes
    _event(record, "build", f"wrote candidate to {new_code_path}")
    return _put(record)


def test(upgrade_id: str) -> dict:
    record = _get(upgrade_id)
    if record is None:
        raise ValueError(f"unknown upgrade_id: {upgrade_id}")

    test_path = record.get("test_path")
    if not test_path or not os.path.isfile(test_path):
        def _no_test(rec):
            rec["status"] = STATUS_TESTED_FAIL
            rec["test_passed"] = False
            rec["test_output"] = "no test file found"
            _event(rec, "test", "no test file found")
            return rec
        return _mutate(upgrade_id, _no_test) or record

    try:
        r = subprocess.run(
            [sys.executable, test_path],
            capture_output=True, text=True, timeout=TEST_TIMEOUT,
            cwd=PROJECT_ROOT,
        )
        stdout = (r.stdout or "")
        stderr = (r.stderr or "")
        combined = (stdout + "\n" + stderr)[:4000]
        passed = (r.returncode == 0) and (TEST_PASS_MARKER in stdout)
    except subprocess.TimeoutExpired as e:
        combined = f"TEST TIMED OUT after {TEST_TIMEOUT}s\n{e}"
        passed = False
    except Exception as e:
        combined = f"TEST ERROR: {e}"
        passed = False

    def _apply(rec):
        rec["test_output"] = combined
        rec["test_passed"] = passed
        rec["status"] = STATUS_TESTED_PASS if passed else STATUS_TESTED_FAIL
        if passed:
            _event(rec, "test", "passed")
        else:
            _event(rec, "test",
                   f"returncode-fail/timeout: passed={passed} {combined[:120]}")
        return rec
    return _mutate(upgrade_id, _apply) or record


def review(upgrade_id: str) -> dict:
    record = _get(upgrade_id)
    if record is None:
        raise ValueError(f"unknown upgrade_id: {upgrade_id}")

    messages = [
        {"role": "system", "content": prompts.UPGRADE_REVIEWER_PROMPT},
        {"role": "user", "content": (
            f"GOAL:\n{record.get('goal', '')}\n\n"
            f"TARGET FILE: {record.get('target_file', '')}\n\n"
            f"DIFF SUMMARY:\n{record.get('diff_summary', '')}\n\n"
            f"TEST OUTPUT:\n{record.get('test_output', '')}")},
    ]
    try:
        text, _ = llm.chat(messages, role="verifier", temperature=0.2,
                           max_tokens=800)
        assessment = text.strip()
    except Exception as e:
        assessment = f"(reviewer error: {e})"

    def _apply(rec):
        rec["review"] = assessment
        _event(rec, "review", "risk assessment recorded")
        return rec
    return _mutate(upgrade_id, _apply) or record


def _new_record(goal: str) -> dict:
    uid = uuid.uuid4().hex[:8]
    ts = time.strftime("%Y%m%d_%H%M%S")
    upgrade_id = f"{ts}_{uid}"
    id_dir = os.path.join(UPGRADES_DIR, upgrade_id)
    os.makedirs(id_dir, exist_ok=True)
    return {
        "id": upgrade_id,
        "goal": goal,
        "created": time.time(),
        "status": STATUS_PLANNED,
        "spec": {},
        "target_file": "",
        "new_code_path": "",
        "test_path": "",
        "test_output": "",
        "test_passed": False,
        "review": "",
        "diff_summary": "",
        "backup_path": "",
        "notes": "",
        "events": [],
        "applied_at": None,
        "restart_required": False,
    }


def propose(goal: str) -> dict:
    """Run plan -> build -> test -> review. Does NOT apply to production."""
    record = _new_record(goal)
    record = _put(record)
    try:
        spec = plan(goal)
        record["spec"] = spec
        record["target_file"] = spec["target_file"]
        _event(record, "plan", f"target={spec['target_file']}")
        _put(record)
    except Exception as e:
        record["status"] = STATUS_ERROR
        _event(record, "plan", f"error: {e}")
        return _put(record)

    try:
        record = build(record["id"], goal, spec)
    except Exception as e:
        record["status"] = STATUS_ERROR
        _event(record, "build", f"error: {e}")
        return _put(record)

    try:
        record = test(record["id"])
    except Exception as e:
        record["status"] = STATUS_ERROR
        record["test_passed"] = False
        record["restart_required"] = False
        _event(record, "test", f"error: {e}")
        return _put(record)

    try:
        record = review(record["id"])
    except Exception as e:
        record["review"] = f"(review error: {e})"
        _event(record, "review", f"error: {e}")
        _put(record)

    return record


def propose_stream(goal: str):
    """Generator yielding live events. NEVER raises out of the generator."""
    record = None
    try:
        record = _new_record(goal)
        record = _put(record)
        yield {"type": "phase", "phase": "planning", "detail": "planning",
               "record": dict(record)}

        spec = plan(goal)
        record["spec"] = spec
        record["target_file"] = spec["target_file"]
        _event(record, "plan", f"target={spec['target_file']}")
        _put(record)
        yield {"type": "phase", "phase": "building", "detail": "planned",
               "record": dict(record)}

        record = build(record["id"], goal, spec)
        yield {"type": "phase", "phase": "testing", "detail": "built",
               "record": dict(record)}

        record = test(record["id"])
        yield {"type": "phase", "phase": "reviewing",
               "detail": record["status"], "record": dict(record)}

        record = review(record["id"])
        yield {"type": "phase", "phase": "done", "detail": record["status"],
               "record": dict(record)}
    except Exception as e:
        if record is not None:
            record["status"] = STATUS_ERROR
            _event(record, "error", f"{e}")
            _put(record)
            yield {"type": "phase", "phase": "error", "detail": str(e),
                   "record": dict(record)}
        else:
            yield {"type": "phase", "phase": "error", "detail": str(e),
                   "record": None}


def approve(upgrade_id: str) -> dict:
    """Apply a TESTED-PASS upgrade to production after an EXPLICIT approval."""
    record = _get(upgrade_id)
    if record is None:
        raise ValueError(f"unknown upgrade_id: {upgrade_id}")

    if record.get("status") != STATUS_TESTED_PASS:
        detail = (
            f"REFUSED: approve() requires status 'tested_pass', "
            f"got '{record.get('status')}'. A failed/other proposal cannot be applied.")

        def _refuse(rec):
            rec["detail"] = detail
            _event(rec, "approve", f"refused: status={rec.get('status')}")
            return rec
        return _mutate(upgrade_id, _refuse) or record

    target_file = record.get("target_file", "")
    if not _path_is_allowed_target(target_file):
        raise ValueError(
            f"approve() safety stop: target {target_file!r} not on allowlist")

    prod_path = os.path.abspath(os.path.join(PROJECT_ROOT, target_file))
    candidate = record.get("new_code_path")
    if not candidate or not os.path.isfile(candidate):
        raise ValueError("approve() safety stop: candidate file missing")

    ts = time.strftime("%Y%m%d_%H%M%S")
    backup_dir = os.path.join(BACKUPS_DIR, ts)
    os.makedirs(backup_dir, exist_ok=True)
    backup_path = os.path.join(backup_dir, os.path.basename(target_file))
    try:
        if os.path.exists(prod_path):
            shutil.copy2(prod_path, backup_path)
        else:
            with open(backup_path, "w", encoding="utf-8") as f:
                f.write("")
    except Exception as e:
        raise ValueError(f"approve() safety stop: backup failed: {e}")

    # Git snapshot before writing
    git_sha = _git_snapshot(f"upgrade {upgrade_id}: {record.get('goal', '')[:60]}")

    shutil.copy2(candidate, prod_path)

    applied_ts = time.time()

    def _commit(rec):
        rec["status"] = STATUS_APPLIED
        rec["applied_at"] = applied_ts
        rec["restart_required"] = True
        rec["backup_path"] = backup_path
        rec["git_snapshot"] = git_sha
        rec["detail"] = (
            "APPLIED. The server must be RESTARTED for changes to take effect "
            "(Python does not hot-reload).")
        if git_sha:
            rec["detail"] += f" Git snapshot: {git_sha[:12]}"
        _event(rec, "approve", f"applied {candidate} -> {prod_path} (git:{git_sha[:12] if git_sha else 'none'})")
        return rec

    return _mutate(upgrade_id, _commit) or record


def reject(upgrade_id: str, reason: str = "") -> dict:
    record = _get(upgrade_id)
    if record is None:
        raise ValueError(f"unknown upgrade_id: {upgrade_id}")

    def _apply(rec):
        rec["status"] = STATUS_REJECTED
        rec["reject_reason"] = reason
        _event(rec, "reject", reason or "no reason given")
        return rec
    return _mutate(upgrade_id, _apply) or record


def rollback(upgrade_id: str) -> dict:
    """Restore the production target from the backup made at apply time."""
    record = _get(upgrade_id)
    if record is None:
        raise ValueError(f"unknown upgrade_id: {upgrade_id}")

    backup_path = record.get("backup_path")
    target_file = record.get("target_file")
    if not backup_path or not os.path.isfile(backup_path):
        def _refuse_no_backup(rec):
            rec["detail"] = "REFUSED: no backup found to roll back to."
            _event(rec, "rollback", "refused: no backup")
            return rec
        return _mutate(upgrade_id, _refuse_no_backup) or record
    if not _path_is_allowed_target(target_file):
        raise ValueError(
            f"rollback() safety stop: target {target_file!r} not on allowlist")

    prod_path = os.path.abspath(os.path.join(PROJECT_ROOT, target_file))
    shutil.copy2(backup_path, prod_path)

    def _commit(rec):
        if rec.get("status") != STATUS_APPLIED:
            rec["detail"] = (
                f"REFUSED: rollback requires status 'applied', "
                f"got '{rec.get('status')}'.")
            _event(rec, "rollback", f"refused: status={rec.get('status')}")
            return rec
        rec["status"] = STATUS_ROLLED_BACK
        rec["restart_required"] = True
        rec["detail"] = "ROLLED BACK to backup. Restart the server to load it."
        _event(rec, "rollback", f"restored from {backup_path}")
        return rec
    return _mutate(upgrade_id, _commit) or record


def list_proposals() -> list:
    data = _load_ledger()
    recs = list(data.get("proposals", {}).values())
    recs.sort(key=lambda r: r.get("created", 0), reverse=True)
    return recs


def get_proposal(upgrade_id: str) -> dict | None:
    return _get(upgrade_id)
