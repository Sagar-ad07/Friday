"""Non-fatal issue reporting for the UI (crash-free surfacing)."""
from .config import config


def collect_issues(eye: dict) -> list:
    issues = []
    if not eye.get("active", False):
        issues.append(
            "Eye is blind: no vision provider has quota right now. Chat and "
            "voice still work. Add a funded vision key so Friday can see.")
    try:
        from .eye import _VISION_CHAIN
        if not any(config.has_key(p) for p, _ in _VISION_CHAIN):
            issues.append("No vision-capable provider configured.")
    except Exception:
        pass
    return issues
