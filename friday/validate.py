"""
Friday — Startup validation.
Hard-coded environment checks that run once at boot. Fail fast with actionable messages.
"""
import logging
import os

logger = logging.getLogger("Friday.Validate")


def validate_env():
    """Check critical config at startup — never raises, always reports."""
    issues = []
    from .config import config as cfg

    if not getattr(cfg, "provider_mode", None):
        issues.append("PROVIDER_MODE is not set (use 'cloud' or 'local')")
    chains = getattr(cfg, "role_chains", {})
    if not chains:
        issues.append("No role chains configured — all LLM calls will fail")
    for role in ["companion", "reasoner", "coder"]:
        chain = chains.get(role, [])
        if not chain:
            issues.append(f"No provider chain for '{role}'")
        else:
            has_key = any(
                cfg.has_key(c.provider) for c in chain if hasattr(c, "provider")
            )
            if not has_key and cfg.provider_mode != "local":
                providers = ", ".join(
                    getattr(c, "provider", "?") for c in chain
                )
                issues.append(
                    f"'{role}' chain ({providers}) — no API key found"
                )

    sandbox = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        "data",
        "sandbox",
    )
    os.makedirs(sandbox, exist_ok=True)
    if not os.access(sandbox, os.W_OK):
        issues.append(f"Sandbox not writable: {sandbox}")

    interface = os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
        "interface",
    )
    if not os.path.isdir(interface):
        issues.append(f"Interface dir not found: {interface}")

    if issues:
        logger.warning("Startup validation found %d issue(s):", len(issues))
        for iss in issues:
            logger.warning("  ! %s", iss)
    else:
        logger.info("Startup validation passed — all checks OK")

    for iss in issues:
        logger.error("ENV: %s", iss)
    return issues


def sanitize(text: str) -> str:
    """Remove internal markup before sending to UI/user."""
    import re

    if not text:
        return text
    text = re.sub(r"\[\[tool:[^\]]+\]\]", "", text, flags=re.DOTALL)
    text = re.sub(
        r"<environment_details>.*?</environment_details>",
        "",
        text,
        flags=re.DOTALL,
    )
    text = re.sub(r"```(?:json|javascript|python|bash|text)?\n?", "", text)
    text = re.sub(r"\n?```", "", text)
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text.strip()
