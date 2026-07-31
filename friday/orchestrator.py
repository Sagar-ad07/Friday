"""
Friday Base - Orchestrator
Two paths:
  * orchestrate(...)        -> quick chat path (router -> workers -> verifier).
  * handle_turn(...)        -> intent classifier that routes to chat OR tasks.
  * agentic_run(...)        -> REAL agentic tool-execution loop (reasoner +
                             tools). This is what makes Friday ACT, not echo.
The whole module is crash-proof: exceptions become an English fallback.
"""
import json
import logging
import os
import re
import threading
import time
from typing import Dict, List

from . import llm, prompts, workers, resilience, tools
from .config import config
from . import learning as _learning
from . import anticipate as _ant
from . import self_model as _self_model
from . import ambient as _ambient

logger = logging.getLogger("Friday.Orchestrator")


def _sanitize(text: str) -> str:
    """Strip internal markup that must never reach the user:
      - [[tool:name|{args}]] markers
      - <environment_details>...</environment_details> blocks
      - stray ```json / ``` fences that wrapped internal structures
    """
    if not text:
        return text
    # Remove tool-call markers.
    text = re.sub(r"\[\[tool:[^\]]+\]\]", "", text, flags=re.DOTALL)
    # Remove environment/details blocks.
    text = re.sub(r"<environment_details>.*?</environment_details>", "", text, flags=re.DOTALL)
    # Remove code fences that wrapped JSON/internal content.
    text = re.sub(r"```(?:json|javascript|python|bash|text)?\n?", "", text)
    text = re.sub(r"\n?```", "", text)
    # Collapse extra whitespace left by removals.
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text.strip()

_CANCEL = {}
_CANCEL_LOCK = threading.Lock()
_CANCEL_SEEN = 0
_CANCEL_MAX = 500


def _confirm_prompt(action_name: str, args: dict) -> str:
    return (
        f"Friday needs your confirmation before `{action_name}` "
        f"with args `{args}`. Reply 'approve' to continue or 'deny' to cancel."
    )


def _new_cancel(run_id):
    global _CANCEL_SEEN
    ev = threading.Event()
    with _CANCEL_LOCK:
        _CANCEL[run_id] = ev
        _CANCEL_SEEN += 1
        # bounded dict: prune oldest entries when headroom is low
        if _CANCEL_SEEN > _CANCEL_MAX:
            keys = list(_CANCEL.keys())
            for old in keys[: _CANCEL_MAX // 4]:
                _CANCEL.pop(old, None)
            _CANCEL_SEEN = len(_CANCEL)
    return ev


def _clear_cancel(run_id):
    with _CANCEL_LOCK:
        _CANCEL.pop(run_id, None)


def request_cancel(run_id: str) -> bool:
    """Set the cancel flag for a running agentic stream/approval-resume."""
    with _CANCEL_LOCK:
        ev = _CANCEL.get(run_id)
    if ev is None:
        return False
    ev.set()
    return True


# Pending approvals for destructive actions (confirm-not-deny gate).
_PENDING = {}
_PENDING_LOCK = threading.Lock()
_PENDING_SEEN = 0
_PENDING_MAX = 200


def list_pending_confirmations() -> list:
    with _PENDING_LOCK:
        return [{"run_id": k, "action": v.get("action"), "args": v.get("args"), "user_text": v.get("user_text", "")}
                for k, v in _PENDING.items()]


def _store_pending(run_id: str, state: dict):
    global _PENDING_SEEN
    with _PENDING_LOCK:
        _PENDING[run_id] = state
        _PENDING_SEEN += 1
        if _PENDING_SEEN > _PENDING_MAX:
            keys = list(_PENDING.keys())
            for old in keys[: _PENDING_MAX // 4]:
                _PENDING.pop(old, None)
            _PENDING_SEEN = len(_PENDING)


def _take_pending(run_id: str):
    with _PENDING_LOCK:
        return _PENDING.pop(run_id, None)


def resume_after_approval(run_id: str, approved: bool):
    """Resume an agentic run after the user answered a confirmation prompt.

    Yields SSE-style events. If approved, executes the destructive tool and
    continues the loop; if declined, tells the model to proceed another way.
    """
    state = _take_pending(run_id)
    if state is None:
        yield {"type": "error", "message": "No pending action for this run."}
        yield {"type": "final", "reply": resilience.EN_FALLBACK}
        return

    messages = list(state.get("messages", []))
    text = state.get("text", "")
    action = state.get("action")
    args = state.get("args", {}) or {}
    cancel = _CANCEL.get(run_id)

    yield {"type": "start", "text": state.get("user_text", ""), "lang": state.get("lang", "en")}

    if not approved:
        verdict = ("The user declined that action. Do NOT repeat it. Find another safe way "
                   "to accomplish the goal, or just explain what you would have done. "
                   "Provide your final answer with done:true.")
    else:
        try:
            result = tools.execute_approved(action, args)
        except Exception as e:
            result = f"tool error ({action}): {e}"
        yield {"type": "result", "action": action, "result": result}
        messages.append({"role": "assistant", "content": text})
        messages.append({"role": "user", "content":
            f"Tool result [{action['action']}]:\n{result}\n\n"
            f"Provide your final answer or continue working (done:true)."})
        verdict = None

    if verdict:
        messages.append({"role": "user", "content": verdict})

    steps = []
    final = None
    deadline = time.time() + getattr(config, "turn_deadline_seconds", 45)

    if not approved:
        try:
            t2, prov = _reasoner_chat(messages)
            act2 = parse_action(t2)
            final = act2.get("answer") or t2
        except Exception as e:
            logger.error("resume_after_approval (declined) failed: %s", e)
            final = resilience.EN_FALLBACK
        yield {"type": "final", "reply": _verify(state.get("user_text", ""), final or resilience.EN_FALLBACK, state.get("lang", "en"))}
        _clear_cancel(run_id)
        return

    for _ in range(4):
        if time.time() > deadline:
            final = final or resilience.EN_FALLBACK
            break
        if cancel is not None and cancel.is_set():
            yield {"type": "cancelled"}
            final = final or "Stopped."
            break
        try:
            t2, prov = _reasoner_chat(messages)
        except Exception as e:
            logger.error("resume_after_approval failed: %s", e)
            final = resilience.EN_FALLBACK
            break
        act2 = parse_action(t2)
        entry = {"thought": act2.get("thought", ""), "action": act2.get("action"),
                 "args": act2.get("args", {}), "provider": prov}
        steps.append(entry)
        yield {"type": "thought", "thought": act2.get("thought"), "provider": prov}
        if act2.get("done") or not act2.get("action"):
            final = act2.get("answer") if act2.get("answer") else t2
            break
        yield {"type": "action", "action": act2["action"], "args": act2["args"]}
        res2 = tools.safe_tool_call(act2["action"], act2["args"])
        if run_id and tools.is_confirmation_result(res2):
            conf = tools.parse_confirmation(res2)
            _store_pending(run_id, {"action": conf.get("action"), "args": conf.get("args"),
                                    "messages": messages, "text": t2,
                                    "user_text": state.get("user_text", ""),
                                    "lang": state.get("lang", "en"),
                                    "context": state.get("context")})
            yield {"type": "confirm", "run_id": run_id,
                   "action": conf.get("action"), "args": conf.get("args")}
            yield {"type": "final", "reply": "I need your okay before I do that. Confirm? (yes/no)"}
            return
        entry["result"] = res2
        yield {"type": "result", "action": act2["action"], "result": res2}
        messages.append({"role": "assistant", "content": t2})
        messages.append({"role": "user", "content":
            f"Tool result [{act2['action']}]:\n{res2}\n\n"
            f"Provide the next step or final answer (done:true)."})

    if final is None:
        final = resilience.EN_FALLBACK
    yield {"type": "final", "reply": _verify(state.get("user_text", ""), final or resilience.EN_FALLBACK, state.get("lang", "en"))}
    _clear_cancel(run_id)


_REASONER_SYSTEM = prompts.WORKER_PROMPTS["reasoner"] + """

You have access to various tools. Use the tools listed below whenever you need
to perform a task or gather information.

Tools (JSON schema):
{schema}

Rules:
- Use the "thought" field to think through the problem: analyze what's needed,
  consider different approaches, plan the steps. The user can see your thinking
  process, so make it useful — explain WHY you chose each approach.
- ACT FIRST: if a tool can directly satisfy the request, set "action" to that
  tool name and "args" to its parameters. But think before you leap.
- To use a tool, set "action" to the tool name and "args" to the parameters,
  with "done" set to false.
- After receiving a tool result, think about the next step. Run as many steps as needed.
- When the task is complete, set "done" to true. The "answer" field is your
  final response — make it thorough and valuable:
  • Explain what you did and why
  • Share the results clearly
  • Propose better alternatives if you found any
  • Offer next steps the user might want to take
  • Vary your sentences — don't be a one-line robot. Be complete.
- Never give a one-sentence answer when the user asked for action. Show your work.

Format:
{{"thought":"(your reasoning here — visible to user)", "action":"tool_name|null", "args":{{}}, "done":false, "answer":null}}"""

_CLASSIFY_PROMPT = """You are Friday's intent classifier. Decide if the user wants
a TASK (web search, run code, open app, file/terminal ops, calculation, get time,
system info) or just CHAT (casual conversation or a plain question).
Reply with ONLY valid JSON: {"intent":"chat"|"task"}."""


def _clean_json(s: str) -> str:
    s = s.strip()
    if s.startswith("```"):
        parts = s.split("```")
        s = parts[1] if len(parts) > 1 else s
        if s.lower().startswith("json"):
            s = s[4:]
    # Extract the first JSON object from mixed text (e.g. preamble + JSON).
    start = s.find("{")
    if start >= 0:
        depth = 0
        for i, c in enumerate(s[start:], start):
            if c == "{":
                depth += 1
            elif c == "}":
                depth -= 1
                if depth == 0:
                    return s[start:i + 1]
    return s.strip()


def parse_action(text: str) -> Dict:
    """Robustly extract the reasoner's action JSON."""
    cleaned = _clean_json(text)
    try:
        data = json.loads(cleaned)
        if not isinstance(data, dict):
            raise ValueError("not an object")
    except Exception:
        # JSON parsing failed — never leak raw model output as user-facing answer.
        # If it looks like the model tried to emit action JSON but failed,
        # return a safe fallback rather than the raw text (which may contain
        # internal "thought"/"action" keys that must never reach the user).
        if any(k in text for k in ["\"action\"", "\"thought\"", "\"done\""]):
            return {"thought": "", "action": None, "args": {}, "done": True,
                    "answer": "Let me handle that directly."}
        return {"thought": "", "action": None, "args": {}, "done": True, "answer": text}

    action = data.get("action")
    args = data.get("args") or {}
    if not isinstance(args, dict):
        args = {}
    done = bool(data.get("done", False))
    answer = data.get("answer")

    if action and not done:
        done = False
    if done and not answer:
        answer = text
    if not done and not action:
        done = True
        answer = answer or text
    return {"thought": data.get("thought", ""), "action": action,
            "args": args, "done": done, "answer": answer}


def _local_chat_reply(text: str) -> str | None:
    """Zero-LLM fast path for obvious simple chat. Returns a reply string
    if the input is a greeting / small talk / acknowledgment / trivial query,
    otherwise None so the caller falls through to the normal LLM path.
    """
    low = (text or "").lower().strip()
    if not low:
        return None

    # Do not steal math/time questions into small talk.
    if bool(re.search(r"[\d\.\)]\s*[\+\-\*/]\s*[\d\.\(]", low)) or \
       (re.fullmatch(r"\s*[\d\.\+\-\*/\(\)\s]+\s*", low) is not None):
        return None

    # Exact-ish greetings / small talk / acknowledgments.
    # Warm, alive personality — never dry or robotic.
    simple = {
        "hi": "Hey there! What's on your mind?",
        "hello": "Hello! What can I do for you?",
        "hey": "Hey hey! How's your day going?",
        "good morning": "Good morning! Hope you slept well — what do you need?",
        "good afternoon": "Good afternoon! Hope your day's going great so far.",
        "good evening": "Good evening! How was your day?",
        "good night": "Good night! Sleep well — I'll be here if you need anything.",
        "bye": "Take care! Talk to you whenever.",
        "goodbye": "Bye for now! Don't be a stranger.",
        "thanks": "Of course — that's what I'm here for!",
        "thank you": "Anytime! Happy to help.",
        "ok": "Got it — I'm on it.",
        "okay": "Got it, moving on it.",
        "sure": "Absolutely, let's go.",
        "yes": "Alright, let's do this!",
        "no": "No worries at all.",
        "yo": "Yo! What's good?",
        "sup": "Not much — what's going on with you?",
        "lol": "Haha, love it 😄",
        "haha": "😂 that's gold",
        "cool": "Right? I thought so too.",
        "nice": "Nice! Love it.",
        "great": "Great stuff!",
        "alright": "Alright, let's roll.",
        "right": "Exactly what I was thinking.",
        "got it": "Perfect, got it locked in.",
        "understood": "Understood, crystal clear.",
        "np": "No problem at all — it's what I do.",
        "no problem": "Not a problem — happy to help.",
        "anyway": "Right, so anyway… what next?",
        "anyways": "Right, anyway — where were we?",
        "k": "Got it 👍",
        "kk": "Alright, on it!",
        "nice to meet you": "Likewise! Looking forward to working with you.",
        "how are you": "I'm great, thanks for asking! What can I do for you?",
        "how r u": "All good here! What's up on your end?",
        "how are you doing": "Doing awesome, appreciate you asking! What's up?",
        "what's up": "Not much on my end — what's up with you?",
        "whats up": "Hey hey! What's going on?",
        "not much": "That's fair. Whenever you need something, I'm here.",
        "nothing": "Fair enough — let me know when you need something.",
        "nothing much": "Same here. Just chilling, ready for whatever you need.",
        "i'm fine": "Glad to hear it! What's on your mind?",
        "im fine": "Good, happy you're doing well! What do you need?",
        "i am fine": "That's great to hear! How can I help?",
        "fine": "Good to hear! What's up?",
        "well": "That's good! What's next?",
        "hmm": "Thinking about something? Tell me.",
        "hrm": "What's on your mind?",
        "hm": "Something on your mind?",
        "mhm": "Uh huh — go on.",
        "mm": "I hear you.",
        "wassup": "Yo! What's happening?",
        "whats good": "What's good with you?",
        "what's good": "What's good? I'm ready.",
        "howdy": "Howdy partner! What can I do for ya?",
        "heya": "Heya! How's it going?",
        "heyy": "Heyyy! What's cooking?",
        "hiya": "Hiiya! Need anything?",
        "ello": "Ello there! What's up?",
        "how's it going": "Going great over here — more importantly, how about you?",
        "hows it going": "Pretty great! What's up on your end?",
        "what's happening": "Not much — what's happening with you?",
        "wsp": "Yo what's up?",
        "wassgood": "What's good?",
        "ay": "Ay! What's up?",
        "ayy": "Ayyy! There you are.",
        "yooo": "Yoo! What's good?",
        "yoo": "Yoo! Ready when you are.",
        "hii": "Hii! What's up?",
        "helloo": "Helloo! Great to see you.",
        "hellooo": "Hellooo! Been a minute.",
        "byee": "Byee! Talk soon.",
        "byeee": "Byeee! Don't stay away too long.",
        "tysm": "You're very welcome! Happy to help anytime.",
        "thx": "Thx right back at you!",
        "ty": "You got it!",
        "appreciate it": "Appreciate you too! Always here.",
        "much appreciated": "Thank you! Means a lot.",
        "no worries": "No worries at all — I've got you.",
        "no prob": "Easy peasy — not a problem.",
        "forget it": "Alright, no problem.",
        "never mind": "No worries, I'm here when you need me.",
        "nevermind": "Alright, forget it — I'll be here.",
        "whatever": "Fair enough.",
        "idk": "No worries — take your time and let me know.",
        "i don't know": "That's okay! When you figure it out, I'm here.",
        "i dont know": "All good — just tell me when you're ready.",
        "don't know": "No worries at all.",
        "dont know": "That's cool — take your time.",
        "maybe": "Take your time deciding.",
        "perhaps": "No rush — think it over.",
        "possibly": "Possibilities are good! Let me know what you settle on.",
        "not sure": "No rush — think it through and come back to me.",
        "not really": "Alright.",
        "kind of": "Got it.",
        "kinda": "Got it.",
        "sort of": "Got it.",
        "sorta": "Got it.",
        "i guess": "Alright.",
        "i suppose": "Alright.",
        "suppose so": "Alright.",
        "fair enough": "Right.",
        "makes sense": "Right.",
        "makes sense to me": "Right.",
        "true": "Right.",
        "facts": "Right.",
        "fr": "Right.",
        "frfr": "Right.",
        "no cap": "Right.",
        "cap": "Right.",
        "bet": "Right.",
        "aight": "Alright.",
        "ight": "Alright.",
        "fr?": "Right.",
        "right?": "Right.",
        "yessir": "Yep.",
        "yessss": "Yep.",
        "nooo": "No problem.",
        "noooo": "No problem.",
        "ugh": "You okay?",
        "uff": "You okay?",
        "fml": "You good?",
        "rip": "I see.",
        "ngl": "Right.",
        "tbh": "Right.",
        "imo": "Right.",
        "imho": "Right.",
        "afaik": "Right.",
        "idc": "Alright.",
        "i don't care": "Alright.",
        "idc tbh": "Alright.",
        "whatever then": "Alright.",
        "whatever man": "Alright.",
        "whatever dude": "Alright.",
        "k then": "Got it.",
        "alr": "Alright.",
        "alright then": "Right.",
        "cool then": "Right.",
        "bet up": "Right.",
        "say less": "Got it.",
        "less go": "Let's go.",
        "lets go": "Let's go.",
        "let's go": "Let's go.",
        "go": "Let's go.",
        "cmon": "Let's go.",
        "come on": "Let's go.",
        "wow": "Right?",
        "whoa": "Whoa.",
        "woah": "Whoa.",
        "omg": "I know.",
        "oh my god": "I know.",
        "oh gosh": "I know.",
        "oh man": "I know.",
        "oh no": "You okay?",
        "oh nooo": "You okay?",
        "oh shoot": "You okay?",
        "oh crap": "You okay?",
        "oh fudge": "You okay?",
        "oh mannn": "I know.",
        "oh wow": "Right?",
        "oh really": "Yeah.",
        "oh really?": "Yeah.",
        "oh ok": "Got it.",
        "oh okay": "Got it.",
        "oh alright": "Got it.",
        "oh sure": "Cool.",
        "oh definitely": "Right.",
        "oh for sure": "Right.",
        "fr fr": "Right.",
        "deadass": "Right.",
        "dead ass": "Right.",
        "on god": "Right.",
        "on god fr": "Right.",
        "no cap fr": "Right.",
        "cap fr": "Right.",
        "lowkey": "Right.",
        "highkey": "Right.",
        "mid": "Fair.",
        "w": "Nice.",
        "l": "Oops.",
        "wowww": "Right?",
        "yasss": "Yep.",
        "yass": "Yep.",
        "slay": "Right.",
        "periodt": "Right.",
        "period": "Right.",
        "facts only": "Right.",
        "big facts": "Right.",
        "fax": "Right.",
        "no fax": "Right.",
        "fax no printer": "Right.",
        "sheesh": "I know.",
        "sheeesh": "I know.",
        "bussin": "Nice.",
        "bussin bussin": "Nice.",
        "fire": "Right.",
        "straight fire": "Right.",
        "it is giving": "Right.",
        "it's giving": "Right.",
        "its giving": "Right.",
        "rizz": "Right.",
        "w riz": "Right.",
        "no riz": "Fair.",
        "sigma": "Right.",
        "sigma male": "Right.",
        "sigma grindset": "Right.",
        "alpha": "Right.",
        "beta": "Right.",
        "chill": "Cool.",
        "chill chill": "Cool.",
        "vibe": "Cool.",
        "vibes": "Cool.",
        "good vibes": "Cool.",
        "good vibes only": "Cool.",
        "vibe check": "Passed.",
        "vibecheck": "Passed.",
        "glow up": "Nice.",
        "glowup": "Nice.",
        "main character": "Right.",
        "protagonist": "Right.",
        "np np": "No problem.",
        "yw": "You're welcome.",
        "yw bro": "Anytime.",
        "yw girl": "Anytime.",
        "np bro": "No problem.",
        "np girl": "No problem.",
        "thanks bro": "Anytime.",
        "thanks girl": "Anytime.",
        "thx bro": "Anytime.",
        "thx girl": "Anytime.",
        "ty bro": "Anytime.",
        "ty girl": "Anytime.",
        "love u": "Love you too.",
        "love you": "Love you too.",
        "miss u": "Miss you too.",
        "miss you": "Miss you too.",
        "bored": "Want me to tell you a fun fact or play a game?",
        "im bored": "Want me to tell you a fun fact or play a game?",
        "i'm bored": "Want me to tell you a fun fact or play a game?",
        "lonely": "I'm here.",
        "im lonely": "I'm here.",
        "i'm lonely": "I'm here.",
        "sad": "You okay? Want me to cheer you up?",
        "im sad": "You okay? Want me to cheer you up?",
        "i'm sad": "You okay? Want me to cheer you up?",
        "happy": "Nice.",
        "im happy": "Nice.",
        "i'm happy": "Nice.",
        "excited": "Nice.",
        "im excited": "Nice.",
        "i'm excited": "Nice.",
        "tired": "You should rest.",
        "im tired": "You should rest.",
        "i'm tired": "You should rest.",
        "sleepy": "You should rest.",
        "imsleepy": "You should rest.",
        "hungry": "You should eat.",
        "im hungry": "You should eat.",
        "i'm hungry": "You should eat.",
        "thirsty": "Drink water.",
        "im thirsty": "Drink water.",
        "i'm thirsty": "Drink water.",
        "bored af": "Want me to tell you a fun fact?",
        "bored asf": "Want me to tell you a fun fact?",
        "bored as hell": "Want me to tell you a fun fact?",
        "bored out of my mind": "Want me to tell you a fun fact?",
        "what should i do": "What are you in the mood for?",
        "what should i do?": "What are you in the mood for?",
        "im bored what should i do": "What are you in the mood for?",
        "i'm bored what should i do": "What are you in the mood for?",
        "tell me something": "Here's one: octopuses have three hearts.",
        "tell me something fun": "Here's one: octopuses have three hearts.",
        "fun fact": "Here's one: octopuses have three hearts.",
        "random fact": "Here's one: octopuses have three hearts.",
        "something fun": "Here's one: octopuses have three hearts.",
        "entertain me": "Want me to tell you a fun fact or a joke?",
        "joke": "Why do programmers prefer dark mode? Because light attracts bugs.",
        "tell me a joke": "Why do programmers prefer dark mode? Because light attracts bugs.",
        "make me laugh": "Why do programmers prefer dark mode? Because light attracts bugs.",
        "i need a laugh": "Why do programmers prefer dark mode? Because light attracts bugs.",
        "cheer me up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i'm sad cheer me up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "im sad cheer me up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "cheer me up please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheering up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to cheer up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to be cheered up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need a cheer up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheer up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheering": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to be cheered": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to cheer": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheer": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheering up please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to be cheered up please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to cheer up please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheer up please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheering please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to be cheered please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to cheer please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheer please": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheering up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to be cheered up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to cheer up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheer up": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheering": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to be cheered": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need to cheer": "Here's one: octopuses have three hearts. Also, you're doing great.",
        "i need cheer": "Here's one: octopuses have three hearts. Also, you're doing great.",
    }

    if low in simple:
        return simple[low]

    # Starts with a greeting word.
    starters = ("hi", "hello", "hey", "good morning", "good afternoon",
                "good evening", "good night", "howdy", "heya", "heyy", "yo", "sup", "wassup", "ay")
    if any(low.startswith(s) for s in starters):
        return simple.get(low, "Hey.")

    # Pure acknowledgments / filler.
    fillers = ("ok", "okay", "sure", "yes", "no", "k", "kk", "alright",
               "right", "got it", "understood", "np", "no prob", "no problem",
               "yw", "bet", "aight", "ight", "alr", "mhm", "hmm", "hm",
               "hrm", "lol", "haha", "cool", "nice", "great", "fine",
               "well", "kinda", "sorta", "sort of", "kind of", "maybe",
               "perhaps", "possibly", "not sure", "not really", "whatever",
               "idk", "i dont know", "i don't know", "dont know", "don't know",
               "true", "facts", "fr", "frfr", "no cap", "cap", "lowkey",
               "highkey", "mid", "w", "l", "sheesh", "sheeesh", "bussin",
               "fire", "slay", "periodt", "period", "fax", "ngl", "tbh",
               "imo", "imho", "afaik", "idc", "i don't care", "i dont care",
               "yessir", "yass", "yasss", "byee", "byeee", "thx", "ty",
               "tysm", "appreciate it", "much appreciated", "no worries",
               "forget it", "never mind", "nevermind", "say less", "less go",
               "lets go", "let's go", "go", "cmon", "come on", "wow", "whoa",
               "woah", "omg", "oh my god", "oh gosh", "oh man", "oh no",
               "oh shoot", "oh crap", "oh fudge", "oh wow", "oh really",
               "oh really?", "oh ok", "oh okay", "oh alright", "oh sure",
               "oh definitely", "oh for sure", "fr fr", "deadass", "dead ass",
               "on god", "on god fr", "no cap fr", "cap fr", "rizz",
               "w riz", "no riz", "sigma", "sigma male", "sigma grindset",
               "alpha", "beta", "chill", "vibe", "vibes", "good vibes",
               "good vibes only", "vibe check", "vibecheck", "glow up",
               "glowup", "main character", "protagonist", "np np", "yw bro",
               "yw girl", "np bro", "np girl", "thanks bro", "thanks girl",
               "thx bro", "thx girl", "ty bro", "ty girl", "love u",
               "love you", "miss u", "miss you", "ugh", "uff", "fml",
               "rip", "mm", "hii", "helloo", "hellooo", "yooo", "yoo",
               "ayy", "ay", "wsp", "wassgood", "whats good", "what's good",
               "how's it going", "hows it going", "what's happening",
               "not much", "nothing", "nothing much", "i'm fine", "im fine",
               "i am fine", "well", "haha", "lol")
    if low in fillers:
        return simple.get(low, "Got it.")

    # Very short inputs that are clearly not tasks.
    words = low.split()
    if len(words) == 1 and len(low) <= 6:
        return simple.get(low, "Got it.")

    # If the input contains a question word or looks like a real question/task,
    # let the LLM handle it instead of treating it as small talk.
    question_words = ("what", "who", "where", "when", "why", "how", "which",
                      "can", "could", "would", "is", "are", "do", "does",
                      "did", "will", "shall", "should", "may", "might",
                      "check", "find", "tell", "show", "list", "get",
                      "remember", "recall", "open", "calculate", "compute",
                      "search", "look", "read", "write", "run", "execute",
                      "my name", "favorite", "colour", "color", "name is")
    if any(w in low for w in question_words):
        return None

    # If it's a statement or command that could be a task, let the LLM handle it.
    if len(words) >= 3:
        return None

    return None


def _reasoner_chat(messages: List[Dict], start: int = 0, reverse: bool = False,
                   role: str = "reasoner"):
    """Use the shared llm.chat() path so this gets timeout, retry, and
    circuit-breaker behavior automatically."""
    order = config.live_chain(role)
    if not order:
        raise RuntimeError("No provider key for role '%s'." % role)
    chain = list(order)
    if reverse:
        chain = chain[::-1]
    # Use lower temperature and max_tokens for faster responses
    # Check if this is a simple task (few messages)
    is_simple = len(messages) <= 3
    temperature = 0.2 if is_simple else 0.4
    max_tokens = 800 if is_simple else 1600
    # llm.chat() already walks the chain, records success/failure, and
    # returns (text, provider). Re-raise if every candidate is exhausted.
    text, prov = llm.chat(messages, role=role, temperature=temperature, max_tokens=max_tokens)
    if not text.strip():
        raise RuntimeError("Empty response from %s" % prov)
    return text, prov


def _heuristic(text: str) -> Dict:
    t = text.lower()
    steps = [{"worker": "companion", "task": text}]
    if any(w in t for w in ["code", "python", "script", "program", "file"]):
        steps = [{"worker": "reasoner", "task": text}, {"worker": "coder", "task": text}]
    elif any(w in t for w in ["search", "research", "news", "info"]):
        steps = [{"worker": "researcher", "task": text}]
    elif any(w in t for w in ["plan", "how to", "problem"]):
        steps = [{"worker": "reasoner", "task": text}, {"worker": "companion", "task": text}]
    return {"intent": "heuristic", "language": "en",
            "steps": steps, "final_worker": "companion"}


# Enhanced router prompt for DAG planning with dependencies
_ROUTER_PROMPT_DAG = """You are Friday's DAG Planner. Decompose the user's request into a Directed Acyclic Graph (DAG) of steps.

Output JSON format:
{
  "intent": "short label",
  "language": "en",
  "steps": [
    {"id": 0, "worker": "researcher|coder|reasoner|companion", "task": "specific task description"},
    {"id": 1, "worker": "coder", "task": "specific task description"}
  ],
  "dag_edges": {
    "1": [0],
    "2": [0, 1]
  },
  "final_worker": "companion",
  "intent": "intent label"
}

Rules:
- Each step gets a unique integer id (0, 1, 2...)
- dag_edges maps step_id -> list of dependency step_ids
- Only include edges for steps that have dependencies
- Steps with no dependencies run first (in parallel)
- Use workers: researcher (web search, info gathering), coder (code, files, scripts), reasoner (planning, logic, multi-step), companion (chat, simple Q&A)
- Max 5 steps
- For simple chat, return 1 step with companion

Example: "Research Python async and write a summary"
{
  "intent": "research_and_summarize",
  "language": "en",
  "steps": [
    {"id": 0, "worker": "researcher", "task": "Search for Python async best practices and patterns"},
    {"id": 1, "worker": "coder", "task": "Write a summary document of Python async patterns"}
  ],
  "dag_edges": {"1": [0]},
  "final_worker": "companion",
  "intent": "research_and_summarize"
}"""

def _route_with_dag(user_input: str, context: str) -> Dict:
    """Generate a DAG plan with dependencies using the router LLM."""
    messages = [
        {"role": "system", "content": _ROUTER_PROMPT_DAG},
        {"role": "user", "content": f"context:\n{context}\n\nuser: {user_input}"},
    ]
    try:
        raw, _ = llm.chat(messages, role="router", temperature=0.2,
                          max_tokens=800, json_mode=True)
        plan = json.loads(_clean_json(raw))
        if isinstance(plan, dict) and "steps" in plan:
            # Ensure dag_edges is present
            if "dag_edges" not in plan:
                plan["dag_edges"] = {}
            return plan
    except Exception as e:
        logger.warning(f"DAG router failed: {e}")
    
    # Fallback heuristic
    return _heuristic_dag(user_input)


def _heuristic_dag(user_input: str) -> Dict:
    """Heuristic fallback for DAG planning."""
    t = user_input.lower()
    steps = []
    dag_edges = {}
    
    if any(w in t for w in ["search", "research", "find", "look up"]):
        steps.append({"id": 0, "worker": "researcher", "task": user_input})
    if any(w in t for w in ["code", "script", "program", "function", "write", "create", "build"]):
        step_id = len(steps)
        steps.append({"id": step_id, "worker": "coder", "task": user_input})
        if step_id > 0:
            dag_edges[str(step_id)] = [step_id - 1]
    if any(w in t for w in ["plan", "analyze", "think", "strategy", "design"]):
        step_id = len(steps)
        steps.append({"id": step_id, "worker": "reasoner", "task": user_input})
        if step_id > 0:
            dag_edges[str(step_id)] = [step_id - 1]
    
    if not steps:
        steps = [{"id": 0, "worker": "companion", "task": user_input}]
    
    return {
        "intent": "heuristic",
        "language": "en",
        "steps": steps,
        "dag_edges": dag_edges,
        "final_worker": "companion"
    }


def _verify(user_input: str, response: str, lang: str) -> str:
    if config.verify_passes <= 0 or not response.strip():
        return response
    messages = [
        {"role": "system", "content": prompts.WORKER_PROMPTS["verifier"]},
        {"role": "user", "content":
            f"Question: {user_input}\n\nAnswer:\n{response}\n\nExpected language: English"},
    ]
    try:
        improved, _ = llm.chat(messages, role="verifier", temperature=0.3, max_tokens=1200)
        return improved.strip() or response
    except Exception as e:
        print(f"[orchestrator] verify failed: {e}")
        return response


def _companion_reply(user_input: str, memory, screen_context: str = None) -> str:
    """Fast path for CHAT: a single companion call. No routing, no multi-worker
    fan-out, no verify pass. This is the 'simple thing -> just answer' path.
    If screen_context is supplied (live eye), the companion can see the screen
    and answer screen-aware questions (guide mode)."""
    context = memory.system_context(user_input) if memory else ""
    return resilience.safe(
        workers.run_worker, "companion", user_input, context, "",
        screen=screen_context,
        fallback=resilience.EN_FALLBACK)


def orchestrate(user_input: str, memory, language_hint: str = None,
                screen_context: str = None) -> Dict:
    # Record conversation turn for ambient intelligence
    _ambient.record_conversation_turn(user_input)
    try:
        context = memory.system_context(user_input)
        plan = _route(user_input, context)
        lang = plan.get("language") or language_hint or "en"
        steps = plan.get("steps") or [{"worker": "companion", "task": user_input}]

        outputs, used = [], []
        for step in steps[:3]:
            w = step.get("worker", "companion")
            task = step.get("task", user_input)
            out = resilience.safe(workers.run_worker, w, task, context, "\n".join(outputs),
                                  screen=screen_context, fallback="")
            if out:
                outputs.append(f"[{w}] {out}")
            used.append(w)

        final_w = plan.get("final_worker", "companion")
        final_task = (f"User's original request: {user_input}\n\nTask results:\n"
                      + "\n".join(outputs)
                      + "\n\nNow combine into a natural English final answer.")
        response = resilience.safe(workers.run_worker, final_w, final_task, context,
                                   screen=screen_context,
                                   fallback=resilience.EN_FALLBACK)
        response = _verify(user_input, response, lang)
        _update_self_model(user_input, response, plan, memory)
        return {"response": response, "language": lang, "plan": plan, "workers_used": used}
    except Exception as e:
        print(f"[orchestrator] fatal, returning fallback: {e}")
        return {"response": resilience.EN_FALLBACK,
                "language": "en",
                "plan": {"intent": "error"}, "workers_used": []}


def _update_self_model(user_input: str, response: str, plan: dict, memory):
    """Best-effort, fire-and-forget self-model growth. Never raises."""
    try:
        from . import self_model as sm
        # Respect explicit boundaries: if the user told Friday not to do something
        # and this turn is about that, we still log it but the boundary stays.
        sm.record_interaction()
        intent = (plan or {}).get("intent") or (plan or {}).get("type") or ""
        # Capture the user's name if they introduced themselves
        if memory is not None and hasattr(memory, "system_context"):
            pass  # name is pulled from memory elsewhere; keep this lightweight
        if intent:
            sm.learn(f"intent={intent}", confidence=0.4)
        # Feed learning engine with the interaction
        _learning.observe(
            user_input=user_input,
            response=response,
            tool=plan.get("learned_tool", "") if plan else ""
        )
    except Exception:
        pass


def _record_agentic_learning(user_text: str, steps: list, final_reply: str):
    """Record agentic loop outcomes for learning."""
    try:
        for step in steps:
            action = step.get("action")
            if action and step.get("result"):
                success = "error" not in step.get("result", "").lower()
                _learning.observe(
                    user_input=user_text,
                    response=final_reply,
                    tool=action,
                    success=success
                )
                break  # Only record once per agentic run
    except Exception:
        pass


def agentic_run(user_text: str, lang: str = "en", context: str = None,
                max_steps: int = 4, screen_context: str = None) -> Dict:
    """Run a real agentic loop: reasoner plans -> calls tools -> reasons again.

    Returns {"reply": str, "steps": [...]} always. Crash-proof.
    """
    try:
        schema = json.dumps(tools.get_tool_schemas(), ensure_ascii=False)
        system = _REASONER_SYSTEM.format(
            lang="English", schema=schema)
        messages: List[Dict] = [{"role": "system", "content": system}]
        if context:
            messages.append({"role": "system", "content": "Context:\n" + context})
        if screen_context:
            messages.append({"role": "system", "content":
                             "LIVE SCREEN (what the user is looking at right now):\n"
                             + screen_context})
        messages.append({"role": "user", "content": user_text})

        steps: List[Dict] = []
        final = None

        for step_i in range(max_steps):
            text, prov = _reasoner_chat(messages)
            action = parse_action(text)
            entry = {"thought": action["thought"], "action": action["action"],
                     "args": action["args"], "provider": prov}
            steps.append(entry)
            # First step: if the model tried to answer without using a tool,
            # force one tool-call retry so task inputs actually execute.
            if step_i == 0 and (action["done"] or not action["action"]):
                messages.append({"role": "user", "content":
                    "CRITICAL: You must use a tool to complete this task. "
                    "Look at the available tools and pick the one that matches "
                    "the user's request. Reply with ONLY this JSON format: "
                    "{\"thought\":\"...\",\"action\":\"tool_name\",\"args\":{...},\"done\":false,\"answer\":null}. "
                    "Set action to the exact tool name from the schema. Do NOT set done=true. "
                    "Do NOT answer directly. Use a tool NOW."})
                text, prov = _reasoner_chat(messages, reverse=True)
                action = parse_action(text)
                entry = {"thought": action["thought"], "action": action["action"],
                         "args": action["args"], "provider": prov,
                         "retry": True}
                steps.append(entry)
            if action["done"] or not action["action"]:
                final = action["answer"] if action["answer"] else text
                break
            args = action["args"] or {}
            # Manage-files guard: the LLM often omits the required "action" key
            # (list/read/write/create/delete) inside args. Infer it from the
            # user's text when missing so delete/write/etc. still route correctly.
            if action["action"] == "manage_files" and not args.get("action"):
                low = (user_text or "").lower()
                for verb in ("delete", "remove", "write", "create", "read", "list"):
                    if verb in low:
                        args = dict(args)
                        args["action"] = verb
                        break
            # Self-model boundary guard: check if action violates user's explicit "do_not" rules
            try:
                from . import self_model as sm
                boundary = sm.should_avoid(f"{action['action']} {json.dumps(args)}")
                if boundary:
                    result = f"BLOCKED by self-model boundary: {boundary}"
                    entry["result"] = result
                    entry["blocked"] = True
                    steps.append(entry)
                    return {"reply": f"I can't do that — you previously said: {boundary}", "steps": steps}
            except Exception:
                pass
            result = tools.safe_tool_call(action["action"], args)
            if tools.is_confirmation_result(result):
                entry["result"] = result
                entry["confirmation"] = tools.parse_confirmation(result)
                steps.append(entry)
                return {"reply": _confirm_prompt(action["action"], action["args"]),
                        "steps": steps,
                        "confirm": entry["confirmation"]}
            entry["result"] = result
            messages.append({"role": "assistant", "content": text})
            messages.append({"role": "user", "content":
                f"Tool result [{action['action']}]:\n{result}\n\n"
                f"Provide the next step or final answer (done:true)."})

        if final is None:
            final = resilience.EN_FALLBACK

        retries = 0
        while len(final or "") < 15 and retries < 2:
            messages.append({"role": "user", "content":
                "The previous answer was insufficient, please try again with a more complete answer."})
            try:
                text, prov = _reasoner_chat(messages, reverse=(retries > 0))
                action = parse_action(text)
                cand = action["answer"] if action["answer"] else text
                if len(cand) >= 15 and cand != final:
                    final = cand
                    steps.append({"thought": action["thought"], "action": None,
                                  "provider": prov, "retry": True})
                    break
                final = cand
            except Exception:
                break
            retries += 1

        final = _verify(user_text, final or resilience.EN_FALLBACK, lang)
        _record_agentic_learning(user_text, steps, final)
        return {"reply": final, "steps": steps}
    except Exception as e:
        logger.error("agentic_run failed: %s", e)
        return {"reply": resilience.EN_FALLBACK, "steps": []}


def agentic_run_with_plan(user_text: str, lang: str = "en", context: str = None,
                          max_steps: int = 4, screen_context: str = None) -> Dict:
    """Run agentic loop with MANDATORY DAG planning first.
    
    1. Generate a DAG plan from user input
    2. Execute the DAG via ExecutionEngine (parallel fan-out, checkpoints)
    3. Fall back to original agentic_run if planning fails
    """
    try:
        # Step 1: Generate DAG plan
        from .planning import generate_plan, ExecutionEngine
        logger.info("Generating DAG plan for: %s", user_text[:100])
        
        graph = generate_plan(user_text, context or "", max_nodes=10)
        
        # Step 2: Execute the DAG with parallel execution and checkpoints
        engine = ExecutionEngine(max_workers=4)
        executed_graph = engine.execute(graph, checkpoint_interval=2)
        
        final_answer = executed_graph.final_answer or "Plan executed."
        
        # Convert DAG execution to steps format for compatibility
        steps = []
        for node_id, node in executed_graph.nodes.items():
            if node.status.value == "completed":
                steps.append({
                    "thought": node.description,
                    "action": node.tool or node.node_type.value,
                    "args": node.args,
                    "result": node.result,
                    "provider": "planner",
                })
        
        _record_agentic_learning(user_text, steps, final_answer)
        logger.info("Plan-first execution completed: %s nodes", len(executed_graph.nodes))
        return {"reply": final_answer, "steps": steps}
        
    except Exception as e:
        logger.warning("Plan-first execution failed, falling back to tool loop: %s", e)
        # Fallback to original agentic_run
        return agentic_run(user_text, lang, context, max_steps, screen_context)


def agentic_stream(user_text, lang="en", context=None, max_steps=4, run_id=None,
                    screen_context=None, preferred_role=None):
    cancel = _CANCEL.get(run_id) if run_id else None
    deadline = time.time() + getattr(config, "turn_deadline_seconds", 45)
    from . import team as _agent_team
    _agent_role = preferred_role or "reasoner"
    _agent_name = _agent_team.name_of(_agent_role)
    try:
        schema = json.dumps(tools.get_tool_schemas(), ensure_ascii=False)
        system = _REASONER_SYSTEM.format(
            lang="English", schema=schema)
        messages: List[Dict] = [{"role": "system", "content": system}]
        if context:
            messages.append({"role": "system", "content": "Context:\n" + context})
        if screen_context:
            messages.append({"role": "system", "content":
                             "LIVE SCREEN (what the user is looking at right now):\n"
                             + screen_context})
        if preferred_role:
            messages.append({"role": "system", "content":
                             f"PREFERRED WORKER: the user specifically asked for the "
                             f"'{preferred_role}' worker. Lean into that worker's "
                             f"strength when answering."})
        messages.append({"role": "user", "content": user_text})

        yield {"type": "start", "text": user_text, "lang": lang, "name": _agent_name}

        # Emit worker status for control center
        if _agent_name:
            yield {"type": "worker_status", "worker": _agent_name, "status": "thinking", "activity": "Analyzing your request"}

        steps: List[Dict] = []
        final = None
        # Context window guard: keep the working set small for long agentic runs.
        _ctx_cap = getattr(config, "agentic_context_cap", 12)

        for step_i in range(max_steps):
            if time.time() > deadline:
                yield {"type": "thought", "thought": "(deadline reached, returning best-so-far)", "provider": None, "name": _agent_name}
                final = final or resilience.EN_FALLBACK
                break
            if cancel is not None and cancel.is_set():
                yield {"type": "cancelled"}
                final = final or "Stopped."
                break
            text, prov = _reasoner_chat(messages, role=preferred_role or "reasoner")
            action = parse_action(text)
            entry = {"thought": action["thought"], "action": action["action"],
                     "args": action["args"], "provider": prov, "name": _agent_name}
            steps.append(entry)
            yield {"type": "thought", "thought": action["thought"], "provider": prov, "name": _agent_name}
            
            # Emit worker status for control center
            if _agent_name:
                yield {"type": "worker_status", "worker": _agent_name, "status": "thinking", "activity": action["thought"][:50]}

            # First step: if the model tried to answer without using a tool,
            # force one tool-call retry so task inputs actually execute.
            if step_i == 0 and (action["done"] or not action["action"]):
                messages.append({"role": "user", "content":
                    "CRITICAL: You must use a tool to complete this task. "
                    "Look at the available tools and pick the one that matches "
                    "the user's request. Reply with ONLY this JSON format: "
                    "{\"thought\":\"...\",\"action\":\"tool_name\",\"args\":{...},\"done\":false,\"answer\":null}. "
                    "Set action to the exact tool name from the schema. Do NOT set done=true. "
                    "Do NOT answer directly. Use a tool NOW."})
                text, prov = _reasoner_chat(messages, role=preferred_role or "reasoner", reverse=True)
                action = parse_action(text)
                entry = {"thought": action["thought"], "action": action["action"],
                         "args": action["args"], "provider": prov,
                         "retry": True, "name": _agent_name}
                steps.append(entry)
                yield {"type": "thought", "thought": action["thought"], "provider": prov, "name": _agent_name}
            if action["done"] or not action["action"]:
                final = action["answer"] if action["answer"] else text
                break
            yield {"type": "action", "action": action["action"], "args": action["args"], "name": _agent_name}
            
            # Emit worker status for control center - working on tool
            if _agent_name and action["action"]:
                yield {"type": "worker_status", "worker": _agent_name, "status": "working", "activity": f"Using {action['action']}"}
            
            args = action["args"] or {}
            if action["action"] == "manage_files" and not args.get("action"):
                low = (user_text or "").lower()
                for verb in ("delete", "remove", "write", "create", "read", "list"):
                    if verb in low:
                        args = dict(args)
                        args["action"] = verb
                        break
            # Self-model boundary guard for streaming agentic loop
            try:
                from . import self_model as sm
                boundary = sm.should_avoid(f"{action['action']} {json.dumps(args)}")
                if boundary:
                    result = f"BLOCKED by self-model boundary: {boundary}"
                    entry["result"] = result
                    entry["blocked"] = True
                    yield {"type": "result", "action": action["action"], "result": result, "name": _agent_name}
                    yield {"type": "final", "reply": f"I can't do that — you previously said: {boundary}"}
                    return
            except Exception:
                pass
            result = tools.safe_tool_call(action["action"], args)
            if tools.is_confirmation_result(result):
                entry["result"] = result
                entry["confirmation"] = tools.parse_confirmation(result)
                steps.append(entry)
                yield {"type": "confirm", "message": _confirm_prompt(action["action"], args),
                       "action": action["action"], "args": args, "steps": steps, "name": _agent_name}
                final = _confirm_prompt(action["action"], args)
                break
            if run_id and tools.is_phone_command_result(result):
                pc = tools.parse_phone_command(result)
                yield {"type": "phone_command", "command_id": pc.get("command_id"),
                        "action": pc.get("action"), "target": pc.get("target"), "name": _agent_name}
                continue
            entry["result"] = result
            yield {"type": "result", "action": action["action"], "result": result, "name": _agent_name}
            messages.append({"role": "assistant", "content": text})
            messages.append({"role": "user", "content":
                f"Tool result [{action['action']}]:\n{result}\n\n"
                f"Provide the next step or final answer (done:true)."})
            # Compact context when it grows too large for a long session.
            if len(messages) > _ctx_cap:
                keep = [messages[0]] + messages[-(_ctx_cap - 1):]
                dropped = messages[1 : len(messages) - (_ctx_cap - 1)]
                if dropped:
                    summary_parts = []
                    for m in dropped:
                        if m["role"] == "assistant":
                            summary_parts.append(m["content"][:120])
                        elif m["role"] == "user" and "Tool result" in m.get("content", ""):
                            summary_parts.append("[tool result]")
                    if summary_parts:
                        messages = keep[:1] + [{"role": "system", "content":
                            "Earlier turns (compacted): " + "; ".join(summary_parts[:8])}] + keep[1:]
                    else:
                        messages = keep

        if final is None:
            final = resilience.EN_FALLBACK

        # Emit worker status - speaking/ready
        if _agent_name:
            yield {"type": "worker_status", "worker": _agent_name, "status": "speaking", "activity": "Preparing response"}

        if cancel is None or not cancel.is_set():
            retries = 0
            while len(final or "") < 15 and retries < 2:
                messages.append({"role": "user", "content":
                    "The previous answer was insufficient, please try again with a more complete answer."})
                try:
                    text, prov = _reasoner_chat(messages, reverse=(retries > 0))
                    action = parse_action(text)
                    cand = action["answer"] if action["answer"] else text
                    if len(cand) >= 15 and cand != final:
                        final = cand
                        steps.append({"thought": action["thought"], "action": None,
                                      "provider": prov, "retry": True})
                        break
                    final = cand
                except Exception:
                    break
                retries += 1

        final = _verify(user_text, final or resilience.EN_FALLBACK, lang)
        _record_agentic_learning(user_text, steps, final)

        yield {"type": "final", "reply": final, "name": _agent_name}
        
        # Emit worker status - idle after completion
        if _agent_name:
            yield {"type": "worker_status", "worker": _agent_name, "status": "idle", "activity": "Ready"}
    except Exception as e:
        logger.error("agentic_stream failed: %s", e)
        yield {"type": "error", "message": str(e), "name": _agent_name}
        yield {"type": "final", "reply": resilience.EN_FALLBACK, "name": _agent_name}
    finally:
        if run_id:
            _clear_cancel(run_id)


def handle_turn_stream(text, lang="en", memory=None, run_id=None,
                        screen_context=None, preferred_role=None):
    mem = memory
    from . import team as _stream_team
    _companion_name = _stream_team.name_of(preferred_role or "companion")
    try:
        low = (text or "").lower()
        _TASK_SIGNALS = ["search", "code", "python", "open", "terminal", "file",
                         "calc", "calculate", "time", "run", "system", "app",
                         "website", "url", "delete", "write", "create", "execute",
                         "install", "launch", "browse", "research", "news", "send",
                         "click", "type", "screenshot", "script", "program",
                         "remember", "rename", "move", "copy", "update", "build",
                         "summarize", "plan", "how to", "problem", "email", "remind"]
        is_task = any(w in low for w in _TASK_SIGNALS)

        if not is_task:
            # Zero-LLM fast path for obvious simple chat.
            local_reply = _local_chat_reply(text)
            if local_reply is not None:
                yield {"type": "thought", "thought": "(quick reply)", "provider": None, "name": _companion_name}
                yield {"type": "final", "reply": local_reply, "name": _companion_name}
                if mem is not None:
                    try:
                        mem.add_turn(text, local_reply, lang)
                    except Exception:
                        pass
                return
            yield {"type": "thought", "thought": "(thinking…)", "provider": None, "name": _companion_name}
            reply = _companion_reply(text, mem, screen_context=screen_context)
            if preferred_role and preferred_role != "companion":
                try:
                    from .workers import run_worker
                    ctx = mem.system_context(text) if mem else ""
                    flavored = run_worker(preferred_role, text, ctx, "",
                                          screen=screen_context)
                    if flavored:
                        reply = flavored
                except Exception:
                    pass
            yield {"type": "final", "reply": reply, "name": _companion_name}
            if mem is not None:
                try:
                    mem.add_turn(text, reply, lang)
                except Exception:
                    pass
            return
        if run_id:
            _new_cancel(run_id)
        final_reply = None
        for ev in agentic_stream(text, lang,
                                  context=mem.system_context(text) if mem else None,
                                  run_id=run_id, screen_context=screen_context,
                                  preferred_role=preferred_role):
            if ev.get("type") == "final":
                final_reply = ev.get("reply")
            yield ev
        if mem is not None and final_reply:
            try:
                mem.add_turn(text, final_reply, lang)
            except Exception:
                pass
    except Exception as e:
        logger.error("handle_turn_stream failed: %s", e)
        yield {"type": "error", "message": str(e)}
        yield {"type": "final", "reply": resilience.EN_FALLBACK}


def _classify(text: str) -> str:
    """Tiered 'decide who, instantly':
    Tier 0 - instant local heuristic (no LLM call) for obvious cases.
    Tier 1 - LLM classifier ONLY when the heuristic is not confident.
    """
    t = text.lower()

    # Obvious TASK signals: user clearly wants action/tools.
    task_signals = ["search", "code", "python", "open", "terminal", "file",
                    "calc", "calculate", "time", "run", "system", "app",
                    "website", "url", "delete", "write", "create", "execute",
                    "install", "launch", "browse", "research", "news", "send",
                    "click", "type", "screenshot", "script", "program",
                    "remember", "rename", "move", "copy", "update", "build"]
    if any(w in t for w in task_signals):
        return "task"

    # Obvious CHAT: very short greetings / filler (cheap, no LLM).
    short_chat = ["hi", "hello", "hey", "thanks", "thank you", "ok", "okay",
                  "yes", "no", "yo", "sup", "good morning", "good night",
                  "bye", "lol", "haha", "cool", "nice", "great", "sure"]
    first = t.strip().split()[0] if t.strip() else ""
    if first in short_chat or t.strip() in short_chat:
        return "chat"

    # Ambiguous: ask the LLM classifier exactly once.
    try:
        messages = [
            {"role": "system", "content": _CLASSIFY_PROMPT},
            {"role": "user", "content": text},
        ]
        raw, _ = llm.chat(messages, role="router", temperature=0.1,
                          max_tokens=200, json_mode=True)
        data = json.loads(_clean_json(raw))
        if isinstance(data, dict) and data.get("intent") == "task":
            return "task"
    except Exception:
        pass
    return "chat"


try:
    from .memory import Memory
    _default_memory = Memory(os.path.join(config.data_dir, "turns"))
except Exception:
    _default_memory = None


def handle_turn_fast(text: str, lang: str = "en", memory=None,
                     screen_context: str = None) -> Dict:
    """Cheap, strong, "thinks-like-you" text path. Cost:
      * chat          -> 1 companion LLM call (+0 for cached repeats)
      * local task    -> 0 LLM calls (calc/time/file-read/simple search)
      * complex task  -> delegated to agentic_run (full multi-call loop, on demand)
      * ambiguous     -> +1 LLM classifier call (only when the heuristic is unsure)
    No feature is removed: agentic_run/agentic_stream are used for real tasks.
    Returns {"reply": str, "steps": [...], "calls": int, "suggestions": [...],
             "cached": bool}.
    """
    mem = memory or _default_memory
    import re as _re
    from . import cache as _cache_mod
    from . import anticipate as _ant

    low = (text or "").lower()
    calls = 0
    cached = False
    suggestions = []
    _t0 = time.time()

    # Pre-compiled math helpers (module-level to avoid closure/scope bugs).
    _WORD_OPS = [
        ("plus", "+"), ("added to", "+"), ("and", "+"),
        ("minus", "-"), ("subtract", "-"), ("less", "-"),
        ("times", "*"), ("multiplied by", "*"), ("multiply", "*"),
        ("divided by", "/"), ("divide by", "/"), ("over", "/"),
    ]
    _WORD_OP_RE = re.compile(
        r"(?i)\b(plus|minus|times|divided\s+by|multiplied\s+by|multiply|divide|"
        r"added\s+to|subtract|over|and)\b"
    )

    try:
        # 0) Local answer cache: repeats / cached facts cost 0 calls.
        #    Only cache simple replies, NOT tool-using or complex responses.
        cacher = _cache_mod.get_cache()
        hit = cacher.get(text) if getattr(config, "local_cache_ttl", 600) > 0 else None
        if hit and not any(w in low for w in ["research", "build", "investigate", "code", "python", "write", "create", "delete", "execute", "run", "install", "trading", "system", "design", "architecture"]):
            logger.info("[timing] handle_turn_fast cache_hit=0ms text=%r", text[:60])
            cached = True
            if mem is not None:
                try:
                    mem.add_turn(text, hit, lang)
                except Exception:
                    pass
            try:
                suggestions = _ant.suggest_next(text, hit) if getattr(
                    config, "enable_anticipation", True) else []
            except Exception:
                pass
            return {"reply": hit, "steps": [{"worker": "cache", "task": text}],
                    "calls": 0, "suggestions": suggestions, "cached": True}
    except Exception:
        pass

    # 0.5) Zero-LLM local chat: greetings, small talk, filler, trivial queries.
    #     These are answered instantly without touching any provider.
    #     STRICT: only handle obvious small talk, NOT questions or tasks.
    local_reply = _local_chat_reply(text)
    if local_reply is not None:
        # Only use local reply for actual small talk, not questions/tasks.
        low_check = (text or "").lower().strip()
        # Don't steal anything that looks like a question or task.
        if not any(w in low_check for w in ["?", "what", "how", "why", "when", "where", "who", "can", "could", "would", "is", "are", "do", "does", "did", "will", "shall", "should", "may", "might"]):
            if mem is not None:
                try:
                    mem.add_turn(text, local_reply, lang)
                except Exception:
                    pass
            try:
                suggestions = _ant.suggest_next(text, local_reply) if getattr(
                    config, "enable_anticipation", True) else []
            except Exception:
                pass
            return {"reply": local_reply, "steps": [{"worker": "local_chat", "task": text}],
                    "calls": 0, "suggestions": suggestions, "cached": False}

    # 1) LOCAL resolution first (0 LLM calls, no key needed). Math / time /
    #    simple lookup are handled entirely on-device before any classifier.
    has_math = bool(_re.search(r"[\d\.\)]\s*[\+\-\*/]\s*[\d\.\(]", text)) or \
               (_re.fullmatch(r"\s*[\d\.\+\-\*/\(\)\s]+\s*", text) is not None) or \
               bool(_WORD_OP_RE.search(text))
    if has_math:
            try:
                expr = text
                # Strip prefixes like "what is", "calculate", "compute", etc.
                expr = _re.sub(r"(?i)^\s*(what\s+is|what's|calculate|compute|"
                               r"can you|please|tell\s+me)\s+", "", expr).strip()
                # Convert word operators to symbols.
                def _word_to_sym(m):
                    w = m.group(1).lower()
                    for word, sym in _WORD_OPS:
                        if w == word or w.startswith(word):
                            return " " + sym + " "
                    return " " + m.group(1) + " "
                expr = _WORD_OP_RE.sub(lambda m: _word_to_sym(m), expr)
                # Keep only digits, operators, parens, spaces, decimal points.
                expr = _re.sub(r"[^0-9\.\+\-\*\/\(\)\s]", "", expr).strip()
                # Fallback: try to grab the last symbolic chunk if the text was
                # mostly prose with only a tiny equation at the end.
                if not expr:
                    m = _re.search(r"([\d\.\+\-\*\/\(\)\s]{3,})", text)
                    expr = m.group(1) if m else ""
                ans = _safe_calc(expr)
                if ans is not None:
                    # Collapse any repeated whitespace from word-op substitution.
                    expr = _re.sub(r"\s+", " ", expr).strip()
                    reply = f"{expr} = {ans}"
                    cacher.put(text, reply)
                    if mem is not None:
                        try:
                            mem.add_turn(text, reply, lang)
                        except Exception:
                            pass
                    logger.info("[timing] handle_turn_fast calc=%.3fs text=%r", time.time() - _t0, text[:60])
                    return {"reply": reply, "steps": [{"worker": "calc", "task": text}],
                            "calls": 0, "suggestions": [], "cached": False}
            except Exception:
                pass
    # time / date
    # Use word-boundary matching so we only answer actual time/date questions,
    # not queries like "when did the jordan soldier die" or "check todays news".
    if (_re.search(r"\b(what\s+time|what\s+is\s+the\s+time|current\s+time|time\s+is\s+it)\b", low)
            or _re.search(r"\b(what\s+date|what\s+is\s+the\s+date|today\s+date|date\s+today)\b", low)
            or _re.search(r"\b(what\s+day|what\s+is\s+today|today\s+is)\b", low)):
            from datetime import datetime
            now = datetime.now()
            reply = f"It's {now.strftime('%I:%M %p')} on {now.strftime('%A, %d %B %Y')}."
            cacher.put(text, reply)
            if mem is not None:
                try:
                    mem.add_turn(text, reply, lang)
                except Exception:
                    pass
            logger.info("[timing] handle_turn_fast time=%.3fs text=%r", time.time() - _t0, text[:60])
            return {"reply": reply, "steps": [{"worker": "local", "task": text}],
                    "calls": 0, "suggestions": [], "cached": False}
    # Memory recall: check if the user is asking about stored facts before
    # falling back to web search. This makes "what is my name" actually work.
    if mem is not None and any(w in low for w in ["my name", "who am i", "what is my",
                                                   "what's my", "what are my",
                                                   "my favorite", "my preference",
                                                   "what do i like", "what do you know about me"]):
            try:
                facts = mem.get_relevant_facts(text, k=5)
                if facts:
                    reply = "\n".join(f"- {f}" for f in facts)
                    return {"reply": reply, "steps": [{"worker": "memory", "task": text}],
                            "calls": 0, "suggestions": [], "cached": False}
            except Exception:
                pass
    # simple web lookup (free DuckDuckGo/Wikipedia, no API key). Only when there
    # is NO arithmetic expression (so "what is 2+2*3" calc's, not searches).
    if (not has_math) and any(w in low for w in ["search", "look up", "google",
                               "who is", "what is", "news", "find out", "wikipedia",
                               "headlines", "latest", "check", "tell me about"]):
            try:
                from .tools import get_tool_handler
                handler = get_tool_handler("web_search")
                if handler is not None:
                    # Clean query: strip common prefixes but keep the core question.
                    q = _re.sub(r"^(search|google|look up|find out|who is|what is|check|get|tell me about|tell me)\s+",
                                "", low).strip() or text
                    # For news queries, add "news" to the query if not present.
                    if "news" in low and "news" not in q.lower():
                        q = q + " news"
                    out = handler({"query": q})
                    reply = out if out else resilience.EN_FALLBACK
                    cacher.put(text, reply)
                    if mem is not None:
                        try:
                            mem.add_turn(text, reply, lang)
                        except Exception:
                            pass
                    return {"reply": reply, "steps": [{"worker": "web_search", "task": text}],
                            "calls": 0, "suggestions": [], "cached": False}
            except Exception as e:
                logger.warning("local web_search failed, falling back: %s", e)

    # 2) Local triage (pure heuristic, 0 LLM calls). An obvious action word
    #    => delegate to the agentic loop (capability preserved, on demand).
    #    Everything else (chat, creative, advice) => one companion call.
    #    This keeps chat at exactly 1 call with NO extra classifier call.
    _TASK_SIGNALS = ["search", "code", "python", "open", "terminal", "file",
                     "calc", "calculate", "time", "run", "system", "app",
                     "website", "url", "delete", "write", "create", "execute",
                     "install", "launch", "browse", "research", "news", "send",
                     "click", "type", "screenshot", "script", "program",
                     "remember", "rename", "move", "copy", "update", "build",
                     "summarize", "plan", "how to", "problem", "email", "remind"]
    is_task = any(w in low for w in _TASK_SIGNALS)

    # 2a) Catch natural-language requests the keyword list above misses. Without
    #     this, polite/imperative commands ("please book a cab", "can you check
    #     my email", "turn off the wifi") fell into chat mode and the model
    #     lazily replied "ok"/"sure". We escalate requests and imperative verbs
    #     to the agentic engine. Pure chit-chat and plain informational questions
    #     stay on the cheap single-call chat path.
    if not is_task:
        _REQUEST_PHRASES = (
            "can you", "could you", "would you", "will you", "please", "pls",
            "i want you to", "i need you to", "i'd like you to",
            "i would like you to", "help me", "make me", "get me",
            "tell friday", "ask friday", "friday,", "hey friday", "ok friday",
        )
        _ACTION_VERBS = (
            "send", "email", "message", "text", "call", "book", "order",
            "schedule", "set", "create", "make", "build", "write", "compose",
            "delete", "remove", "rename", "move", "copy", "open", "close",
            "start", "stop", "launch", "run", "execute", "find", "search",
            "lookup", "show", "display", "play", "download", "install",
            "update", "upgrade", "check", "read", "turn", "lock", "unlock",
            "connect", "disconnect", "remind", "translate", "summarize",
            "compute", "scan", "print", "save", "share", "post", "organize",
            "clean", "sort", "list", "activate", "enable", "disable", "switch",
            "mute", "unmute", "dial", "arrange", "prepare", "fetch",
        )
        _INFO_QUESTION = _re.compile(
            r"\?|^(what|how|why|who|when|where|which|explain|describe|define|"
            r"tell me about|what's|who's|where's|how's)\b")
        if any(p in low for p in _REQUEST_PHRASES) and not _INFO_QUESTION.match(low):
            is_task = True
        else:
            # Imperative: drop leading filler words, then check the first verb.
            _w = _re.sub(r"[^a-z\s]", " ", low).split()
            while _w and _w[0] in ("friday", "hey", "hi", "hello", "ok", "so",
                                   "please", "pls", "yo", "friday,"):
                _w.pop(0)
            if _w and _w[0] in _ACTION_VERBS:
                is_task = True

    # 2b) Autonomous RESEARCH / BUILD / INVESTIGATE -> the verify-and-promote
    #     engine. Isolated per-topic folder, self-test gate, promote-on-green.
    #     This is the trusted "do real work" path (creative + 100% verified).
    _RESEARCH_SIGNALS = ["research", "investigate", "deep dive", "deep research",
                          "build me", "find out deeply", "study", "analyze deeply"]
    is_research = any(w in low for w in _RESEARCH_SIGNALS) or \
        (("research" in low or "build" in low or "investigate" in low)
         and ("about" in low or "on" in low or "the" in low or "how" in low))
    if is_research and not has_math:
            try:
                from . import research as _research
                depth = "deep" if any(w in low for w in ["deep", "thorough", "full"]) else "quick"
                status = _research.run_research(text, depth=depth)
                reply = (f"I researched '{text}'.\nStatus: {status.get('status')}. "
                         f"Sources reviewed: {status.get('sources_count')}. "
                         f"Self-test passed: {status.get('tests_passed')}.")
                if status.get("report"):
                    reply += "\n\n" + status["report"][:1200]
                return {"reply": reply,
                        "steps": [{"worker": "researcher", "task": text},
                                  {"worker": "builder", "task": text},
                                  {"worker": "tester", "task": text},
                                  {"worker": "promoter", "task": text}],
                        "calls": 0, "suggestions": [], "cached": False,
                        "research": status}
            except Exception as e:
                logger.error("research routing failed: %s", e)
                # fall through to generic task handling below

    # 3) CHAT -> single OPERATOR reasoning call (high-level "think like you"
    #    brain). Still exactly 1 LLM call. Self-check is opt-in (ENABLE_SELFCHECK).
    if not is_task:
        from . import reason as _reason
        ctx = mem.system_context(text) if mem else ""
        _t_reason0 = time.time()
        reply = _reason.reason_with_selfcheck(text, context=ctx, lang=lang) \
            if getattr(config, "enable_selfcheck", False) \
            else _reason.reason(text, context=ctx, lang=lang)
        _t_reason1 = time.time()
        logger.info("[timing] handle_turn_fast reason=%.3fs text=%r", _t_reason1 - _t_reason0, text[:60])
        if not reply:
            # Reasoning call failed (e.g. dead key): fall back to the thin
            # companion path so Friday still answers from cache/memory.
            reply = _companion_reply(text, mem, screen_context=screen_context)
        calls = 1
        cacher.put(text, reply)
        if mem is not None:
            try:
                mem.add_turn(text, reply, lang)
                if getattr(config, "enable_user_memory", True):
                    nm = _re.search(r"(?:i am|i'm|my name is|call me)\s+([a-zA-Z][a-zA-Z]+)", low)
                    if nm:
                        mem.remember_user("name", nm.group(1).title())
                mem.note_working("last_topic", text[:80])
            except Exception:
                pass
        try:
            if getattr(config, "enable_anticipation", True):
                state = _ant.detect_state(text)
                suggestions = _ant.suggest_next(text, reply, state=state)
        except Exception:
            pass
        try:
            _learning.observe(text, reply, success=True, latency=time.time() - _t0)
        except Exception:
            pass
        return {"reply": reply, "steps": [{"worker": "reason", "task": text}],
                "calls": calls, "suggestions": suggestions, "cached": False}

    # 3) Complex TASK -> full agentic loop (capability preserved, on demand).
    #    But first, handle simple "open <app>" commands locally so they don't
    #    need the LLM to plan tool calls (the 1.5B model often messes this up).
    _open_match = _re.match(r"^(?:open|launch|start)\s+(.+?)(?:\s+please)?$", low)
    if _open_match and not has_math:
        app_name = _open_match.group(1).strip()
        try:
            from .tools import safe_tool_call
            result_str = safe_tool_call("open_app", {"name": app_name})
            if result_str and not result_str.startswith("unknown tool") and not result_str.startswith("could not open"):
                return {"reply": result_str, "steps": [{"worker": "open_app", "task": text}],
                        "calls": 0, "suggestions": [], "cached": False}
        except Exception:
            pass

    # 3b) Multi-faceted requests -> parallel fan-out to 2-4 workers simultaneously.
    #     This covers "research X and code Y", "compare A vs B", "analyze and build",
    #     and similar requests that benefit from parallel decomposition.
    _PARALLEL_SIGNALS = ["research.*and.*code", "research.*and.*build",
                         "analyze.*and.*create", "compare.*and.*contrast",
                         "plan.*and.*implement", "investigate.*and.*build",
                         "find.*code", "write.*and.*test", "both", "multiple"]
    _is_parallel = any(_re.search(p, low) for p in _PARALLEL_SIGNALS)
    if _is_parallel:
        try:
            from . import parallel as _parallel
            _t_para0 = time.time()
            reply = _parallel.process(text,
                                      context=mem.system_context(text) if mem else "",
                                      screen=screen_context)
            _t_para1 = time.time()
            logger.info("[timing] handle_turn_fast parallel=%.3fs text=%r",
                        _t_para1 - _t_para0, text[:60])
            if mem is not None:
                try:
                    mem.add_turn(text, reply, lang)
                except Exception:
                    pass
            return {"reply": reply,
                    "steps": [{"worker": "parallel", "task": text}],
                    "calls": 4, "suggestions": [], "cached": False}
        except Exception as e:
            logger.warning("Parallel execution failed, falling back: %s", e)

    _t_agentic0 = time.time()
    result = agentic_run_with_plan(text, lang=lang,
                         context=mem.system_context(text) if mem else None,
                         max_steps=4, screen_context=screen_context)
    _t_agentic1 = time.time()
    result.setdefault("steps", [])
    result["calls"] = len(result.get("steps", [])) + 1
    result["suggestions"] = []
    result["cached"] = False
    logger.info("[timing] handle_turn_fast agentic=%.3fs steps=%s text=%r",
                _t_agentic1 - _t_agentic0, len(result.get("steps", [])), text[:60])
    if mem is not None:
        try:
            mem.add_turn(text, result["reply"], lang)
        except Exception:
            pass
    try:
        if getattr(config, "enable_anticipation", True):
            state = _ant.detect_state(text)
            result["suggestions"] = _ant.suggest_next(text, result["reply"], state=state)
    except Exception:
        pass
    try:
        _learning.observe(text, result["reply"], tool="agentic",
                          success=not result.get("error"), latency=time.time() - _t0)
    except Exception:
        pass
    return result


def _safe_calc(expr: str):
    """Eval a simple arithmetic expression safely (no names/builtins).

    Validates the AST contains ONLY arithmetic nodes (no calls, no names,
    no non-numeric constants), then evaluates with a stripped namespace so
    no builtins/functions are reachable. This is safe despite using eval.
    """
    try:
        import ast as _ast
        node = _ast.parse(expr, mode="eval")
        for n in _ast.walk(node):
            if isinstance(n, _ast.Call):
                return None
            if isinstance(n, _ast.Name):
                return None
            if isinstance(n, _ast.Constant) and not isinstance(n.value, (int, float)):
                return None
        return eval(expr, {"__builtins__": {}}, {})
    except Exception:
        return None


def handle_turn(text: str, lang: str = "en", memory=None,
                max_steps: int = None, screen_context: str = None) -> Dict:
    """Classify intent (instant heuristic + optional LLM), then route to the
    cheap single-call fast path (default) or the full agentic loop.

    When config.single_call_mode is true (default), handle_turn_fast runs:
    chat = 1 companion call, local tasks = 0 calls, complex tasks delegate to
    agentic_run. This is the "think like you" path — strong but cheap.
    Set SINGLE_CALL_MODE=false to use the older orchestrate() fan-out.

    Always returns {"reply": str, "steps": [...], "calls": int,
                    "suggestions": [...], "cached": bool}. Never raises.
    """
    mem = memory or _default_memory
    try:
        if getattr(config, "single_call_mode", True):
            return handle_turn_fast(text, lang=lang, memory=mem,
                                    screen_context=screen_context)
        # Legacy rich path (kept for parity / explicit complex routing).
        intent = _classify(text)
        if intent == "chat":
            reply = _companion_reply(text, mem, screen_context=screen_context)
            return {"reply": reply, "steps": [{"worker": "companion", "task": text}],
                    "calls": 1, "suggestions": [], "cached": False}
        result = agentic_run(text, lang=lang,
                             context=mem.system_context(text) if mem else None,
                             max_steps=max_steps if max_steps else 4,
                             screen_context=screen_context)
        result.setdefault("steps", [])
        result["calls"] = len(result.get("steps", [])) + 1
        result["suggestions"] = []
        result["cached"] = False
        if mem is not None:
            try:
                mem.add_turn(text, result["reply"], lang)
            except Exception:
                pass
        return result
    except Exception as e:
        logger.error("handle_turn failed: %s", e)
        return {"reply": resilience.EN_FALLBACK, "steps": [],
                "calls": 0, "suggestions": [], "cached": False}


def start_background_research(topic: str, depth: str = "quick", output_file: str = None) -> dict:
    """Start an autonomous research task in the background.

    Returns a task dict with task_id, status, and estimated completion.
    """
    try:
        from .background_tasks import BackgroundTaskQueue
        from .proactive import announce

        task_queue = BackgroundTaskQueue()

        if not output_file:
            safe_name = "".join(c if c.isalnum() or c in "._-" else "_" for c in topic)
            output_file = os.path.join(config.data_dir, "reports", f"{safe_name}_{int(time.time())}.md")

        payload = {
            "topic": topic,
            "depth": depth,
            "output_file": output_file,
            "notify": True,
        }

        task_id = task_queue.submit_task("research", payload=payload, priority=0)

        announce(f"Starting research on {topic}. I'll notify you when it's done.", "info")

        return {
            "task_id": task_id,
            "status": "started",
            "topic": topic,
            "output_file": output_file,
            "message": f"Research started. Check {output_file} when complete.",
        }
    except Exception as e:
        logger.error("Failed to start background research: %s", e)
        return {"error": str(e), "status": "failed"}


def get_research_status(task_id: str = None) -> dict:
    """Get status of background research tasks."""
    try:
        from .background_tasks import BackgroundTaskQueue
        task_queue = BackgroundTaskQueue()

        if task_id:
            status = task_queue.get_task_status(task_id)
            result = task_queue.get_task_result(task_id)
            return {"task_id": task_id, "status": status, "result": result}

        tasks = task_queue.list_tasks()
        return {"tasks": tasks}
    except Exception as e:
        return {"error": str(e)}
