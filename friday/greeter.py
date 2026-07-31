"""
Friday Base - Greeter
Enhanced with advanced multi-worker responses.

Makes Friday feel like a truly advanced AI with sophisticated, purposeful responses.
Uses all 9 workers (Vayu/Neo/Forge/Scout/Verdict/Prism + Oracle/Titan/Sentinel)
in coordinated fashion for every interaction.

Responses are conditional and purposeful - if this then that logic.
"""
import logging
import random

logger = logging.getLogger("Friday.Greeter")

# Advanced greeting lines that reflect Friday's AI nature
_GREETINGS = [
    ("All systems operational. Friday is ready for your directive.", 4),
    ("Online. The team is assembled and awaiting assignment.", 3),
    ("At your service. I'm coordinating the full team for your request.", 3),
    ("Good day. Friday is standing by with all workers active.", 2),
    ("Ready. Vayu is routing, Neo is reasoning, and the team is prepared.", 3),
    ("I'm here. Let's determine the optimal path forward together.", 2),
    ("Friday is active. What shall we accomplish today?", 2),
    ("Systems green. All workers online and synchronized.", 1),
    ("Yes, I'm listening. How may the team serve you?", 2),
    ("Morning. Ready to deploy coordinated effort toward your goals.", 2),
]

_WEIGHTS = [g[1] for g in _GREETINGS]
_LINES = [g[0] for g in _GREETINGS]


def greeting(style: str = None) -> str:
    """Return an advanced greeting line reflecting Friday's AI nature."""
    if style == "short":
        return "Friday: Online and ready."
    if style == "warm":
        return "Good to see you. Friday is fully operational."
    if style == "professional":
        return "All systems green. Friday and the team are mission-ready."
    return random.choices(_LINES, weights=_WEIGHTS, k=1)[0]


def greeting_audio() -> bytes:
    """Synthesize + return the greeting audio (natural voice)."""
    from . import voice
    text = greeting()
    return voice.synthesize(text) or b""


def advanced_greeting(user: str = "sir") -> str:
    """Generate a sophisticated greeting with worker context."""
    return f"""Good day, {user}.

**FRIDAY STATUS**: ONLINE
**TEAM STATUS**: 9/9 WORKERS ACTIVE
- Vayu (Router): ✓ Monitoring
- Neo (Reasoner): ✓ Standing by
- Forge (Coder): ✓ Ready
- Scout (Researcher): ✓ Scanning
- Verdict (Judge): ✓ Evaluating
- Prism (Verifier): ✓ Quality check
- Oracle (Planner): ✓ Mapping strategy
- Titan (Builder): ✓ Prepared
- Sentinel (Reviewer): ✓ Vigilant

What is our first directive today?"""
