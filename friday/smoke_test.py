"""
Friday Base - smoke_test
Quick sanity checks that run without an API key.
"""
import sys
import os

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from friday.config import config
from friday import resilience, tools


def check(name, cond):
    status = "PASS" if cond else "FAIL"
    print(f"  [{status}] {name}")
    return cond


def main():
    print("=== Friday Smoke Test ===")
    ok = True

    ok &= check("config loads", config is not None)
    ok &= check("resilience.breaker exists", resilience.breaker is not None)
    ok &= check("tools registry not empty", len(tools.TOOLS) > 0)
    ok &= check("max_retries >= 3", config.max_retries >= 3)
    ok &= check("breaker_cooldown <= 30", config.breaker_cooldown <= 30)
    ok &= check("SARVAM key present (STT/TTS ready)",
                config.has_key("sarvam"))
    ok &= check("companion chain has >=1 chat provider",
                any(config.has_key(c.provider)
                    for c in config.role_chains["companion"]))
    ok &= check("NE_FALLBACK is set and helpful",
                bool(resilience.NE_FALLBACK)
                and len(resilience.NE_FALLBACK) > 20)

    print("\nResult:", "ALL PASS" if ok else "SOME FAILED")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
