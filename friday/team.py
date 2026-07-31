"""
Friday's Team - the single source of truth for the named worker-avatars.
Each member maps to a REAL role in config.role_chains / upgrader.
Pure data, no side effects, stdlib only. Used by run.py (/team endpoint) and the UI.

Icons are TECHNICAL (not decorative) so the orbit reads as an engineering
console: each worker has a distinct symbol tied to its function.
"""
import sys

STATUS_ICONS = {
    "idle":           "○",
    "thinking":       "◌",
    "working":        "◉",
    "done":           "✓",
    "error":          "✕",
    "needs_approval": "?",
}

UPGRADE_STATUS_TO_RUNTIME = {
    "planned": "thinking",
    "built": "working",
    "tested_pass": "done",
    "tested_fail": "error",
    "approved": "done",
    "applied": "done",
    "rejected": "idle",
    "rolled_back": "idle",
    "error": "error",
}

FRIDAY = {
    "id": "friday",
    "name": "Friday",
    "role": "friday",
    "tagline": "your right hand — sharp, warm, unstoppable",
    "head_sign": "◈",
    "emoji_face": "◈",
    "color": "#e879f9",
    "center": True,
    "group": "center",
    "duty": "leads the team, talks with you, gets things done",
}

TEAM = [
    {
        "id": "router",
        "name": "Vayu",
        "role": "router",
        "tagline": "the swift messenger — never a wrong turn",
        "head_sign": "➤",
        "emoji_face": "➤",
        "color": "#38bdf8",
        "group": "core",
        "duty": "reads your intent and sends every task to exactly the right worker — fast, no mistakes",
    },
    {
        "id": "reasoner",
        "name": "Neo",
        "role": "reasoner",
        "tagline": "sees the matrix — breaks it down",
        "head_sign": "🧠",
        "emoji_face": "🧠",
        "color": "#a78bfa",
        "group": "core",
        "duty": "thinks through every angle, plans the steps, and figures out the smartest path forward",
    },
    {
        "id": "coder",
        "name": "Forge",
        "role": "coder",
        "tagline": "shapes logic into reality",
        "head_sign": "⚙️",
        "emoji_face": "⚙️",
        "color": "#f59e0b",
        "group": "core",
        "duty": "writes clean code, hammers out bugs, and runs it so you don't have to",
    },
    {
        "id": "researcher",
        "name": "Scout",
        "role": "researcher",
        "tagline": "always hunting — never empty-handed",
        "head_sign": "🔍",
        "emoji_face": "🔍",
        "color": "#34d399",
        "group": "core",
        "duty": "scours the web, cross-references sources, and brings back the truth — not just links",
    },
    {
        "id": "judge",
        "name": "Verdict",
        "role": "judge",
        "tagline": "the final word — fair and sharp",
        "head_sign": "⚖️",
        "emoji_face": "⚖️",
        "color": "#fbbf24",
        "group": "core",
        "duty": "tastes every answer and serves the one that's most natural, correct, and useful",
    },
    {
        "id": "verifier",
        "name": "Prism",
        "role": "verifier",
        "tagline": "sees what others miss",
        "head_sign": "✅",
        "emoji_face": "✅",
        "color": "#22d3ee",
        "group": "core",
        "duty": "checks every reply for fluency, accuracy, and polish — nothing slips past",
    },
    {
        "id": "planner",
        "name": "Oracle",
        "role": "planner",
        "tagline": "reads the future — then builds it",
        "head_sign": "🗺️",
        "emoji_face": "🗺️",
        "color": "#c084fc",
        "group": "upgrade",
        "duty": "envisions the perfect upgrade, maps every dependency, and charts the safest path",
    },
    {
        "id": "builder",
        "name": "Titan",
        "role": "builder",
        "tagline": "heavy lifting, zero drama",
        "head_sign": "🔨",
        "emoji_face": "🔨",
        "color": "#fb7185",
        "group": "upgrade",
        "duty": "takes the plan, builds the code in staging, and makes sure it compiles before anyone sees it",
    },
    {
        "id": "reviewer",
        "name": "Sentinel",
        "role": "reviewer",
        "tagline": "trust, but verify — always",
        "head_sign": "🛡️",
        "emoji_face": "🛡️",
        "color": "#94a3b8",
        "group": "upgrade",
        "duty": "reviews every change for risk, sanity, and safety — then watches your back while you decide",
    },
    {
        "id": "skill_builder",
        "name": "Architect",
        "role": "skill_builder",
        "tagline": "turns your intent into reusable skills — tools, workers, workflows",
        "head_sign": "🏗️",
        "emoji_face": "🏗️",
        "color": "#f472b6",
        "group": "skills",
        "duty": "listens to your intent, designs the skill (tool/worker/workflow), generates the code, tests it, and registers it so Friday can use it forever",
    },
]


def roster() -> dict:
    """Full roster for the UI."""
    return {
        "friday": FRIDAY,
        "members": TEAM,
        "status_icons": STATUS_ICONS,
        "upgrade_status_map": UPGRADE_STATUS_TO_RUNTIME,
    }


def get(member_id: str) -> dict | None:
    """Look up Friday or a member by id/role."""
    if member_id == FRIDAY["id"]:
        return FRIDAY
    for m in TEAM:
        if m["id"] == member_id:
            return m
    return None


def name_of(role: str) -> str:
    """Return the cute name for a role, fallback to role.title()."""
    m = get(role)
    if m is not None:
        return m["name"]
    return role.title()


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

    print(f"{FRIDAY['head_sign']} {FRIDAY['name']} ({FRIDAY['role']}) - {FRIDAY['tagline']}")
    for m in TEAM:
        print(f"{m['head_sign']} {m['name']} ({m['role']}) - {m['tagline']}")
    print("TEAM OK")
