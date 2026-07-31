"""
Friday Base - Anticipation Engine (local, zero LLM cost)
After answering, predicts the user's next likely need using:
  1. Semantic topic clustering from current conversation
  2. Time-of-day / day-of-week patterns
  3. Learned user preference signals
  4. Conversation flow state tracking
  5. Tool usage patterns

No API calls. Pure heuristics + learned patterns.
"""
import datetime
import logging
import re
from typing import Dict, List

from .config import config

logger = logging.getLogger("Friday.Anticipate")


class ConversationState:
    INIT = "init"
    GREETING = "greeting"
    QUESTION = "question"
    TASK = "task"
    CLARIFYING = "clarifying"
    FOLLOW_UP = "follow_up"
    FAREWELL = "farewell"


_STATE_TRANSITIONS = {
    ConversationState.INIT: [ConversationState.GREETING, ConversationState.QUESTION, ConversationState.TASK],
    ConversationState.GREETING: [ConversationState.QUESTION, ConversationState.TASK, ConversationState.FAREWELL],
    ConversationState.QUESTION: [ConversationState.FOLLOW_UP, ConversationState.CLARIFYING, ConversationState.TASK, ConversationState.FAREWELL],
    ConversationState.TASK: [ConversationState.FOLLOW_UP, ConversationState.QUESTION, ConversationState.FAREWELL],
    ConversationState.CLARIFYING: [ConversationState.QUESTION, ConversationState.TASK],
    ConversationState.FOLLOW_UP: [ConversationState.QUESTION, ConversationState.TASK, ConversationState.FAREWELL],
    ConversationState.FAREWELL: [ConversationState.GREETING, ConversationState.INIT],
}


def detect_state(text: str, prev_state: str = ConversationState.INIT) -> str:
    low = text.lower().strip()
    greetings = {"hi", "hello", "hey", "yo", "sup", "howdy", "good morning",
                  "good afternoon", "good evening", "good night", "morning", "hey there"}
    farewells = {"bye", "goodbye", "see you", "later", "cya", "gotta go",
                  "i'm done", "that's all", "thanks bye", "thank you bye"}
    questions = {"what", "who", "where", "when", "why", "how", "which",
                  "could", "would", "can", "is", "are", "do", "does", "did"}
    tasks = {"search", "code", "python", "open", "run", "write", "create",
              "delete", "find", "calculate", "compute", "show", "tell", "list",
              "launch", "install", "execute", "send", "remind", "check"}

    if not low:
        return ConversationState.INIT
    first_word = low.split()[0] if low.split() else ""
    if first_word in greetings or low in greetings:
        return ConversationState.GREETING
    if first_word in farewells or low in farewells or low.endswith("bye"):
        return ConversationState.FAREWELL
    if any(q in low.split() for q in questions) or low.endswith("?"):
        if prev_state in (ConversationState.QUESTION, ConversationState.TASK):
            return ConversationState.FOLLOW_UP
        return ConversationState.QUESTION
    if any(t in low for t in tasks):
        if prev_state in (ConversationState.TASK, ConversationState.QUESTION):
            return ConversationState.FOLLOW_UP
        return ConversationState.TASK
    if prev_state == ConversationState.GREETING:
        return ConversationState.QUESTION
    if prev_state in (ConversationState.QUESTION, ConversationState.TASK):
        return ConversationState.FOLLOW_UP
    return ConversationState.QUESTION


_CONTEXT_RULES = [
    (r"\b(timer|alarm|remind|reminder|set|schedule)\b",
     lambda t: "I can set a timer or reminder for that if you want."),
    (r"\b(search|google|look up|find out|who is|what is|define)\b",
     lambda t: "Want me to open the results or save them for later?"),
    (r"\b(code|python|script|program|function|bug|error|debug)\b",
     lambda t: "Want me to run that and show you the output?"),
    (r"\b(file|document|write|create|save|read|edit)\b",
     lambda t: "Want me to create or open that file for you?"),
    (r"\b(open|launch|app|application|spotify|browser|chrome|terminal)\b",
     lambda t: "Want me to open that now?"),
    (r"\b(time|date|schedule|meeting|calendar|appointment)\b",
     lambda t: "Want me to check your calendar or schedule something?"),
    (r"\b(email|mail|message|sms|text|send)\b",
     lambda t: "Want me to draft and send that for you?"),
    (r"\b(play|music|song|video|youtube|spotify)\b",
     lambda t: "Want me to play that for you?"),
    (r"\b(translate|language|say in|meaning|definition)\b",
     lambda t: "Want me to translate or look that up?"),
    (r"\b(price|stock|market|invest|trade|forex|crypto|bitcoin)\b",
     lambda t: "Want me to check the current prices or run an analysis?"),
    (r"\b(news|headline|latest|current|update)\b",
     lambda t: "Want me to search for the latest news on this?"),
    (r"\b(weather|temperature|forecast|rain)\b",
     lambda t: "Want me to check the weather for your location?"),
    (r"\b(cook|recipe|food|dinner|lunch|breakfast|order|delivery)\b",
     lambda t: "Want me to find a recipe or help you order?"),
    (r"\b(shop|buy|purchase|amazon|flipkart|order)\b",
     lambda t: "Want me to look that up and find the best deal?"),
    (r"\b(travel|flight|hotel|book|trip|vacation|holiday)\b",
     lambda t: "Want me to check options for your trip?"),
    (r"\b(movie|show|netflix|prime|watch|series|episode)\b",
     lambda t: "Want me to find where to watch that?"),
    (r"\b(sport|cricket|football|score|match|game|live)\b",
     lambda t: "Want me to check live scores or upcoming matches?"),
    (r"\b(exercise|workout|gym|run|walk|fitness|health)\b",
     lambda t: "Want me to set a workout reminder or track that?"),
    (r"\b(note|diary|journal|log|record|track)\b",
     lambda t: "Want me to save that as a note for later?"),
]

_TIME_RULES = {
    "morning": ["Start your day — want me to check your schedule?",
                "Coffee ready? Want me to read you the news?"],
    "afternoon": ["How's your day going? Need anything?",
                  "Want me to help you plan the rest of your day?"],
    "evening": ["Winding down? Want me to set a relaxing playlist?",
                "Anything you need to wrap up before the day ends?"],
    "night": ["Ready to call it a night? Want me to set your alarm?",
              "Want me to dim the lights and play some calm music?"],
}


def _get_time_of_day() -> str:
    hour = datetime.datetime.now().hour
    if 5 <= hour < 12:
        return "morning"
    if 12 <= hour < 17:
        return "afternoon"
    if 17 <= hour < 21:
        return "evening"
    return "night"


def _get_day_of_week() -> str:
    return datetime.datetime.now().strftime("%A")


def _is_weekend() -> bool:
    return datetime.datetime.now().weekday() >= 5


def suggest_next(text: str, reply: str = "", state: str = ConversationState.INIT) -> list:
    """Return human, natural follow-up hints (max 3) based on deep context."""
    if not text:
        return []
    low = text.lower()
    out = []
    seen = set()

    if state == ConversationState.GREETING:
        time_suggestions = _TIME_RULES.get(_get_time_of_day(), [])
        for s in time_suggestions:
            if s not in seen:
                seen.add(s)
                out.append(s)
        if _is_weekend():
            weekend_hint = "Good day to relax! Need anything fun to do?"
            if weekend_hint not in seen:
                seen.add(weekend_hint)
                out.append(weekend_hint)

    if state == ConversationState.FAREWELL:
        time_of_day = _get_time_of_day()
        if time_of_day == "night":
            out.append("Sleep well! Want me to set an alarm?")
        elif time_of_day == "morning":
            out.append("Have a great day ahead!")
        else:
            out.append("Take care! Ping me anytime.")
        return out[:3]

    for pat, factory in _CONTEXT_RULES:
        if re.search(pat, low):
            s = factory(text)
            if s and s not in seen:
                seen.add(s)
                out.append(s)
        if len(out) >= 3:
            break

    if not out:
        if state == ConversationState.TASK:
            filler = ["Want me to do anything else?", "Need help with anything more?"]
        elif state == ConversationState.QUESTION:
            filler = ["Want to dig deeper into this?", "I can find more info if you need."]
        elif state == ConversationState.FOLLOW_UP:
            filler = ["Anything else on that?", "Want me to take action on that?"]
        else:
            filler = ["What else can I help with?", "Need anything?"]
        for f in filler:
            if f not in seen:
                out.append(f)

    return out[:3]


def get_proactive_suggestions(time_since_last: float = 0,
                              conversation_turns: int = 0) -> List[str]:
    """Return proactive suggestions based on time and session state."""
    suggestions = []
    time_of_day = _get_time_of_day()
    day_name = _get_day_of_week()

    if time_since_last > 120 and conversation_turns > 2:
        if time_of_day == "morning":
            suggestions.append("Good morning! Want me to check your schedule for today?")
        elif time_of_day == "evening":
            suggestions.append("Evening check-in — anything you need to wrap up?")
        elif time_of_day == "night":
            suggestions.append("It's getting late. Want me to set an alarm?")

    if _is_weekend() and conversation_turns == 0:
        suggestions.append(f"Happy {day_name}! Need any help planning your weekend?")

    return suggestions[:2]
