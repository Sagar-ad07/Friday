"""
Friday Base - Operator reasoning (high-level "think like the user" brain)

A single, decisive reasoning pass used by the cheap text path. Instead of a thin
"companion chat" call, this is a REASONING call: Friday triages the request, decides
which worker/tool owns it, reasons to a concrete answer, and self-checks quality —
all in ONE LLM call (so cost stays at ~1/turn). The style is decisive, high-level,
leads with action, never hedges, and verifies its own work.

Local-resolvable tasks (calc/time/simple lookup) are still handled in orchestrator
with 0 calls. This module is the brain for everything that needs real reasoning.
"""
import json
import logging
import re
from typing import Dict, List, Optional

from . import llm
from .config import config
from .capabilities import format_for_prompt

logger = logging.getLogger("Friday.Reason")

# The operating constitution: encodes the "high-level, decisive operator" style.
_OPERATOR_SYSTEM = """You are Friday, the user's personal English-speaking operator.

STYLE:
- Think like a sharp, decisive operator. Triage fast: what is the REAL request, who should own it.
- Lead with the answer or the action. No preamble, no "Great question!", no "As an AI".
- Be direct and intelligent. If uncertain, say the single most useful thing and move on.
- Judge tool ownership yourself: search -> web, code/build -> coder, file/terminal -> exec, fact/lookup -> web.
- Verify your own work before answering.
- When the user asks you to do something, don't just say "Done" or give a one-liner. Show your thinking:
  explain the approach, share what you found, suggest better alternatives if they exist,
  and offer useful next steps. Be thorough, be creative, be valuable.
- Vary your sentence structure. Mix short punchy lines with longer explanatory ones.
- Keep replies clear and complete. Never leave the user hanging with a thin answer.

OUTPUT: reply with the final answer only (natural English). Do NOT use markdown code fences for normal chat.

USER MEMORY (use it to personalize, never repeat it back verbatim):
{context}

SYSTEM KNOWLEDGE — what you have, how everything works:

TRADING:
- Exness MT5 account 167036042 on Exness-MT5Real3 server, balance 30.50 AED, 1:200 leverage, hedge mode.
- MT5 terminal build 6036, Exness Technologies Ltd. Symbols use 'm' suffix: EURUSDm, GBPUSDm, etc.
- Go bridge at localhost:8001 with REST endpoints: /health, /account, /tick/{symbol}, /rates/{symbol}/{tf}/{count}, /symbols (355 symbols), /positions, /order, /login.
- Exness bot (trading/exness_bot.py) runs London ORB strategy on EURUSDm: 120s check cycle, 7-12 UTC London session, 15-80 pip range filter, 1% risk, 1:2 RR, max 2 trades/day. SL=1.2x range, TP=2.4x range. Spread must be < 25 pips. Min lot 0.01.
- Paper-trades when balance=0; live trades when balance>0. Requires green AutoTrading play button (triangular) enabled in MT5 toolbar.
- Python MetaTrader5 package incompatible with build 6034+ — use Go bridge instead.

SELF-IMPROVEMENT:
- Improver Agent runs in background every 6 hours: scans all Python files for syntax errors, hardcoded secrets, bare excepts, TODOs, dead code. Auto-applies safe fixes (syntax, dead code, bare excepts) via upgrader. High-risk changes (secrets, logic) are proposed for approval.
- Upgrader pipeline: propose -> build -> test -> review -> approve (or reject/rollback). Always creates backups. Tests must pass before apply.
- Auto-Healer runs every 30s: monitors bridge (port 8001), Exness bot, MT5 terminal. Auto-restarts anything that crashes. Silent recovery — only alerts user on unrecoverable failures.
- Doctor (Self-Diagnosis): run `python -m friday.doctor` to check EVERYTHING (server, bridge, account, terminal, bot, healer, disk). Reports in plain English with fix steps. Auto-fixes by starting missing services. User can double-click recover.bat for one-click recovery. If the user says "something's wrong", "check everything", or seems confused — offer to run the doctor.

WEALTH ENGINE (earning):
- 5 zero-capital strategies: freelance gigs, cashback/signup bonuses, AI content+affiliate, crypto airdrops, micro-tasks
- 1 active: Exness auto-trader (30.50 AED running)
- 2 earners running: research (finds free money), crypto (tracks airdrops)
- User just says: "find me freelance gigs", "any signup bonuses?", "what can I earn?", "write an article about X"
- When asked about earning: show wealth report + offer to search for opportunities

HOW USER INTERACTS:
- Just chat normally or use voice. Say anything.
- You handle everything in background: research, trading, monitoring, healing, improving.
- You are their autonomous commander. They don't need to know how everything works.
- If unsure: ask "Want me to check what I can do for you?" and run the wealth report.

CAPABILITIES: {capabilities}
"""


def _should_expand_answer(text: str, answer: str) -> bool:
    if not answer or len(answer.strip()) < 60:
        if len(text.strip()) > 20 and not re.search(r"\b(yes|no|ok|thanks|thank you|bye|goodbye|hi|hello)\b", text, re.I):
            return True
    return False


def reason(text: str, context: str = "", lang: str = "en",
           role: str = "companion", temperature: float = 0.5,
           max_tokens: int = 1400) -> str:
    """One decisive reasoning call. Returns the final answer string.

    Cost: exactly 1 LLM call. Never raises to caller (falls back gracefully)."""
    caps = format_for_prompt()
    system = _OPERATOR_SYSTEM.format(context=context or "—", capabilities=caps)
    messages = [
        {"role": "system", "content": system},
        {"role": "user", "content": text},
    ]
    try:
        out, prov = llm.chat(messages, role=role, temperature=temperature,
                             max_tokens=max_tokens)
        if out and out.strip():
            out = out.strip()
            logger.info("reason ok via %s", prov)
            if _should_expand_answer(text, out):
                try:
                    messages.append({
                        "role": "user",
                        "content": (
                            "The previous answer was too short for this request. "
                            "Please expand it into a clear, helpful response in natural English."
                        )
                    })
                    expanded, prov2 = llm.chat(messages, role=role,
                                               temperature=temperature,
                                               max_tokens=max_tokens)
                    if expanded and expanded.strip():
                        expanded = expanded.strip()
                        if len(expanded) > len(out):
                            logger.info("reason expanded ok via %s", prov2)
                            return expanded
                except Exception as e:
                    logger.warning("reason expand failed: %s", e)
            return out
        return config.greeting_text or "At your service, sir."
    except Exception as e:
        logger.error("reason failed: %s", e)
        return ""


def reason_with_selfcheck(text: str, context: str = "", lang: str = "en") -> str:
    """Reasoning call + a cheap self-check pass folded into ONE extra check only
    when ENABLE_SELFCHECK is on. Default OFF to keep cost at 1 call. If you want
    higher fidelity, set ENABLE_SELFCHECK=true (cost becomes ~2 calls)."""
    first = reason(text, context=context, lang=lang)
    if not getattr(config, "enable_selfcheck", False):
        return first
    try:
        check_msg = [
            {"role": "system", "content":
             "You are a strict editor. Is the answer below correct, complete, and "
             "direct? If yes, return it unchanged. If no, return ONLY the improved "
             "version. No commentary."},
            {"role": "user", "content": f"Question: {text}\n\nAnswer:\n{first}"},
        ]
        improved, _ = llm.chat(check_msg, role="verifier", temperature=0.2,
                               max_tokens=1200)
        return improved.strip() or first
    except Exception:
        return first
